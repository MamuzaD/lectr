package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MinimumOverlap = 5 * time.Minute
)

type ClassMeeting struct {
	Course string
	Start  int
	End    int
	Days   []time.Weekday
}

type Schedule struct {
	Start    time.Time
	End      time.Time
	Meetings []ClassMeeting
}

type Recording struct {
	Path     string
	UUID     string
	Started  time.Time
	Duration time.Duration
}

type Options struct {
	Root     string
	Source   string
	Schedule Schedule
	Location *time.Location
	DryRun   bool
	Quiet    bool
}

func Route(ctx context.Context, options Options) (int, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return 0, err
	}
	options.Root = root
	if options.Location == nil {
		options.Location = time.Local
	}
	return routeRecordings(ctx, options)
}

func routeRecordings(ctx context.Context, options Options) (int, error) {
	if options.Source == "" {
		return 0, errors.New("Voice Memos directory is unavailable")
	}
	info, err := os.Stat(options.Source)
	if err != nil {
		return 0, fmt.Errorf("cannot access Voice Memos directory %s: %w", options.Source, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("Voice Memos directory not found: %s", options.Source)
	}
	known, err := existingUUIDs(ctx, options)
	if err != nil {
		return 0, err
	}
	paths, err := sourceRecordingPaths(options.Source)
	if err != nil {
		return 0, err
	}
	sort.Strings(paths)
	copied := 0
	reserved := make(map[string]bool)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		recording, err := inspectRecording(ctx, path, options.Location)
		if err != nil {
			return copied, err
		}
		if recording == nil || known[recording.UUID] {
			continue
		}
		meeting := MatchingClass(*recording, options.Schedule)
		if meeting == nil {
			continue
		}
		destination, err := nextDestination(options.Root, meeting.Course, recording.Started, reserved)
		if err != nil {
			return copied, err
		}
		reserved[destination] = true
		action := "Copied"
		if options.DryRun {
			action = "Would copy"
			known[recording.UUID] = true
		} else {
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return copied, err
			}
			if err := CopyAtomically(path, destination); err != nil {
				return copied, err
			}
			known[recording.UUID] = true
		}
		fmt.Printf("%s %s -> %s\n", action, filepath.Base(path), relativeToRoot(destination, options.Root))
		copied++
	}
	if copied == 0 && !options.Quiet {
		fmt.Println("No new class recordings found.")
	}
	return copied, nil
}

// SourceInventory deliberately uses ReadDir instead of filepath.Glob. Glob
// suppresses directory read errors, which made a launchd privacy failure look
// exactly like an empty Voice Memos folder.
func SourceInventory(source string) (int, error) {
	paths, err := sourceRecordingPaths(source)
	if err != nil {
		return 0, err
	}
	return len(paths), nil
}

func sourceRecordingPaths(source string) ([]string, error) {
	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, fmt.Errorf("cannot read Voice Memos directory %s: %w", source, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".m4a") {
			continue
		}
		paths = append(paths, filepath.Join(source, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func ffprobePath() (string, error) {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", errors.New("ffprobe is missing; install it with: brew install ffmpeg")
	}
	return path, nil
}

func inspectRecording(ctx context.Context, path string, location *time.Location) (*Recording, error) {
	probe, err := ffprobePath()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration:format_tags=creation_time,voice-memo-uuid", "-of", "json", path)
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	var result struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, nil
	}
	created := result.Format.Tags["creation_time"]
	uuid := result.Format.Tags["voice-memo-uuid"]
	duration, durationErr := time.ParseDuration(result.Format.Duration + "s")
	started, startedErr := parseCreationTime(created, location)
	if uuid == "" || durationErr != nil || startedErr != nil {
		return nil, nil
	}
	return &Recording{Path: path, UUID: uuid, Started: started.In(location), Duration: duration}, nil
}

func parseCreationTime(value string, location *time.Location) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05.999999999", value, location)
}

func MatchingClass(recording Recording, schedule Schedule) *ClassMeeting {
	local := recording.Started
	if local.Before(schedule.Start) || local.After(schedule.End) {
		return nil
	}
	end := local.Add(recording.Duration)
	best := -1 * time.Nanosecond
	var match *ClassMeeting
	for index := range schedule.Meetings {
		meeting := &schedule.Meetings[index]
		if !includesDay(meeting.Days, local.Weekday()) {
			continue
		}
		start := time.Date(local.Year(), local.Month(), local.Day(), meeting.Start/60, meeting.Start%60, 0, 0, local.Location())
		finish := time.Date(local.Year(), local.Month(), local.Day(), meeting.End/60, meeting.End%60, 0, 0, local.Location())
		overlap := minTime(end, finish).Sub(maxTime(local, start))
		if overlap >= MinimumOverlap && overlap > best {
			best, match = overlap, meeting
		}
	}
	return match
}

func includesDay(days []time.Weekday, target time.Weekday) bool {
	for _, day := range days {
		if day == target {
			return true
		}
	}
	return false
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func maxTime(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func existingUUIDs(ctx context.Context, options Options) (map[string]bool, error) {
	uuids := make(map[string]bool)
	seenCourses := make(map[string]bool)
	for _, meeting := range options.Schedule.Meetings {
		if seenCourses[meeting.Course] {
			continue
		}
		seenCourses[meeting.Course] = true
		memoDir := filepath.Join(options.Root, meeting.Course, "memos")
		paths, err := filepath.Glob(filepath.Join(memoDir, "*.m4a"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			recording, err := inspectRecording(ctx, path, options.Location)
			if err != nil {
				return nil, err
			}
			if recording != nil {
				uuids[recording.UUID] = true
			}
		}
	}
	return uuids, nil
}

func NextDestination(root, course string, recorded time.Time) string {
	destination, _ := nextDestination(root, course, recorded, nil)
	return destination
}

func nextDestination(root, course string, recorded time.Time, reserved map[string]bool) (string, error) {
	memoDir := filepath.Join(root, course, "memos")
	prefix := recorded.Format("2006-01-02")
	paths, _ := filepath.Glob(filepath.Join(memoDir, prefix+"-pt[0-9][0-9].m4a"))
	part := 0
	for _, path := range paths {
		base := strings.TrimSuffix(filepath.Base(path), ".m4a")
		pieces := strings.Split(base, "-pt")
		if len(pieces) != 2 {
			continue
		}
		var value int
		if _, err := fmt.Sscanf(pieces[1], "%d", &value); err == nil && value > part {
			part = value
		}
	}
	for path := range reserved {
		if filepath.Dir(path) != memoDir {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(path), ".m4a")
		if !strings.HasPrefix(base, prefix+"-pt") {
			continue
		}
		var value int
		if _, err := fmt.Sscanf(strings.TrimPrefix(base, prefix+"-pt"), "%d", &value); err == nil && value > part {
			part = value
		}
	}
	if part >= 99 {
		return "", fmt.Errorf("%s: no part numbers remain for %s", course, prefix)
	}
	return filepath.Join(memoDir, fmt.Sprintf("%s-pt%02d.m4a", prefix, part+1)), nil
}

func CopyAtomically(source, destination string) error {
	metadata, err := os.Stat(source)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".memo-*.m4a")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	input, err := os.Open(source)
	if err != nil {
		temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	closeErr := input.Close()
	if copyErr != nil {
		temporary.Close()
		return copyErr
	}
	if closeErr != nil {
		temporary.Close()
		return closeErr
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, metadata.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(temporaryPath, metadata.ModTime(), metadata.ModTime()); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func relativeToRoot(path, root string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return value
}
