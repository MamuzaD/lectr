package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestSelectionModelMapsGroupsToSharedSelector(t *testing.T) {
	model := newSelectionModel(selectionTestGroups(), "2026-08-27")
	view := model.View().Content
	for _, want := range []string{
		"3 recordings ready", "about 1h 35m audio", "Catch up everything",
		"Today only", "2 recordings", "Choose recordings",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("selection menu missing %q:\n%s", want, view)
		}
	}
}

func TestSelectionMenuStillShowsWhenEverythingIsToday(t *testing.T) {
	view := newSelectionModel(selectionTestGroups()[1:], "2026-08-27").View().Content
	for _, want := range []string{"2 recordings ready", "Catch up everything", "Today only", "Choose recordings"} {
		if !strings.Contains(view, want) {
			t.Errorf("all-today menu missing %q:\n%s", want, view)
		}
	}
}

func TestChooseGroupsHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := chooseGroups(ctx, selectionTestGroups(), time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("chooseGroups error = %v, want context cancellation", err)
	}
}

func TestTodaySelectionKeepsOnlyTodaysPendingRecordings(t *testing.T) {
	model := newSelectionModel(selectionTestGroups(), "2026-08-27")
	model = updateSelectionModel(t, model, "down")
	updated, command := model.Update(keyMessage("enter"))
	if command == nil {
		t.Fatal("today selection did not finish the picker")
	}
	result := updated.(selectionModel).result
	if len(result) != 1 || result[0].Date != "2026-08-27" || len(result[0].Memos) != 2 {
		t.Fatalf("today result = %+v", result)
	}
}

func TestRecordingPickerCanSelectOnePart(t *testing.T) {
	model := newSelectionModel(selectionTestGroups(), "2026-08-27")
	model = updateSelectionModel(t, model, "down", "down", "enter", "down", "down", "space")
	updated, command := model.Update(keyMessage("enter"))
	if command == nil {
		t.Fatal("selected recording did not finish the picker")
	}
	result := updated.(selectionModel).result
	if len(result) != 1 || len(result[0].Memos) != 1 || result[0].Memos[0].Part != "02" {
		t.Fatalf("picker result = %+v", result)
	}
}

func TestSelectionModelIgnoresKnownInvalidTranscripts(t *testing.T) {
	groups := selectionTestGroups()
	groups[0].Memos[0].Status = failed
	groups[0].Memos[0].Detail = "empty transcript"
	model := newSelectionModel(groups, "2026-08-27")
	if len(model.recordings) != 2 {
		t.Fatalf("selectable recordings = %d, want only 2 healthy recordings", len(model.recordings))
	}
	if view := model.View().Content; !strings.Contains(view, "2 recordings ready") {
		t.Fatalf("invalid transcript was counted as selectable:\n%s", view)
	}
}

func TestCatchUpSelectionKeepsKnownInvalidTranscriptsForReporting(t *testing.T) {
	groups := selectionTestGroups()
	groups[0].Memos[0].Status = failed
	groups[0].Memos[0].Detail = "empty transcript"
	model := newSelectionModel(groups, "2026-08-27")
	updated, command := model.Update(keyMessage("enter"))
	if command == nil {
		t.Fatal("catch-up selection did not finish")
	}
	result := updated.(selectionModel).result
	if len(result) != 2 || len(result[0].Memos) != 1 || result[0].Memos[0].Status != failed {
		t.Fatalf("catch-up selection silently dropped invalid transcript: %+v", result)
	}
}

func TestSelectionModelLatchesConfirmedResult(t *testing.T) {
	model := newSelectionModel(selectionTestGroups(), "2026-08-27")
	updated, command := model.Update(keyMessage("enter"))
	if command == nil {
		t.Fatal("confirmation did not request quit")
	}
	confirmed := updated.(selectionModel)
	updated, command = confirmed.Update(keyMessage("q"))
	latched := updated.(selectionModel)
	if command != nil || latched.exited || !latched.done || len(latched.result) != len(confirmed.result) {
		t.Fatalf("queued key changed confirmed selection: %+v", latched)
	}
}

func TestKnownInvalidTranscriptDoesNotFailPreparation(t *testing.T) {
	value := group{
		Course: "OLD", Date: "2026-08-26", TranscriptDir: t.TempDir(),
		Memos: []Memo{{Stem: "2026-08-26-pt01", Status: failed, Detail: "empty transcript"}},
	}
	if err := prepareGroup(&value, false); err != nil {
		t.Fatalf("known invalid transcript failed preparation: %v", err)
	}
}

func TestKnownInvalidTranscriptRemainsPendingAndReportsFailure(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "2026-08-26.txt"), []byte("old combined transcript\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "2026-08-26-pt01.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	value := group{
		Course: "OLD", Date: "2026-08-26", TranscriptDir: directory,
		Memos: []Memo{{Path: "old.m4a", Stem: "2026-08-26-pt01", Status: failed, Detail: "empty transcript"}},
	}
	pending := pendingGroups([]group{value})
	if len(pending) != 1 {
		t.Fatalf("invalid transcript disappeared from pending work: %+v", pending)
	}
	hadFailureEvent := false
	err := processGroups(context.Background(), pending, Options{}, func(message event) bool {
		hadFailureEvent = hadFailureEvent || message.HasMemoUpdate && message.Status == failed
		return true
	})
	if err == nil || !strings.Contains(err.Error(), "replace it with: lectr transcribe OLD 2026-08-26 --force") || !hadFailureEvent {
		t.Fatalf("invalid transcript result: err=%v failureEvent=%v", err, hadFailureEvent)
	}
}

func TestUnselectedInvalidTranscriptDoesNotBlockChosenGroup(t *testing.T) {
	root := t.TempDir()
	oldTranscripts := filepath.Join(root, "OLD", "transcripts")
	if err := os.MkdirAll(oldTranscripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldTranscripts, "2026-08-26-pt01.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{
		{
			Course: "OLD", Date: "2026-08-26", TranscriptDir: oldTranscripts,
			Memos: []Memo{
				{Stem: "2026-08-26-pt01", Part: "01"},
				{Stem: "2026-08-26-pt02", Part: "02"},
			},
		},
		{
			Course: "TODAY", Date: "2026-08-27", TranscriptDir: filepath.Join(root, "TODAY", "transcripts"),
			Memos: []Memo{{Stem: "2026-08-27-pt01", Part: "01"}},
		},
	}
	for index := range groups {
		markExistingMemos(&groups[index], false)
	}
	if groups[0].Memos[0].Status != failed {
		t.Fatalf("invalid transcript status = %v, want failed", groups[0].Memos[0].Status)
	}
	selected := filterGroups(pendingGroups(groups), func(groupIndex, _ int) bool {
		return groups[groupIndex].Date == "2026-08-27"
	})
	if len(selected) != 1 || selected[0].Course != "TODAY" {
		t.Fatalf("selected groups = %+v", selected)
	}
	if err := prepareGroup(&selected[0], false); err != nil {
		t.Fatalf("unselected invalid transcript blocked chosen group: %v", err)
	}
}

func TestUnreadableExistingTranscriptDoesNotBlockSelectionInventory(t *testing.T) {
	directory := t.TempDir()
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.MkdirAll(filepath.Join(transcriptDir, "2026-08-26-pt01.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	value := group{
		Course: "OLD", Date: "2026-08-26", TranscriptDir: transcriptDir,
		Memos: []Memo{{Stem: "2026-08-26-pt01", Part: "01"}},
	}
	markExistingMemos(&value, false)
	if value.Memos[0].Status != failed || value.Memos[0].Detail == "" {
		t.Fatalf("unreadable transcript = %+v, want failed classification", value.Memos[0])
	}
}

func TestTranscriptStatErrorDoesNotBlockSelectionInventory(t *testing.T) {
	directory := t.TempDir()
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "2026-08-26-pt01.txt"
	if err := os.Symlink(name, filepath.Join(transcriptDir, name)); err != nil {
		t.Fatal(err)
	}
	value := group{
		Course: "OLD", Date: "2026-08-26", TranscriptDir: transcriptDir,
		Memos: []Memo{{Stem: "2026-08-26-pt01", Part: "01"}},
	}
	markExistingMemos(&value, false)
	if value.Memos[0].Status != failed || value.Memos[0].Detail == "" {
		t.Fatalf("metadata error = %+v, want failed classification", value.Memos[0])
	}
}

func TestPartialSelectionDoesNotPublishCombinedTranscript(t *testing.T) {
	directory := t.TempDir()
	installSelectionWhisper(t, directory)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	groups := []group{{
		Course: "MATH351", Date: "2026-08-27", TranscriptDir: transcriptDir,
		Memos: []Memo{
			{Course: "MATH351", Path: filepath.Join(directory, "pt01.m4a"), Stem: "2026-08-27-pt01", Part: "01"},
			{Course: "MATH351", Path: filepath.Join(directory, "pt02.m4a"), Stem: "2026-08-27-pt02", Part: "02"},
		},
	}}
	selected := filterGroups(groups, func(_, memoIndex int) bool { return memoIndex == 0 })
	hadCombineEvent := false
	if err := processGroup(context.Background(), &selected[0], Options{Model: DefaultModel}, func(message event) bool {
		hadCombineEvent = hadCombineEvent || message.HasCombine
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if hadCombineEvent {
		t.Fatal("partial selection emitted a combine event")
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-08-27-pt01.txt")); err != nil {
		t.Fatalf("selected part was not transcribed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-08-27.txt")); !os.IsNotExist(err) {
		t.Fatalf("partial combined transcript was published: %v", err)
	}
}

func TestUnselectedInvalidSiblingDoesNotBlockSelectedPart(t *testing.T) {
	directory := t.TempDir()
	installSelectionWhisper(t, directory)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDir, "2026-08-27-pt01.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{{
		Course: "MATH351", Date: "2026-08-27", TranscriptDir: transcriptDir,
		Memos: []Memo{
			{Course: "MATH351", Stem: "2026-08-27-pt01", Part: "01"},
			{Course: "MATH351", Path: filepath.Join(directory, "pt02.m4a"), Stem: "2026-08-27-pt02", Part: "02"},
		},
	}}
	markExistingMemos(&groups[0], false)
	selected := filterGroups(groups, func(_, memoIndex int) bool { return memoIndex == 1 })
	if err := prepareGroup(&selected[0], false); err != nil {
		t.Fatalf("unselected invalid sibling blocked selected preparation: %v", err)
	}
	if err := processGroup(context.Background(), &selected[0], Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatalf("unselected invalid sibling blocked selected part: %v", err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-08-27-pt02.txt")); err != nil {
		t.Fatalf("selected part was not transcribed: %v", err)
	}
}

func TestPartialSelectionPreservesExistingCombinedTranscript(t *testing.T) {
	directory := t.TempDir()
	installSelectionWhisper(t, directory)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	combined := filepath.Join(transcriptDir, "2026-08-27.txt")
	if err := os.WriteFile(combined, []byte("previous complete lecture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{{
		Course: "MATH351", Date: "2026-08-27", TranscriptDir: transcriptDir,
		Memos: []Memo{
			{Course: "MATH351", Path: filepath.Join(directory, "pt01.m4a"), Stem: "2026-08-27-pt01", Part: "01"},
			{Course: "MATH351", Path: filepath.Join(directory, "pt02.m4a"), Stem: "2026-08-27-pt02", Part: "02"},
		},
	}}
	selected := filterGroups(groups, func(_, memoIndex int) bool { return memoIndex == 0 })
	if err := processGroup(context.Background(), &selected[0], Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(combined)
	if err != nil || string(contents) != "previous complete lecture\n" {
		t.Fatalf("existing combined transcript = %q, err=%v", contents, err)
	}
}

func TestFilterGroupsKeepsFullyTranscribedGroupNeedingCombine(t *testing.T) {
	directory := t.TempDir()
	installSelectionWhisper(t, directory)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDir, "2026-08-27-pt01.txt"), []byte("already transcribed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []group{{
		Course: "MATH351", Date: "2026-08-27", TranscriptDir: transcriptDir,
		Memos: []Memo{{Course: "MATH351", Stem: "2026-08-27-pt01", Part: "01"}},
	}}
	markExistingMemos(&groups[0], false)
	if hasPendingMemos(groups[0]) {
		t.Fatalf("expected memo to already be skipped, got %+v", groups[0].Memos)
	}
	// Direct unit check of filterGroups' retention rule in isolation. A
	// single group like this can no longer actually reach the interactive
	// menu in production (Run's anyPendingMemos check skips the menu
	// entirely when there is nothing left to select) — that end-to-end path
	// is covered by TestSelectionMenuCombinesUnselectedFullyTranscribedGroup
	// below, which drives the real bubbletea selector across a *mixed* set
	// of groups (one with real work, one combine-only).
	selected := filterGroups(groups, func(_, _ int) bool { return false })
	if len(selected) != 1 {
		t.Fatalf("expected the combine-only group to survive filterGroups, got %d groups: %+v", len(selected), selected)
	}
	if err := processGroup(context.Background(), &selected[0], Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-08-27.txt")); err != nil {
		t.Fatalf("combined transcript was not produced: %v", err)
	}
}

func TestSelectionMenuCombinesUnselectedFullyTranscribedGroup(t *testing.T) {
	directory := t.TempDir()
	installSelectionWhisper(t, directory)
	transcriptDir := filepath.Join(directory, "transcripts")
	if err := os.Mkdir(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptDir, "2026-01-02-pt01.txt"), []byte("already transcribed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	combineOnly := group{
		Course: "OLD", Date: "2026-01-02", TranscriptDir: transcriptDir,
		Memos: []Memo{{Course: "OLD", Stem: "2026-01-02-pt01", Part: "01"}},
	}
	markExistingMemos(&combineOnly, false)
	if hasPendingMemos(combineOnly) {
		t.Fatalf("expected the OLD memo to already be skipped, got %+v", combineOnly.Memos)
	}
	groups := append([]group{combineOnly}, selectionTestGroups()...)

	model := newSelectionModel(groups, "2026-08-27")
	view := model.View().Content
	if !strings.Contains(view, "3 recordings ready") {
		t.Fatalf("combine-only group should contribute no selectable items:\n%s", view)
	}
	// "Catch up everything" (the default first backlog choice) selects only
	// the real pending items; the combine-only group must still ride along
	// via needsCombine, not via being selected.
	updated, command := model.Update(keyMessage("enter"))
	if command == nil {
		t.Fatal("catch up everything did not finish the picker")
	}
	result := updated.(selectionModel).result
	if len(result) != 3 {
		t.Fatalf("expected 3 groups in the result (combine-only + 2 pending), got %d: %+v", len(result), result)
	}
	var kept *group
	for index := range result {
		if result[index].Course == "OLD" {
			kept = &result[index]
		}
	}
	if kept == nil {
		t.Fatalf("combine-only OLD group was dropped by the real selection menu: %+v", result)
	}
	if kept.SkipCombine {
		t.Fatalf("combine-only OLD group should not have SkipCombine set: %+v", kept)
	}
	if err := processGroup(context.Background(), kept, Options{Model: DefaultModel}, func(event) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(transcriptDir, "2026-01-02.txt")); err != nil {
		t.Fatalf("combined transcript was not produced through the real selection menu: %v", err)
	}
}

func TestExactMemoSelectorSkipsDateCombine(t *testing.T) {
	root := t.TempDir()
	memoDir := filepath.Join(root, "MATH351", "memos")
	if err := os.MkdirAll(memoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-08-27-pt01.m4a", "2026-08-27-pt02.m4a"} {
		if err := os.WriteFile(filepath.Join(memoDir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := discoverGroups(Options{
		Root: root, Courses: []string{"MATH351"}, Selector: "2026-08-27-pt01.m4a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Memos) != 1 || !groups[0].SkipCombine {
		t.Fatalf("exact memo groups = %+v, want one part with combining disabled", groups)
	}
}

func installSelectionWhisper(t *testing.T, directory string) {
	t.Helper()
	installFakeWhisper(t, directory, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) output_dir="$2"; shift 2 ;;
    --output-name) output_name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'a valid transcript\n' > "$output_dir/$output_name.txt"
`)
}

func updateSelectionModel(t *testing.T, model selectionModel, keys ...string) selectionModel {
	t.Helper()
	for _, key := range keys {
		updated, _ := model.Update(keyMessage(key))
		model = updated.(selectionModel)
	}
	return model
}

func keyMessage(value string) tea.KeyPressMsg {
	switch value {
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "space":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace})
	default:
		return tea.KeyPressMsg(tea.Key{Text: value, Code: rune(value[0])})
	}
}

func selectionTestGroups() []group {
	return []group{
		{
			Course: "MATH351", Date: "2026-08-26",
			Memos: []Memo{{Part: "01", Duration: "45:00", Status: waiting}},
		},
		{
			Course: "MATH451", Date: "2026-08-27",
			Memos: []Memo{
				{Part: "01", Duration: "30:00", Status: waiting},
				{Part: "02", Duration: "20:00", Status: waiting},
			},
		},
	}
}
