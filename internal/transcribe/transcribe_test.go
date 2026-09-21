package transcribe

import (
	"context"
	"errors"
	"fmt"
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

func TestPendingGroupsDropsFullyTranscribedAndCombinedDates(t *testing.T) {
	transcriptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(transcriptDir, "2026-08-24.txt"), []byte("combined"), 0o644); err != nil {
		t.Fatal(err)
	}
	values := []group{
		// All memos transcribed and already combined: nothing left to do.
		{Course: "MATH351", Date: "2026-08-24", TranscriptDir: transcriptDir, Memos: []Memo{{Part: "01", Status: skipped}}},
		// All memos transcribed but never combined: still pending.
		{Course: "MATH351", Date: "2026-08-25", TranscriptDir: transcriptDir, Memos: []Memo{{Part: "01", Status: skipped}}},
		{Course: "MATH351", Date: "2026-08-26", TranscriptDir: transcriptDir, Memos: []Memo{{Part: "01", Status: skipped}, {Part: "02", Status: waiting}}},
		{Course: "MATH451", Date: "2026-08-27", TranscriptDir: transcriptDir, Memos: []Memo{{Part: "01", Status: waiting}}},
	}
	pending := pendingGroups(values)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending groups, got %d: %+v", len(pending), pending)
	}
	if pending[0].Date != "2026-08-25" || pending[1].Date != "2026-08-26" || pending[2].Date != "2026-08-27" {
		t.Fatalf("unexpected pending groups: %+v", pending)
	}
}

func TestPendingGroupsDropsFullyTranscribedSkipCombineDates(t *testing.T) {
	transcriptDir := t.TempDir()
	values := []group{
		{Course: "MATH351", Date: "2026-08-24", TranscriptDir: transcriptDir, SkipCombine: true, Memos: []Memo{{Part: "01", Status: skipped}}},
	}
	pending := pendingGroups(values)
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending groups, got %d: %+v", len(pending), pending)
	}
}

func TestRunCombinesFullyTranscribedGroupWithoutWhisper(t *testing.T) {
	root := t.TempDir()
	memoDir := filepath.Join(root, "MATH351", "memos")
	transcriptDir := filepath.Join(root, "MATH351", "transcripts")
	if err := os.MkdirAll(memoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoDir, "2026-08-27-pt01.m4a"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDir, "2026-08-27-pt01.txt"), []byte("already transcribed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No mlx_whisper on PATH: Run must not require it since nothing needs
	// transcribing, only combining. Note stdin/stdout are not TTYs under
	// `go test`, so this exercises the non-interactive branch of Run, not
	// the chooseGroups/filterGroups menu path — that's covered separately
	// by TestSelectionMenuCombinesUnselectedFullyTranscribedGroup in
	// select_test.go, which drives the real bubbletea selector.
	t.Setenv("PATH", t.TempDir())
	if err := Run(context.Background(), Options{Root: root, Courses: []string{"MATH351"}, ShowSelectionMenu: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-08-27.txt")); err != nil {
		t.Fatalf("combined transcript was not produced: %v", err)
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
	}{{waiting, ui.Waiting}, {active, ui.Active}, {complete, ui.Complete}, {skipped, ui.Skipped}, {failed, ui.Failed}, {confirming, ui.Confirming}}
	for _, value := range values {
		if value.internal != value.shared {
			t.Fatalf("status %d != shared status %d", value.internal, value.shared)
		}
	}
}

func TestModelForwardsConfirmAnswerToResponseChannel(t *testing.T) {
	values := []group{{Course: "MATH451", Date: "2026-09-10", Memos: []Memo{{Part: "01"}}}}
	m := newModel(values, make(chan event), func() {})
	response := make(chan bool, 1)
	updated, cmd := m.Update(eventMsg{ok: true, value: event{
		Index: 0, Status: confirming, Detail: "Repetition loop detected. Trim it and use this transcript?",
		HasMemoUpdate: true, HasConfirm: true, ConfirmResponse: response,
	}})
	if cmd == nil {
		t.Fatal("expected Update to keep waiting for events after a confirm prompt")
	}
	m = updated.(model)
	if !strings.Contains(m.View().Content, "y trim & use") {
		t.Fatalf("expected confirm prompt in view:\n%s", m.View().Content)
	}
	updated, _ = m.Update(keyMessage("y"))
	m = updated.(model)
	select {
	case accepted := <-response:
		if !accepted {
			t.Fatal("expected 'y' to answer true")
		}
	default:
		t.Fatal("pressing y did not send a response")
	}
	if m.confirmAnswers != nil {
		t.Fatal("confirmAnswers should be cleared once answered")
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
	for _, contents := range []string{"", "  \n\t\n"} {
		empty := filepath.Join(directory, fmt.Sprintf("empty-%d.txt", len(contents)))
		if err := os.WriteFile(empty, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		passes, err = transcriptPassesQualityCheck(empty)
		if err != nil || passes {
			t.Fatalf("blank transcript: passes=%v err=%v", passes, err)
		}
	}
}

func TestTrimRepetitionLoopClearsTheQualityCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.txt")
	contents := "About the changes.\nThank you.\nThank you.\n" + strings.Repeat("Let's prove.\n", 8) + "proper initial segment of that.\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := trimRepetitionLoop(path); err != nil {
		t.Fatal(err)
	}
	trimmed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "About the changes.\nThank you.\nThank you.\nLet's prove.\nproper initial segment of that.\n"
	if string(trimmed) != want {
		t.Fatalf("trimmed = %q, want %q", trimmed, want)
	}
	if passes, err := transcriptPassesQualityCheck(path); err != nil || !passes {
		t.Fatalf("trimmed transcript still fails quality check: passes=%v err=%v", passes, err)
	}
}

func TestRepetitionLoopSampleReportsTheRepeatedLineAndCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.txt")
	contents := "About the changes.\nAll right.\n" + strings.Repeat("Let's prove.\n", 8) + "proper initial segment of that.\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	line, count, err := repetitionLoopSample(path)
	if err != nil {
		t.Fatal(err)
	}
	if line != "Let's prove." || count != 8 {
		t.Fatalf("sample = %q x%d, want %q x8", line, count, "Let's prove.")
	}
}

func TestTruncateForDisplayShortensLongSamplesByRune(t *testing.T) {
	short := truncateForDisplay("hello", 60)
	if short != `"hello"` {
		t.Fatalf("short = %q", short)
	}
	long := truncateForDisplay(strings.Repeat("é", 100), 5)
	if long != `"ééééé"…` {
		t.Fatalf("long = %q", long)
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

func TestProcessGroupsContinuesAfterBadOrphanedPart(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'healthy transcript\n' > "$output_dir/$output_name.txt"
`)
	oldDir := filepath.Join(directory, "old")
	todayDir := filepath.Join(directory, "today")
	for _, path := range []string{oldDir, todayDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldDir, "2026-08-26-pt01.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{
		{Course: "OLD", Date: "2026-08-26", TranscriptDir: oldDir},
		{
			Course: "TODAY", Date: "2026-08-27", TranscriptDir: todayDir, SkipCombine: true,
			Memos: []Memo{{Course: "TODAY", Path: filepath.Join(directory, "today.m4a"), Stem: "2026-08-27-pt01", Status: waiting}},
		},
	}
	err := processGroups(context.Background(), groups, Options{Model: DefaultModel}, func(event) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "empty transcript") || !strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("processGroups error = %v, want orphan transcript failure", err)
	}
	if _, err := os.Stat(filepath.Join(todayDir, "2026-08-27-pt01.txt")); err != nil {
		t.Fatalf("later healthy group did not run: %v", err)
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
	// A failed transcription is reported through the memo's status, not a
	// returned error, so one bad recording doesn't abort the whole run.
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatalf("error = %v", err)
	}
	if value.Memos[0].Status != failed || value.Memos[0].Detail != "error: fake failure" {
		t.Fatalf("memo status = %+v", value.Memos[0])
	}
	contents, readErr := os.ReadFile(destination)
	if readErr != nil || string(contents) != "keep me\n" {
		t.Fatalf("existing transcript = %q, err=%v", contents, readErr)
	}
	if matches, _ := filepath.Glob(filepath.Join(transcriptDir, "*-new-*.txt")); len(matches) != 0 {
		t.Fatalf("temporary transcripts remain: %v", matches)
	}
}

func TestTranscribeMemoRejectsBlankOutput(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
: > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH351", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH351", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-08-25-pt01", Part: "01"}}}
	// A rejected transcript is reported through the memo's status, not a
	// returned error, so one bad recording doesn't abort the whole run.
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatalf("error = %v", err)
	}
	if value.Memos[0].Status != failed || !strings.Contains(value.Memos[0].Detail, "empty transcript") {
		t.Fatalf("memo status = %+v", value.Memos[0])
	}
	if _, statErr := os.Stat(filepath.Join(transcriptDir, "2026-08-25-pt01.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("blank output was published: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(transcriptDir, "2026-08-25-pt01.rejected.txt")); statErr != nil {
		t.Fatalf("rejected output missing: %v", statErr)
	}
}

func TestTranscribeMemoInteractiveAcceptTrimsRepetitionLoop(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
{
  printf 'All right.\n'
  for i in 1 2 3 4 5 6 7 8; do printf "Let's prove.\n"; done
  printf 'proper initial segment of that.\n'
} > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH451", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-09-10-pt01", Part: "01"}}}
	var confirmPrompt string
	emit := func(message event) bool {
		if message.HasConfirm {
			confirmPrompt = message.ConfirmPrompt
			message.ConfirmResponse <- true
		}
		return true
	}
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel, Interactive: true}, emit); err != nil {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(confirmPrompt, "Let's prove.") || !strings.Contains(confirmPrompt, "8x") {
		t.Fatalf("confirm prompt = %q, want it to name the repeated line and count", confirmPrompt)
	}
	if value.Memos[0].Status != complete {
		t.Fatalf("memo status = %+v", value.Memos[0])
	}
	contents, err := os.ReadFile(filepath.Join(transcriptDir, "2026-09-10-pt01.txt"))
	if err != nil {
		t.Fatalf("accepted transcript missing: %v", err)
	}
	want := "All right.\nLet's prove.\nproper initial segment of that.\n"
	if string(contents) != want {
		t.Fatalf("transcript = %q, want %q", contents, want)
	}
	if _, statErr := os.Stat(filepath.Join(transcriptDir, "2026-09-10-pt01.rejected.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected file should not exist after accepting the trim: %v", statErr)
	}
}

func TestTranscribeMemoInteractiveDeclineKeepsRejection(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
{
  printf 'All right.\n'
  for i in 1 2 3 4 5 6 7 8; do printf "Let's prove.\n"; done
  printf 'proper initial segment of that.\n'
} > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH451", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-09-10-pt01", Part: "01"}}}
	emit := func(message event) bool {
		if message.HasConfirm {
			message.ConfirmResponse <- false
		}
		return true
	}
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel, Interactive: true}, emit); err != nil {
		t.Fatalf("error = %v", err)
	}
	if value.Memos[0].Status != failed || !strings.Contains(value.Memos[0].Detail, "repetition loop") {
		t.Fatalf("memo status = %+v", value.Memos[0])
	}
	if _, statErr := os.Stat(filepath.Join(transcriptDir, "2026-09-10-pt01.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("declined transcript should not be published: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(transcriptDir, "2026-09-10-pt01.rejected.txt")); statErr != nil {
		t.Fatalf("rejected output missing: %v", statErr)
	}
}

func TestTranscribeMemoConfirmationCancellationDoesNotHang(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
{
  printf 'All right.\n'
  for i in 1 2 3 4 5 6 7 8; do printf "Let's prove.\n"; done
} > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH451", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-09-10-pt01", Part: "01"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Nobody ever answers the prompt (as if the user walked away, or the
	// run was cancelled mid-question) - the wait must still return instead
	// of blocking forever.
	emit := func(event) bool { return true }
	started := time.Now()
	err := transcribeMemo(ctx, &value, 0, Options{Model: DefaultModel, Interactive: true}, emit)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestTranscribeMemoNonInteractiveNeverPromptsForRepetitionLoop(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
{
  printf 'All right.\n'
  for i in 1 2 3 4 5 6 7 8; do printf "Let's prove.\n"; done
} > "$output_dir/$output_name.txt"
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{Course: "MATH451", TranscriptDir: transcriptDir, Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "memo.m4a"), Stem: "2026-09-10-pt01", Part: "01"}}}
	emit := func(message event) bool {
		if message.HasConfirm {
			t.Fatal("non-interactive runs must not prompt for confirmation")
		}
		return true
	}
	// Options.Interactive left false, as it is for any non-terminal run.
	if err := transcribeMemo(context.Background(), &value, 0, Options{Model: DefaultModel}, emit); err != nil {
		t.Fatalf("error = %v", err)
	}
	if value.Memos[0].Status != failed || !strings.Contains(value.Memos[0].Detail, "repetition loop") {
		t.Fatalf("memo status = %+v", value.Memos[0])
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

func TestProcessGroupMarksOperationalTranscriptionErrorFailed(t *testing.T) {
	directory := t.TempDir()
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mlx_whisper"), []byte("#!/definitely/missing/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{
		Course: "MATH351", Date: "2026-08-25", TranscriptDir: transcriptDir, SkipCombine: true,
		Memos: []Memo{
			{Course: "MATH351", Path: filepath.Join(directory, "2026-08-25-pt01.m4a"), Stem: "2026-08-25-pt01", Part: "01"},
			{Course: "MATH351", Path: filepath.Join(directory, "2026-08-25-pt02.m4a"), Stem: "2026-08-25-pt02", Part: "02"},
		},
	}
	hadFailedEvent := false
	err := processGroup(context.Background(), &value, Options{Model: DefaultModel}, func(message event) bool {
		hadFailedEvent = hadFailedEvent || message.HasMemoUpdate && message.Status == failed
		return true
	})
	if err == nil || value.Memos[0].Status != failed || value.Memos[0].Detail == "" || !hadFailedEvent {
		t.Fatalf("operational error: err=%v memo=%+v failedEvent=%v", err, value.Memos[0], hadFailedEvent)
	}
	// The failure must not stop later parts of the same date from running.
	if value.Memos[1].Status != failed {
		t.Fatalf("second part was not attempted after an operational error: %+v", value.Memos[1])
	}
	for _, want := range []string{"2026-08-25-pt01.m4a", "2026-08-25-pt02.m4a", "retry with: lectr transcribe MATH351 2026-08-25"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("operational error %q missing %q", err, want)
		}
	}
}

func TestProcessGroupsReportsEachFailedRecordingWithItsRetry(t *testing.T) {
	stuck, rejected := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(stuck, "2026-08-25-pt01.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{
		{Course: "MATH351", Date: "2026-08-25", TranscriptDir: stuck, Memos: []Memo{
			{Path: "2026-08-25-pt01.m4a", Stem: "2026-08-25-pt01", Status: failed, Detail: "empty transcript"},
			{Path: "2026-08-25-pt02.m4a", Stem: "2026-08-25-pt02", Status: failed, Detail: "Rejected repetition loop"},
		}},
		{Course: "MATH451", Date: "2026-09-10", TranscriptDir: rejected, Memos: []Memo{
			{Path: "2026-09-10-pt01.m4a", Stem: "2026-09-10-pt01", Status: failed, Detail: "Rejected repetition loop"},
		}},
	}
	err := processGroups(context.Background(), groups, Options{}, func(event) bool { return true })
	var batch *BatchError
	if !errors.As(err, &batch) || len(batch.Failures) != 3 {
		t.Fatalf("processGroups error = %#v, want a BatchError with one entry per failed recording", err)
	}
	want := []string{
		"MATH351: 2026-08-25-pt01.m4a: empty transcript; replace it with: lectr transcribe MATH351 2026-08-25 --force",
		"MATH351: 2026-08-25-pt02.m4a: Rejected repetition loop; retry with: lectr transcribe MATH351 2026-08-25",
		"MATH451: 2026-09-10-pt01.m4a: Rejected repetition loop; retry with: lectr transcribe MATH451 2026-09-10",
	}
	for index, failure := range batch.Failures {
		if failure.Error() != want[index] {
			t.Errorf("failure %d = %q\nwant        %q", index, failure.Error(), want[index])
		}
	}
}

func TestProcessGroupsContinuesPastARejectedGroup(t *testing.T) {
	directory := t.TempDir()
	installFakeWhisper(t, directory, `
audio_path="$1"
shift
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "$audio_path" in
  *bad*) printf 'loop\nloop\nloop\nloop\nloop\nloop\nloop\n' > "$output_dir/$output_name.txt" ;;
  *) printf 'a valid transcript\n' > "$output_dir/$output_name.txt" ;;
esac
`)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	groups := []group{
		{Course: "MATH451", Date: "2026-09-10", TranscriptDir: transcriptDir,
			Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "bad.m4a"), Stem: "2026-09-10-pt01", Part: "01"}}},
		{Course: "MATH451", Date: "2026-09-15", TranscriptDir: transcriptDir,
			Memos: []Memo{{Course: "MATH451", Path: filepath.Join(directory, "good.m4a"), Stem: "2026-09-15-pt01", Part: "01"}}},
	}
	// One rejected recording must not stop the rest of the batch, but the
	// final result still reports that the batch was only partially successful.
	if err := processGroups(context.Background(), groups, Options{Model: DefaultModel}, func(event) bool { return true }); err == nil || !strings.Contains(err.Error(), "repetition loop") {
		t.Fatalf("processGroups error = %v, want aggregate rejection after continuing", err)
	}
	if groups[0].Memos[0].Status != failed {
		t.Fatalf("expected the bad recording to be marked failed, got %+v", groups[0].Memos[0])
	}
	if groups[1].Memos[0].Status != complete {
		t.Fatalf("expected the second group to still be transcribed, got %+v", groups[1].Memos[0])
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-09-15-pt01.txt")); err != nil {
		t.Fatalf("second group's transcript was not produced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-09-10.txt")); !os.IsNotExist(err) {
		t.Fatalf("combined transcript should not be published for a group with a failed part: %v", err)
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
