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
	LaunchAgentLabel = "local.lectr.route"
	MinimumOverlap   = 5 * time.Minute
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
	if err != nil || !info.IsDir() {
		return 0, fmt.Errorf("Voice Memos directory not found: %s", options.Source)
	}
	known, err := existingUUIDs(ctx, options)
	if err != nil {
		return 0, err
	}
	paths, err := filepath.Glob(filepath.Join(options.Source, "*.m4a"))
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

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

func launchLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "lectr.log")
}

func launchAgentConfigFor(configPath, source, executable string) (string, error) {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	escape := func(value string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--config</string><string>%s</string><string>route</string><string>--quiet</string></array>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string></dict>
<key>WatchPaths</key><array><string>%s</string></array><key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Background</string>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, LaunchAgentLabel, escape(executable), escape(configPath), escape(source), escape(launchLogPath()), escape(launchLogPath())), nil
}

// InstallWatcher installs and starts the launchd watcher, returning the
// source directory watched and the log path so callers can render them.
func InstallWatcher(configPath, source string) (string, string, error) {
	executable, err := stableExecutable()
	if err != nil {
		return "", "", err
	}
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	config, err := launchAgentConfigFor(configPath, source, executable)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		return "", "", err
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", uid, path).Run()
	if err := exec.Command("launchctl", "bootstrap", uid, path).Run(); err != nil {
		return "", "", err
	}
	if err := exec.Command("launchctl", "kickstart", uid+"/"+LaunchAgentLabel).Run(); err != nil {
		return "", "", err
	}
	return source, launchLogPath(), nil
}

func stableExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	return validateStableExecutable(executable)
}

func validateStableExecutable(executable string) (string, error) {
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("lectr executable is not a runnable regular file: %s", executable)
	}
	for _, component := range strings.Split(filepath.Clean(executable), string(filepath.Separator)) {
		if strings.HasPrefix(component, "go-build") {
			return "", errors.New("cannot install from go run; run `make build`, then `./lectr watch install`")
		}
	}
	return executable, nil
}

func UninstallWatcher() error {
	path := launchAgentPath()
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", uid, path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Uninstalled %s\n", LaunchAgentLabel)
	return nil
}

func WatcherStatus() (bool, string) {
	path := launchAgentPath()
	_, err := os.Stat(path)
	return err == nil, path
}
