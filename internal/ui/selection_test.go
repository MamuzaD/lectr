package ui

import (
	"strings"
	"testing"
)

func TestRecordingSelectorBacklogAndTodaySelection(t *testing.T) {
	selector := NewRecordingSelector([]SelectionItem{
		{Label: "Yesterday", Duration: "45:00"},
		{Label: "Today one", Duration: "30:00", Today: true},
		{Label: "Today two", Duration: "20:00", Today: true},
	}, "about 1h 35m audio")
	for _, want := range []string{
		"3 recordings ready", "about 1h 35m audio", "Catch up everything",
		"Today only", "2 recordings", "Choose recordings",
	} {
		if view := selector.View(); !strings.Contains(view, want) {
			t.Errorf("selector missing %q:\n%s", want, view)
		}
	}
	selector, _ = selector.Update("down")
	selector, action := selector.Update("enter")
	if action != SelectionConfirmed {
		t.Fatalf("today action = %v", action)
	}
	want := []int{1, 2}
	got := selector.SelectedIndices()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("today selection = %v, want %v", got, want)
	}
}

func TestRecordingSelectorPickerAndExitActions(t *testing.T) {
	selector := NewRecordingSelector([]SelectionItem{{Label: "One"}, {Label: "Two"}}, "about 2m audio")
	for _, key := range []string{"down", "enter", "down", "space"} {
		selector, _ = selector.Update(key)
	}
	selector, action := selector.Update("enter")
	if action != SelectionConfirmed || len(selector.SelectedIndices()) != 1 || selector.SelectedIndices()[0] != 1 {
		t.Fatalf("picker action=%v selection=%v", action, selector.SelectedIndices())
	}
	selector = NewRecordingSelector([]SelectionItem{{Label: "One"}}, "about 1m audio")
	if _, action := selector.Update("q"); action != SelectionExited {
		t.Fatalf("q action = %v", action)
	}
	if _, action := selector.Update("ctrl+c"); action != SelectionInterrupted {
		t.Fatalf("ctrl+c action = %v", action)
	}
}

func TestRecordingSelectorOmitsTodayWhenNoneAreAvailable(t *testing.T) {
	selector := NewRecordingSelector([]SelectionItem{{Label: "Yesterday"}}, "about 45m audio")
	if view := selector.View(); strings.Contains(view, "Today only") {
		t.Fatalf("zero-count Today option should be omitted:\n%s", view)
	}
	selector, _ = selector.Update("down")
	selector, action := selector.Update("enter")
	if action != SelectionNone || !strings.Contains(selector.View(), "Choose recordings") {
		t.Fatalf("first option after Catch up should open the picker, action=%v", action)
	}
}

func TestRecordingSelectorWithNoItemsCanOnlyExit(t *testing.T) {
	selector := NewRecordingSelector(nil, "audio duration unavailable")
	view := selector.View()
	if strings.Contains(view, "Catch up everything") || !strings.Contains(view, "Exit") {
		t.Fatalf("empty selector offered invalid action:\n%s", view)
	}
	if _, action := selector.Update("enter"); action != SelectionExited {
		t.Fatalf("empty selector enter action = %v, want exit", action)
	}
}
