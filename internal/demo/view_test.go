package demo

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mamuzad/lectr/internal/ui"
)

func TestBacklogMatchesPythonDemoData(t *testing.T) {
	view := New().render()
	for _, value := range []string{"8 recordings ready", "Catch up everything", "Today only", "Choose recordings"} {
		if !strings.Contains(view, value) {
			t.Fatalf("demo missing %q", value)
		}
	}
}

func TestBacklogQuitsWithQOrEscape(t *testing.T) {
	keys := []tea.Key{
		{Text: "q", Code: 'q'},
		{Code: tea.KeyEscape},
	}
	for _, key := range keys {
		_, command := New().updateBacklog(tea.KeyPressMsg(key))
		if command == nil {
			t.Fatalf("%s did not return a quit command", tea.KeyPressMsg(key).String())
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatalf("%s command did not quit", tea.KeyPressMsg(key).String())
		}
	}
}

func TestWorkingViewUsesProductionLectureRenderer(t *testing.T) {
	m := New()
	m.queue, m.screen, m.phase = []lecture{m.fixture.lectures[0]}, workingScreen, finished
	got := m.workingView()
	want := ui.CompletedTranscriptionQueue([]ui.Lecture{{
		Course: "MATH351", Date: "2026-11-10",
		Memos:   []ui.Memo{{Part: "01", Duration: "1:11:08", Status: ui.Complete, Percent: 100}},
		Combine: ui.Complete, CombinedPath: "MATH351/transcripts/2026-11-10.txt", TranscriptDir: "MATH351/transcripts",
	}})
	if got != want {
		t.Fatal("demo working view drifted from production renderer")
	}
}

func TestWorkingViewKeepsCompletedLecturesVisible(t *testing.T) {
	m := New()
	m.queue, m.current, m.screen = m.fixture.lectures[:3], 2, workingScreen
	view := uiText(m.workingView())
	for _, want := range []string{
		"✓  MATH351  ·  November 10, 2026  ·  1 recording transcribed",
		"✓  MATH451  ·  November 10, 2026  ·  1 recording transcribed",
		"MATH351  ·  November 12, 2026  ·  1 recording",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("working queue missing %q:\n%s", want, view)
		}
	}
}

func uiText(value string) string {
	return regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`).ReplaceAllString(value, "")
}

func TestSpringDemoHasFiveCoursesFromJanuaryToMay(t *testing.T) {
	m, err := NewProfile("spring")
	if err != nil {
		t.Fatal(err)
	}
	view := m.render()
	for _, value := range []string{"10 recordings ready", "Today only", "5 recordings"} {
		if !strings.Contains(view, value) {
			t.Fatalf("spring demo missing %q", value)
		}
	}
	for _, lecture := range m.fixture.lectures {
		if strings.Contains(lecture.label, "disc") {
			t.Fatalf("stress fixture contains invented discussion label: %q", lecture.label)
		}
	}
	picker := m.pickerView()
	for _, value := range []string{"CS401", "MATH402", "PHYS310", "HIST220", "ENGL305"} {
		if !strings.Contains(picker, value) {
			t.Fatalf("spring picker missing course %q:\n%s", value, picker)
		}
	}
}

func TestStressDemoHasDenseFiveDaySchedule(t *testing.T) {
	m, err := NewProfile("stress")
	if err != nil {
		t.Fatal(err)
	}
	view := m.render()
	for _, value := range []string{"20 recordings ready", "about 21h audio"} {
		if !strings.Contains(view, value) {
			t.Fatalf("stress demo missing %q", value)
		}
	}
	if len(m.fixture.lectures) != 20 {
		t.Fatalf("stress fixture has %d lectures, want 20", len(m.fixture.lectures))
	}
	seen := make(map[string]bool)
	for _, lecture := range m.fixture.lectures {
		seen[lecture.course] = true
	}
	if len(seen) != 5 {
		t.Fatalf("stress fixture has %d courses: %v", len(seen), seen)
	}
	picker := m.pickerView()
	for _, value := range []string{"CS401", "MATH402", "PHYS310", "HIST220", "ENGL305"} {
		if !strings.Contains(picker, value) {
			t.Fatalf("stress picker missing course %q:\n%s", value, picker)
		}
	}
}
