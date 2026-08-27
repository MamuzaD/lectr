package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mamuzad/lectr/internal/ui"
)

func TestValidateSelector(t *testing.T) {
	valid := []string{"", "2026-08-25", "2026-08-25-pt01.m4a", "2026-08-25-pt99.wav"}
	for _, value := range valid {
		if !ValidateSelector(value) {
			t.Errorf("expected selector %q to be valid", value)
		}
	}
	for _, value := range []string{"2026-8-25", "MATH351", "2026-08-25-pt1.m4a", "2026-08-25.txt"} {
		if ValidateSelector(value) {
			t.Errorf("expected selector %q to be invalid", value)
		}
	}
}

func TestPendingGroupsDropsFullyTranscribedDates(t *testing.T) {
	values := []group{
		{Course: "MATH351", Date: "2026-08-24", Memos: []Memo{{Part: "01", Status: skipped}}},
		{Course: "MATH351", Date: "2026-08-25", Memos: []Memo{{Part: "01", Status: skipped}, {Part: "02", Status: waiting}}},
		{Course: "MATH451", Date: "2026-08-26", Memos: []Memo{{Part: "01", Status: waiting}}},
	}
	pending := pendingGroups(values)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending groups, got %d: %+v", len(pending), pending)
	}
	if pending[0].Date != "2026-08-25" || pending[1].Date != "2026-08-26" {
		t.Fatalf("unexpected pending groups: %+v", pending)
	}
}

func TestInventoryCountsOnlyNamedMemosWithoutTranscripts(t *testing.T) {
	root := t.TempDir()
	memos := filepath.Join(root, "MATH351", "memos")
	transcripts := filepath.Join(root, "MATH351", "transcripts")
	if err := os.MkdirAll(memos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(transcripts, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-08-25-pt01.m4a", "2026-08-25-pt02.wav", "notes.m4a"} {
		if err := os.WriteFile(filepath.Join(memos, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(transcripts, "2026-08-25-pt01.txt"), []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}
	counts, err := Inventory(root, "MATH351")
	if err != nil {
		t.Fatal(err)
	}
	if counts.Recordings != 2 || counts.Pending != 1 {
		t.Fatalf("counts = %#v", counts)
	}
}

func TestWhisperCommandMatchesPythonCLI(t *testing.T) {
	memo := Memo{Course: "MATH451", Path: "/memo.m4a"}
	got := whisperCommand(memo, "/transcripts", "lecture-new-123", "model-name", "logic lecture")
	want := []string{
		"mlx_whisper", "/memo.m4a", "--model", "model-name", "--language", "en",
		"--initial-prompt", "logic lecture", "--condition-on-previous-text", "False",
		"--word-timestamps", "True", "--hallucination-silence-threshold", "2",
		"--output-dir", "/transcripts", "--output-name", "lecture-new-123",
		"--output-format", "txt", "--verbose", "False",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestParseProgress(t *testing.T) {
	memo := Memo{Detail: "Starting Whisper"}
	parseProgress("\x1b[2KDownloading bytes: 63%| stuff", &memo)
	if memo.Percent != 63 || memo.Detail != "Downloading model" {
		t.Fatalf("download progress = %d %q", memo.Percent, memo.Detail)
	}
	parseProgress("100%| 309341/309341 [frames/s]", &memo)
	if memo.Percent != 100 || memo.Detail != "Transcribing" {
		t.Fatalf("transcribe progress = %d %q", memo.Percent, memo.Detail)
	}
}

func TestStatusValuesStayInSyncWithSharedUI(t *testing.T) {
	values := []struct {
		internal memoStatus
		shared   ui.Status
	}{{waiting, ui.Waiting}, {active, ui.Active}, {complete, ui.Complete}, {skipped, ui.Skipped}, {failed, ui.Failed}}
	for _, value := range values {
		if value.internal != value.shared {
			t.Fatalf("status %d != shared status %d", value.internal, value.shared)
		}
	}
}

func TestQueueModelKeepsCompletedGroupsVisible(t *testing.T) {
	values := []group{
		{Course: "MATH351", Date: "2026-08-25", Memos: []Memo{{Part: "01"}}},
		{Course: "MATH451", Date: "2026-08-25", Memos: []Memo{{Part: "01"}}},
	}
	m := newModel(values, make(chan event), func() {})
	m.groups[0].Memos[0].Status, m.combines[0] = complete, complete
	m.current = 1
	view := m.View().Content
	for _, want := range []string{"MATH351", "1 recording transcribed", "MATH451"} {
		if !strings.Contains(view, want) {
			t.Errorf("queue view missing %q:\n%s", want, view)
		}
	}
}

func TestFinishedQueueCollapsesFinalGroup(t *testing.T) {
	values := []group{{
		Course: "MATH451", Date: "2026-08-25",
		Memos: []Memo{{Part: "01", Status: skipped}},
	}}
	m := newModel(values, make(chan event), func() {})
	m.finished = true
	view := m.View().Content
	if !strings.Contains(view, "1 recording already transcribed") {
		t.Fatalf("finished queue did not show settled receipt:\n%s", view)
	}
	if strings.Contains(view, "pt01") || strings.Contains(view, "Combined") {
		t.Fatalf("finished queue retained detailed card:\n%s", view)
	}
}

func TestTranscriptPassesQualityCheck(t *testing.T) {
	directory := t.TempDir()
	clean := filepath.Join(directory, "clean.txt")
	if err := os.WriteFile(clean, []byte("one\ntwo\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	passes, err := transcriptPassesQualityCheck(clean)
	if err != nil || !passes {
		t.Fatalf("clean transcript: passes=%v err=%v", passes, err)
	}
	loop := filepath.Join(directory, "loop.txt")
	if err := os.WriteFile(loop, []byte(strings.Repeat("same sentence\n", 8)), 0o644); err != nil {
		t.Fatal(err)
	}
	passes, err = transcriptPassesQualityCheck(loop)
	if err != nil || passes {
		t.Fatalf("loop transcript: passes=%v err=%v", passes, err)
	}
}

func TestTranscriptQualityCheckSupportsLongLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("mathematics ", 100_000)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	passes, err := transcriptPassesQualityCheck(path)
	if err != nil || !passes {
		t.Fatalf("long transcript: passes=%v err=%v", passes, err)
	}
}

func TestCombinePartsIsAtomicAndOrdered(t *testing.T) {
	directory := t.TempDir()
	for _, part := range []struct{ name, body string }{{"2026-08-25-pt02.txt", "second"}, {"2026-08-25-pt01.txt", "first"}} {
		if err := os.WriteFile(filepath.Join(directory, part.name), []byte(part.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path, err := combineParts("MATH351", "2026-08-25", directory)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "===== Part 01 =====\n\nfirst\n\n===== Part 02 =====\n\nsecond\n\n"
	if string(contents) != want {
		t.Fatalf("combined transcript = %q, want %q", contents, want)
	}
}

func TestRunRejectsCourseTraversalBeforeWriting(t *testing.T) {
	root := t.TempDir()
	err := Run(context.Background(), Options{Root: root, Courses: []string{"../outside"}, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "invalid course") {
		t.Fatalf("Run error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "outside")); !os.IsNotExist(statErr) {
		t.Fatalf("unexpected outside path: %v", statErr)
	}
}

func TestTranscribeMemoSuccessUsesTemporaryOutput(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'Downloading bytes: 63%%|\r100%%| frames/s\n'
printf 'a valid transcript\n' > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH351", Date: "2026-08-25", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH351", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-08-25-pt01", Part: "01"}}}
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(transcriptDir, "2026-08-25-pt01.txt"))
	if err != nil || string(contents) != "a valid transcript\n" {
		t.Fatalf("transcript = %q, err=%v", contents, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(transcriptDir, "*-new-*.txt")); len(matches) != 0 {
		t.Fatalf("temporary transcripts remain: %v", matches)
	}
}

func TestTranscribeMemoFailurePreservesExistingAndCleansTemporary(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'partial\n' > "$output_dir/$output_name.txt"
printf 'error: fake failure\n'
exit 7
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(transcriptDir, "2026-08-25-pt01.txt")
	if err := os.WriteFile(destination, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH351", Date: "2026-08-25", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH351", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-08-25-pt01", Part: "01"}}}
	err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, func(event) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "transcription failed") {
		t.Fatalf("error = %v", err)
	}
	contents, readErr := os.ReadFile(destination)
	if readErr != nil || string(contents) != "keep me\n" {
		t.Fatalf("existing transcript = %q, err=%v", contents, readErr)
	}
	if matches, _ := filepath.Glob(filepath.Join(transcriptDir, "*-new-*.txt")); len(matches) != 0 {
		t.Fatalf("temporary transcripts remain: %v", matches)
	}
}

func TestTranscribeMemoCancellationStopsProcess(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, "sleep 30\n")
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH351", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH351", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-08-25-pt01", Part: "01"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := transcribeMemo(ctx, &value, 0, Options{Model: DefaultModel}, func(event) bool { return true })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func installFakeWhisper(t *testing.T, directory, body string) {
	t.Helper()
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "mlx_whisper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
