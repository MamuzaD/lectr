package watch

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMatchingClassUsesOverlap(t *testing.T) {
	started := time.Date(2026, 8, 25, 11, 40, 0, 0, time.Local)
	meeting := MatchingClass(Recording{Started: started, Duration: 20 * time.Minute}, testSchedule())
	if meeting == nil || meeting.Course != "MATH351" {
		t.Fatalf("meeting = %#v, want MATH351", meeting)
	}
	outside := MatchingClass(Recording{Started: time.Date(2026, 8, 25, 13, 0, 0, 0, time.Local), Duration: 4 * time.Minute}, testSchedule())
	if outside != nil {
		t.Fatalf("short non-overlap matched %#v", outside)
	}
	weekend := MatchingClass(Recording{Started: time.Date(2026, 8, 30, 11, 30, 0, 0, time.Local), Duration: time.Hour}, testSchedule())
	if weekend != nil {
		t.Fatalf("weekend recording matched %#v", weekend)
	}
}

func TestMatchingClassIncludesExactFiveMinuteBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		started  time.Time
		duration time.Duration
		want     bool
	}{
		{"ends five minutes into class", time.Date(2026, 8, 25, 11, 25, 0, 0, time.Local), 10 * time.Minute, true},
		{"starts five minutes before end", time.Date(2026, 8, 25, 12, 40, 0, 0, time.Local), 5 * time.Minute, true},
		{"ends one second short", time.Date(2026, 8, 25, 11, 25, 0, 0, time.Local), 9*time.Minute + 59*time.Second, false},
		{"starts at class end", time.Date(2026, 8, 25, 12, 45, 0, 0, time.Local), time.Hour, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MatchingClass(Recording{Started: test.started, Duration: test.duration}, testSchedule()) != nil
			if got != test.want {
				t.Fatalf("matched = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMatchingClassUsesConfiguredDay(t *testing.T) {
	schedule := Schedule{
		Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local),
		End:   time.Date(2026, 5, 1, 23, 59, 59, 0, time.Local),
		Meetings: []ClassMeeting{
			{Course: "HIST200", Start: 9 * 60, End: 10 * 60, Days: []time.Weekday{time.Wednesday}},
		},
	}
	recording := Recording{Started: time.Date(2026, 1, 7, 9, 15, 0, 0, time.Local), Duration: 30 * time.Minute}
	if meeting := MatchingClass(recording, schedule); meeting == nil || meeting.Course != "HIST200" {
		t.Fatalf("meeting = %#v", meeting)
	}
}

func TestParseCreationTimeUsesLocalTimezone(t *testing.T) {
	zone := time.Local
	for _, value := range []string{"2026-08-25T18:40:00Z", "2026-08-25T11:40:00"} {
		parsed, err := parseCreationTime(value, zone)
		if err != nil {
			t.Fatal(err)
		}
		if value[len(value)-1] != 'Z' && parsed.Location() != zone {
			t.Fatalf("location = %s, want %s", parsed.Location(), zone)
		}
	}
}

func TestNextDestinationUsesPtNN(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "MATH351", "memos")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-08-25-pt01.m4a", "2026-08-25-pt02.m4a", "2026-08-25-pt07.m4a", "2026-08-25-ptbad.m4a"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(directory, "2026-08-25-pt08.m4a")
	if got := NextDestination(root, "MATH351", time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local)); got != want {
		t.Fatalf("destination = %s, want %s", got, want)
	}
}

func TestNextDestinationReservesDryRunParts(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "MATH351", "memos")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "2026-08-25-pt01.m4a"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local)
	reserved := make(map[string]bool)
	first, err := nextDestination(root, "MATH351", date, reserved)
	if err != nil {
		t.Fatal(err)
	}
	reserved[first] = true
	second, err := nextDestination(root, "MATH351", date, reserved)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "2026-08-25-pt02.m4a" || filepath.Base(second) != "2026-08-25-pt03.m4a" {
		t.Fatalf("reserved destinations = %s, %s", first, second)
	}
}

func TestNextDestinationStopsAtPt99(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "MATH351", "memos")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "2026-08-25-pt99.m4a"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := nextDestination(root, "MATH351", time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local), nil)
	if err == nil || !strings.Contains(err.Error(), "no part numbers") {
		t.Fatalf("error = %v", err)
	}
}

func TestCopyAtomically(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.m4a")
	destination := filepath.Join(directory, "nested", "destination.m4a")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(strings.Repeat("audio", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2025, 5, 4, 3, 2, 1, 0, time.Local)
	if err := os.Chmod(source, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	if err := CopyAtomically(source, destination); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || len(contents) != 500 {
		t.Fatalf("copied contents len=%d err=%v", len(contents), err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 || !info.ModTime().Equal(wantTime) {
		t.Fatalf("metadata = %o %s", info.Mode().Perm(), info.ModTime())
	}
}

func TestRouteDeduplicatesUUIDsAndNumbersParts(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "semester")
	source := filepath.Join(directory, "source")
	memos := filepath.Join(root, "MATH351", "memos")
	for _, path := range []string{source, memos, filepath.Join(root, "MATH451", "memos")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(memos, "2026-08-25-pt01.m4a"): "existing",
		filepath.Join(source, "duplicate.m4a"):      "duplicate",
		filepath.Join(source, "new-a.m4a"):          "new-a",
		filepath.Join(source, "new-b.m4a"):          "new-b",
	}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installFakeFFprobe(t, directory)
	copied, err := routeRecordings(context.Background(), Options{Root: root, Source: source, Location: time.Local, Schedule: testSchedule(), Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if copied != 2 {
		t.Fatalf("copied = %d, want 2", copied)
	}
	for part, contents := range map[string]string{"2026-08-25-pt02.m4a": "new-a", "2026-08-25-pt03.m4a": "new-b"} {
		got, err := os.ReadFile(filepath.Join(memos, part))
		if err != nil || string(got) != contents {
			t.Fatalf("%s = %q, err=%v", part, got, err)
		}
	}
}

func TestLaunchAgentConfigEmbedsAbsoluteConfigAndSource(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	source := filepath.Join(directory, "Recordings")
	executable := filepath.Join(directory, "lectr")
	config, err := launchAgentConfigFor(configPath, source, executable)
	if err != nil {
		t.Fatal(err)
	}
	arguments := "<string>" + executable + "</string><string>route</string><string>--config</string><string>" + configPath + "</string><string>--quiet</string>"
	if !strings.Contains(config, arguments) || !strings.Contains(config, source) {
		t.Fatalf("launch config does not embed config and source: %s", config)
	}
	decoder := xml.NewDecoder(strings.NewReader(config))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("invalid plist XML: %v", err)
		}
	}
}

func TestInstallWatcherRemovesNewPlistWhenBootstrapFails(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	executable := filepath.Join(directory, "lectr")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := func(arguments ...string) error {
		if arguments[0] == "bootstrap" {
			return errors.New("bootstrap failed")
		}
		return nil
	}
	if _, _, err := installWatcher(filepath.Join(directory, "config.json"), filepath.Join(directory, "Recordings"), executable, runner); err == nil {
		t.Fatal("expected bootstrap failure")
	}
	if _, err := os.Stat(launchAgentPath()); !os.IsNotExist(err) {
		t.Fatalf("failed install left plist behind: %v", err)
	}
}

func TestInstallWatcherRestoresPreviousPlistWhenKickstartFails(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "lectr")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := func(arguments ...string) error {
		if arguments[0] == "kickstart" {
			return errors.New("kickstart failed")
		}
		return nil
	}
	if _, _, err := installWatcher(filepath.Join(directory, "config.json"), filepath.Join(directory, "Recordings"), executable, runner); err == nil {
		t.Fatal("expected kickstart failure")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "previous" {
		t.Fatalf("previous plist = %q, err=%v", contents, err)
	}
}

func TestWatcherStatusUsesLoadedLaunchdService(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, gotPath := watcherStatus(func(arguments ...string) error {
		if len(arguments) != 2 || arguments[0] != "print" || !strings.HasSuffix(arguments[1], "/"+LaunchAgentLabel) {
			t.Fatalf("launchctl arguments = %v", arguments)
		}
		return errors.New("not loaded")
	})
	if loaded || gotPath != path {
		t.Fatalf("loaded=%v path=%q, want false %q", loaded, gotPath, path)
	}
}

func testSchedule() Schedule {
	return Schedule{
		Start: time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local),
		End:   time.Date(2026, 12, 18, 23, 59, 59, 0, time.Local),
		Meetings: []ClassMeeting{
			{Course: "MATH351", Start: 11*60 + 30, End: 12*60 + 45, Days: []time.Weekday{time.Tuesday, time.Thursday}},
			{Course: "MATH451", Start: 14*60 + 30, End: 15*60 + 45, Days: []time.Weekday{time.Tuesday, time.Thursday}},
		},
	}
}

func TestValidateStableExecutableRejectsGoRunAndAcceptsBuiltBinary(t *testing.T) {
	directory := t.TempDir()
	stable := filepath.Join(directory, "watch")
	if err := os.WriteFile(stable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := validateStableExecutable(stable); err != nil || got != stable {
		t.Fatalf("stable executable = %q, %v", got, err)
	}
	goRunDirectory := filepath.Join(directory, "go-build123", "b001", "exe")
	if err := os.MkdirAll(goRunDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	goRun := filepath.Join(goRunDirectory, "watch")
	if err := os.WriteFile(goRun, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := validateStableExecutable(goRun); err == nil || !strings.Contains(err.Error(), "make build") {
		t.Fatalf("go run executable error = %v", err)
	}
}

func installFakeFFprobe(t *testing.T, directory string) {
	t.Helper()
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
for last do :; done
case "${last##*/}" in
  2026-08-25-pt01.m4a|duplicate.m4a) uuid="duplicate-uuid" ;;
  new-a.m4a) uuid="new-a-uuid" ;;
  new-b.m4a) uuid="new-b-uuid" ;;
  *) exit 1 ;;
esac
printf '{"format":{"duration":"4200.0","tags":{"creation_time":"2026-08-25T11:40:00","voice-memo-uuid":"%s"}}}\n' "$uuid"
`
	path := filepath.Join(bin, "ffprobe")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
