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

func TestRouteHandlesQTAWithoutBroadeningUntaggedM4A(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "semester")
	source := filepath.Join(directory, "source")
	memos := filepath.Join(root, "MATH351", "memos")
	for _, path := range []string{source, memos} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{
		"cloud.qta": "cloud-partial", "native-a.m4a": "native-a", "native-b.m4a": "native-b",
		"pair.m4a": "pair", "pair.qta": "pair", "untagged.m4a": "untagged",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installQTAFFprobe(t, directory, "600.0")
	options := Options{Root: root, Source: source, Location: time.Local, Schedule: testSchedule(), Quiet: true}
	copied, err := routeRecordings(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if copied != 4 {
		t.Fatalf("copied = %d, want cloud qta, two native UUIDs, and one representation of pair", copied)
	}
	transcriptDir := filepath.Join(root, "MATH351", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-08-25-pt01.txt", "2026-08-25.txt"} {
		if err := os.WriteFile(filepath.Join(transcriptDir, name), []byte("stale transcript\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "cloud.qta"), []byte("cloud-complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	installQTAFFprobe(t, directory, "1200.0")
	copied, err = routeRecordings(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if copied != 1 {
		t.Fatalf("duration growth updates = %d, want 1 existing part update", copied)
	}
	contents, err := os.ReadFile(filepath.Join(memos, "2026-08-25-pt01.m4a"))
	if err != nil || string(contents) != "cloud-complete" {
		t.Fatalf("updated qta destination = %q, err=%v", contents, err)
	}
	for _, stem := range []string{"2026-08-25-pt01", "2026-08-25"} {
		if _, err := os.Stat(filepath.Join(transcriptDir, stem+".txt")); !os.IsNotExist(err) {
			t.Fatalf("updated audio retained stale %s.txt: %v", stem, err)
		}
		kept, err := os.ReadFile(filepath.Join(transcriptDir, stem+".superseded.txt"))
		if err != nil || string(kept) != "stale transcript\n" {
			t.Fatalf("old %s transcript was not set aside: %q, err=%v", stem, kept, err)
		}
	}
}

func TestRouteFailedQTAUpdateKeepsTranscripts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	directory := t.TempDir()
	root := filepath.Join(directory, "semester")
	source := filepath.Join(directory, "source")
	memos := filepath.Join(root, "MATH351", "memos")
	transcriptDir := filepath.Join(root, "MATH351", "transcripts")
	for _, path := range []string{source, memos, transcriptDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "cloud.qta"), []byte("cloud-partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	installQTAFFprobe(t, directory, "600.0")
	options := Options{Root: root, Source: source, Location: time.Local, Schedule: testSchedule(), Quiet: true}
	if _, err := routeRecordings(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-08-25-pt01.txt", "2026-08-25.txt"} {
		if err := os.WriteFile(filepath.Join(transcriptDir, name), []byte("hand-edited transcript\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "cloud.qta"), []byte("cloud-complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	installQTAFFprobe(t, directory, "1200.0")
	// A read-only memos directory makes the atomic copy fail after the
	// transcripts have already been set aside.
	if err := os.Chmod(memos, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(memos, 0o755) })
	if _, err := routeRecordings(context.Background(), options); err == nil {
		t.Fatal("update into a read-only memos directory succeeded")
	}
	audio, err := os.ReadFile(filepath.Join(memos, "2026-08-25-pt01.m4a"))
	if err != nil || string(audio) != "cloud-partial" {
		t.Fatalf("routed audio = %q, err=%v; want the original partial copy", audio, err)
	}
	for _, stem := range []string{"2026-08-25-pt01", "2026-08-25"} {
		contents, err := os.ReadFile(filepath.Join(transcriptDir, stem+".txt"))
		if err != nil || string(contents) != "hand-edited transcript\n" {
			t.Fatalf("failed update lost %s.txt: %q, err=%v", stem, contents, err)
		}
		if _, err := os.Stat(filepath.Join(transcriptDir, stem+".superseded.txt")); !os.IsNotExist(err) {
			t.Fatalf("failed update left %s.superseded.txt behind: %v", stem, err)
		}
	}
}

func TestRoutePrefersLaterLongerNativeRecordingOverSyntheticQTA(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "semester")
	source := filepath.Join(directory, "source")
	memos := filepath.Join(root, "MATH351", "memos")
	for _, path := range []string{source, memos} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{"a-cloud.qta": "cloud-partial", "z-native.m4a": "cloud-native"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installQTAFFprobe(t, directory, "1200.0")
	copied, err := routeRecordings(context.Background(), Options{
		Root: root, Source: source, Location: time.Local, Schedule: testSchedule(), Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if copied != 2 {
		t.Fatalf("copy/update count = %d, want initial qta plus native replacement", copied)
	}
	contents, err := os.ReadFile(filepath.Join(memos, "2026-08-25-pt01.m4a"))
	if err != nil || string(contents) != "cloud-native" {
		t.Fatalf("routed pair = %q, err=%v; partial qta won", contents, err)
	}
}

func TestRoutePreservesSourceStatErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "Recordings")
	_, err := routeRecordings(context.Background(), Options{Root: t.TempDir(), Source: missing})
	if err == nil {
		t.Fatal("expected missing source error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source error does not preserve os.ErrNotExist: %v", err)
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
	for _, unexpected := range []string{"<key>KeepAlive</key>", "<key>ThrottleInterval</key>"} {
		if strings.Contains(config, unexpected) {
			t.Fatalf("launch config contains persistent retry setting %q: %s", unexpected, config)
		}
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
	state := watcherStatus(func(arguments ...string) error {
		if len(arguments) != 2 || arguments[0] != "print" || !strings.HasSuffix(arguments[1], "/"+LaunchAgentLabel) {
			t.Fatalf("launchctl arguments = %v", arguments)
		}
		return errors.New("not loaded")
	})
	if state.Enabled || state.AgentPath != path || state.LogPath != launchLogPath() {
		t.Fatalf("state=%+v, want disabled with agent %q", state, path)
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
  2026-08-25-pt01.m4a|duplicate.m4a) uuid="duplicate-uuid"; created="2026-08-25T11:40:00" ;;
  new-a.m4a) uuid="new-a-uuid"; created="2026-08-25T11:41:00" ;;
  new-b.m4a) uuid="new-b-uuid"; created="2026-08-25T11:42:00" ;;
  *) exit 1 ;;
esac
printf '{"format":{"duration":"4200.0","tags":{"creation_time":"%s","voice-memo-uuid":"%s"}}}\n' "$created" "$uuid"
`
	path := filepath.Join(bin, "ffprobe")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installQTAFFprobe(t *testing.T, directory, duration string) {
	t.Helper()
	bin := filepath.Join(directory, "qta-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
for last do :; done
value=$(cat "$last")
duration="` + duration + `"
case "$value" in
  cloud-partial) created="2026-08-25T11:40:00"; uuid=""; duration="600.0" ;;
  cloud-complete) created="2026-08-25T11:40:00"; uuid="" ;;
  cloud-native) created="2026-08-25T11:40:00"; uuid="cloud-native-uuid" ;;
  native-a) created="2026-08-25T11:42:00"; uuid="native-a-uuid" ;;
  native-b) created="2026-08-25T11:42:00"; uuid="native-b-uuid" ;;
  pair)
    created="2026-08-25T11:41:00"
    case "$last" in *.qta) uuid="" ;; *) uuid="pair-uuid" ;; esac
    ;;
  untagged) created="2026-08-25T11:42:00"; uuid="" ;;
  *) exit 1 ;;
esac
printf '{"format":{"duration":"%s","tags":{"creation_time":"%s","voice-memo-uuid":"%s"}}}\n' "$duration" "$created" "$uuid"
`
	path := filepath.Join(bin, "ffprobe")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSetAsideTranscriptsNeverOverwritesAnEarlierSetAside(t *testing.T) {
	root := t.TempDir()
	memo := filepath.Join(root, "MATH351", "memos", "2026-08-25-pt01.m4a")
	transcripts := filepath.Join(root, "MATH351", "transcripts")
	if err := os.MkdirAll(transcripts, 0o755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(transcripts, "2026-08-25-pt01.txt")
	for _, version := range []string{"hand-edited v1\n", "v2\n"} {
		if err := os.WriteFile(part, []byte(version), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := setAsideTranscripts(memo); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"2026-08-25-pt01.superseded.txt":   "hand-edited v1\n",
		"2026-08-25-pt01.superseded-2.txt": "v2\n",
	} {
		got, err := os.ReadFile(filepath.Join(transcripts, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, err=%v; want %q", name, got, err, want)
		}
	}
}
