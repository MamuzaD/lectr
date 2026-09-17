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
	Path       string
	UUID       string
	CreationID string
	Synthetic  bool
	Started    time.Time
	Duration   time.Duration
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
		recording, err := inspectRecording(ctx, path, options.Location, strings.EqualFold(filepath.Ext(path), ".qta"))
		if err != nil {
			return copied, err
		}
		if recording == nil {
			continue
		}
		if existing := known.match(recording); existing != nil {
			if existing.Synthetic && recording.Duration > existing.Duration {
				action := "Updated"
				if options.DryRun {
					action = "Would update"
				} else if err := replaceRoutedAudio(path, existing.Path); err != nil {
					return copied, err
				}
				existing.Duration, existing.UUID, existing.Synthetic = recording.Duration, recording.UUID, recording.Synthetic
				fmt.Printf("%s %s -> %s\n", action, filepath.Base(path), relativeToRoot(existing.Path, options.Root))
				copied++
			}
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
			routed := *recording
			routed.Path = destination
			known.remember(&routed)
		} else {
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return copied, err
			}
			if err := CopyAtomically(path, destination); err != nil {
				return copied, err
			}
			routed := *recording
			routed.Path = destination
			known.remember(&routed)
		}
		fmt.Printf("%s %s -> %s\n", action, filepath.Base(path), relativeToRoot(destination, options.Root))
		copied++
	}
	if copied == 0 && !options.Quiet {
		fmt.Println("No new class recordings found.")
	}
	return copied, nil
}

// replaceRoutedAudio swaps in a longer copy of an already-routed recording.
// Transcripts of the old audio are set aside rather than deleted (they may
// carry hand edits) and are put back if the copy fails, so a failed update
// never costs a transcript.
func replaceRoutedAudio(source, destination string) error {
	moved, err := setAsideTranscripts(destination)
	if err == nil {
		err = CopyAtomically(source, destination)
	}
	if err != nil {
		return errors.Join(err, restoreTranscripts(moved))
	}
	return nil
}

type setAside struct{ original, aside string }

// setAsideTranscripts renames the part and combined transcripts derived from
// memoPath to *.superseded.txt, which the transcribe glob never matches.
func setAsideTranscripts(memoPath string) ([]setAside, error) {
	transcriptDir := filepath.Join(filepath.Dir(filepath.Dir(memoPath)), "transcripts")
	stem := strings.TrimSuffix(filepath.Base(memoPath), filepath.Ext(memoPath))
	paths := []string{filepath.Join(transcriptDir, stem+".txt")}
	if len(stem) >= len("2006-01-02") {
		paths = append(paths, filepath.Join(transcriptDir, stem[:len("2006-01-02")]+".txt"))
	}
	var moved []setAside
	for _, path := range paths {
		aside := freeSupersededPath(path)
		if err := os.Rename(path, aside); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return moved, err
		}
		moved = append(moved, setAside{original: path, aside: aside})
	}
	return moved, nil
}

// freeSupersededPath picks X.superseded.txt, or X.superseded-2.txt and so on
// when earlier updates already set a transcript aside, so a second update
// never overwrites a first one that may hold hand edits.
func freeSupersededPath(path string) string {
	base := strings.TrimSuffix(path, ".txt") + ".superseded"
	candidate := base + ".txt"
	for n := 2; ; n++ {
		// Any stat error (not just "missing") ends the search; the rename
		// that follows then reports the real problem.
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d.txt", base, n)
	}
}

func restoreTranscripts(moved []setAside) error {
	var errs []error
	for _, entry := range moved {
		if err := os.Rename(entry.aside, entry.original); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (!strings.EqualFold(ext, ".m4a") && !strings.EqualFold(ext, ".qta")) {
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

func inspectRecording(ctx context.Context, path string, location *time.Location, allowSynthetic bool) (*Recording, error) {
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
	if durationErr != nil || startedErr != nil {
		return nil, nil
	}
	creationID := "created:" + started.UTC().Format(time.RFC3339Nano)
	if uuid == "" {
		if !allowSynthetic {
			return nil, nil
		}
		// .qta recordings synced in from other devices via iCloud carry no
		// voice-memo-uuid tag. Creation time remains stable while an iCloud
		// download's duration grows, and is preserved in the routed copy.
		uuid = "synthetic:" + creationID
		return &Recording{Path: path, UUID: uuid, CreationID: creationID, Synthetic: true, Started: started.In(location), Duration: duration}, nil
	}
	return &Recording{Path: path, UUID: uuid, CreationID: creationID, Started: started.In(location), Duration: duration}, nil
}

type recordingIndex struct {
	byUUID     map[string]*Recording
	byCreation map[string][]*Recording
}

func newRecordingIndex() *recordingIndex {
	return &recordingIndex{byUUID: make(map[string]*Recording), byCreation: make(map[string][]*Recording)}
}

func (index *recordingIndex) match(recording *Recording) *Recording {
	if existing := index.byUUID[recording.UUID]; existing != nil {
		return existing
	}
	for _, existing := range index.byCreation[recording.CreationID] {
		if recording.Synthetic || existing.Synthetic {
			return existing
		}
	}
	return nil
}

func (index *recordingIndex) remember(recording *Recording) {
	copy := *recording
	index.byUUID[copy.UUID] = &copy
	index.byCreation[copy.CreationID] = append(index.byCreation[copy.CreationID], &copy)
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

func existingUUIDs(ctx context.Context, options Options) (*recordingIndex, error) {
	recordings := newRecordingIndex()
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
			recording, err := inspectRecording(ctx, path, options.Location, true)
			if err != nil {
				return nil, err
			}
			if recording != nil {
				recordings.remember(recording)
			}
		}
	}
	return recordings, nil
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
