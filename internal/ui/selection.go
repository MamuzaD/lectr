package ui

type SelectionAction uint8

const (
	SelectionNone SelectionAction = iota
	SelectionConfirmed
	SelectionExited
	SelectionInterrupted
)

type SelectionItem struct {
	Label    string
	Duration string
	Today    bool
}

type selectionScreen uint8

const (
	selectionBacklog selectionScreen = iota
	selectionPicker
)

type backlogChoice uint8

const (
	backlogEverything backlogChoice = iota
	backlogToday
	backlogPick
	backlogExit
)

type RecordingSelector struct {
	items    []SelectionItem
	audio    string
	screen   selectionScreen
	cursor   int
	selected []bool
}

func NewRecordingSelector(items []SelectionItem, audio string) RecordingSelector {
	return RecordingSelector{
		items: append([]SelectionItem(nil), items...), audio: audio,
		selected: make([]bool, len(items)),
	}
}

func (s RecordingSelector) Update(key string) (RecordingSelector, SelectionAction) {
	if key == "ctrl+c" {
		return s, SelectionInterrupted
	}
	if s.screen == selectionPicker {
		return s.updatePicker(key)
	}
	return s.updateBacklog(key)
}

func (s RecordingSelector) updateBacklog(key string) (RecordingSelector, SelectionAction) {
	choices := s.backlogChoices()
	switch key {
	case "q", "esc":
		return s, SelectionExited
	case "up", "k":
		s.cursor = (s.cursor + len(choices) - 1) % len(choices)
	case "down", "j":
		s.cursor = (s.cursor + 1) % len(choices)
	case "enter":
		switch choices[s.cursor] {
		case backlogEverything:
			s.selected = make([]bool, len(s.items))
			for index := range s.selected {
				s.selected[index] = true
			}
			return s, SelectionConfirmed
		case backlogToday:
			s.selected = make([]bool, len(s.items))
			for index, item := range s.items {
				s.selected[index] = item.Today
			}
			if len(s.SelectedIndices()) > 0 {
				return s, SelectionConfirmed
			}
		case backlogPick:
			s.screen, s.cursor = selectionPicker, 0
			s.selected = make([]bool, len(s.items))
		case backlogExit:
			return s, SelectionExited
		}
	}
	return s, SelectionNone
}

func (s RecordingSelector) backlogChoices() []backlogChoice {
	if len(s.items) == 0 {
		return []backlogChoice{backlogExit}
	}
	choices := []backlogChoice{backlogEverything}
	if s.todayCount() > 0 {
		choices = append(choices, backlogToday)
	}
	return append(choices, backlogPick, backlogExit)
}

func (s RecordingSelector) todayCount() int {
	count := 0
	for _, item := range s.items {
		if item.Today {
			count++
		}
	}
	return count
}

func (s RecordingSelector) updatePicker(key string) (RecordingSelector, SelectionAction) {
	count := len(s.items)
	if count == 0 {
		return s, SelectionNone
	}
	switch key {
	case "up", "k":
		s.cursor = (s.cursor + count - 1) % count
	case "down", "j":
		s.cursor = (s.cursor + 1) % count
	case "space", " ":
		s.selected = append([]bool(nil), s.selected...)
		s.selected[s.cursor] = !s.selected[s.cursor]
	case "esc":
		s.screen, s.cursor = selectionBacklog, 0
	case "enter":
		if len(s.SelectedIndices()) > 0 {
			return s, SelectionConfirmed
		}
	}
	return s, SelectionNone
}

func (s RecordingSelector) SelectedIndices() []int {
	indices := make([]int, 0, len(s.selected))
	for index, selected := range s.selected {
		if selected {
			indices = append(indices, index)
		}
	}
	return indices
}

func (s RecordingSelector) View() string {
	if s.screen == selectionPicker {
		choices := make([]RecordingChoice, len(s.items))
		for index, item := range s.items {
			choices[index] = RecordingChoice{
				Label: item.Label, Duration: item.Duration, Selected: s.selected[index],
			}
		}
		return RecordingPicker(choices, s.cursor)
	}
	options := make([]MenuOption, 0, 4)
	for _, choice := range s.backlogChoices() {
		switch choice {
		case backlogEverything:
			options = append(options, MenuOption{Label: "Catch up everything", Detail: recordingCount(len(s.items))})
		case backlogToday:
			options = append(options, MenuOption{Label: "Today only", Detail: recordingCount(s.todayCount())})
		case backlogPick:
			options = append(options, MenuOption{Label: "Choose recordings"})
		case backlogExit:
			options = append(options, MenuOption{Label: "Exit"})
		}
	}
	return BacklogMenu(
		recordingCount(len(s.items))+" ready  ·  "+s.audio,
		"What do you want to transcribe?",
		options,
		s.cursor,
	)
}
