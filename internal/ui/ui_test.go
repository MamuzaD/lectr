package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLectureRenderHandlesMultipleParts(t *testing.T) {
	directory := filepath.Join("MATH351", "transcripts")
	lecture := Lecture{
		Course: "MATH351", Date: "2026-08-25", TranscriptDir: directory,
		Memos: []Memo{
			{Part: "01", Duration: "51:33", Status: Skipped},
			{Part: "02", Duration: "19:30", Status: Complete},
		},
		Combine: Complete, CombinedPath: filepath.Join(directory, "2026-08-25.txt"),
	}
	view := stripANSI(lecture.Render())
	for _, want := range []string{"2 recordings", "pt01", "Already exists", "pt02", "Combined 2 parts", "MATH351/transcripts/2026-08-25.txt"} {
		if !strings.Contains(view, want) {
			t.Errorf("render missing %q:\n%s", want, view)
		}
	}
}

func TestTranscriptionQueueLeavesCompactCompletionReceipts(t *testing.T) {
	view := stripANSI(TranscriptionQueue(
		[]Lecture{
			{Course: "MATH351", Date: "2026-08-25", Memos: []Memo{{}, {}}},
			{Course: "MATH451", Date: "2026-08-25", Memos: []Memo{{}}},
		},
		Lecture{Course: "ENGL305", Date: "2026-08-26", Memos: []Memo{{Part: "01", Status: Active}}},
	))
	for _, want := range []string{
		"✓  MATH351  ·  August 25, 2026  ·  2 recordings transcribed",
		"✓  MATH451  ·  August 25, 2026  ·  1 recording transcribed",
		"ENGL305  ·  August 26, 2026  ·  1 recording",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("queue render missing %q:\n%s", want, view)
		}
	}
}

func TestCompletedTranscriptionQueueCollapsesLastLecture(t *testing.T) {
	view := stripANSI(CompletedTranscriptionQueue([]Lecture{{
		Course: "MATH451", Date: "2026-08-25", Memos: []Memo{{Part: "01", Status: Skipped}},
	}}))
	for _, want := range []string{"1 recording already transcribed", "Local processing"} {
		if !strings.Contains(view, want) {
			t.Errorf("completed queue missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pt01") || strings.Contains(view, "Combined") {
		t.Fatalf("completed queue retained detailed card:\n%s", view)
	}
}

var testANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(value string) string { return testANSI.ReplaceAllString(value, "") }

func TestLectureRenderShowsFailureDetail(t *testing.T) {
	view := (Lecture{
		Course: "MATH451", Date: "2026-08-25",
		Memos: []Memo{{Part: "01", Duration: "1:15:53", Status: Failed, Detail: "Whisper did not create a transcript"}},
	}).Render()
	if !strings.Contains(view, "Failed") || !strings.Contains(view, "Whisper did not create a transcript") {
		t.Fatalf("failure render:\n%s", view)
	}
}

func TestDividerHandlesSmallWidths(t *testing.T) {
	if got := Divider(1); got != "" {
		t.Fatalf("Divider(1) = %q", got)
	}
}

func TestLogoUsesLECTRWordmark(t *testing.T) {
	logo := stripANSI(Logo())
	for _, line := range []string{"▜     ▗   ", "▐ █▌▛▘▜▘▛▘", "▐▖▙▖▙▖▐▖▌ "} {
		if !strings.Contains(logo, line) {
			t.Fatalf("logo missing %q:\n%s", line, logo)
		}
	}
}

func TestThemesChangeSharedUI(t *testing.T) {
	defer UseTheme("pumpkin-spice")
	if err := UseTheme("pumpkin-spice"); err != nil {
		t.Fatal(err)
	}
	autumn := Logo()
	if err := UseTheme("enchanting-table"); err != nil {
		t.Fatal(err)
	}
	enchanting := Logo()
	if autumn == enchanting {
		t.Fatal("themes rendered the same ANSI colors")
	}
}

func TestBacklogMenuUsesSharedUI(t *testing.T) {
	menu := BacklogMenu("2 recordings ready", "What next?", []MenuOption{{Label: "Everything", Detail: "2 recordings"}}, 0)
	for _, value := range []string{"2 recordings ready", "What next?", "Everything", "q/esc exit", "Local processing"} {
		if !strings.Contains(menu, value) {
			t.Fatalf("shared menu missing %q", value)
		}
	}
}

func TestHelpUsesSharedLogoAndCommandLayout(t *testing.T) {
	view := stripANSI(Help(
		"Local lectures.", "lectr <command>", "/tmp/config.json",
		[]Command{{Name: "transcribe", Description: "Transcribe recordings"}},
	))
	for _, value := range []string{"▜     ▗   ", "Local lectures.", "lectr <command>", "transcribe", "/tmp/config.json"} {
		if !strings.Contains(view, value) {
			t.Fatalf("help missing %q:\n%s", value, view)
		}
	}
}

func TestCommandHelpUsesFocusedSection(t *testing.T) {
	view := stripANSI(CommandHelp(
		"Manage watcher.", "lectr watch [ACTION]", "Actions",
		[]Command{{Name: "install", Description: "Install watcher"}},
		"/tmp/config.json", "Bare watch shows status.",
	))
	for _, value := range []string{"▜     ▗   ", "Manage watcher.", "lectr watch [ACTION]", "Actions", "install", "Bare watch shows status.", "/tmp/config.json"} {
		if !strings.Contains(view, value) {
			t.Fatalf("command help missing %q:\n%s", value, view)
		}
	}
}

func TestRecordingPickerWindowsStressQueues(t *testing.T) {
	choices := make([]RecordingChoice, 20)
	for index := range choices {
		choices[index] = RecordingChoice{Label: fmt.Sprintf("Recording %02d", index+1)}
	}
	first := RecordingPicker(choices, 0)
	if !strings.Contains(first, "Recording 01") || !strings.Contains(first, "↓ 12 more") || strings.Contains(first, "Recording 20") {
		t.Fatalf("first picker window:\n%s", stripANSI(first))
	}
	last := RecordingPicker(choices, 19)
	if !strings.Contains(last, "↑ 12 more") || !strings.Contains(last, "Recording 20") || strings.Contains(last, "Recording 01") {
		t.Fatalf("last picker window:\n%s", stripANSI(last))
	}
}
