package transcribe

import (
	"github.com/mamuzad/lectr/internal/ui"

	tea "charm.land/bubbletea/v2"
)

type eventMsg struct {
	value event
	ok    bool
}

type model struct {
	groups    []group
	current   int
	events    <-chan event
	cancel    contextCancel
	combines  []memoStatus
	combined  []string
	frame     int
	cancelled bool
	finished  bool
}

type contextCancel func()

func newModel(values []group, events <-chan event, cancel func()) model {
	groups := make([]group, len(values))
	for index, value := range values {
		groups[index] = valueCopy(value)
	}
	return model{
		groups: groups, events: events, cancel: contextCancel(cancel),
		combines: make([]memoStatus, len(groups)), combined: make([]string, len(groups)),
	}
}

func valueCopy(value group) group {
	value.Memos = append([]Memo(nil), value.Memos...)
	return value
}

func (m model) Init() tea.Cmd { return waitEvent(m.events) }

func waitEvent(events <-chan event) tea.Cmd {
	return func() tea.Msg {
		value, ok := <-events
		return eventMsg{value: value, ok: ok}
	}
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyPressMsg:
		if message.String() == "ctrl+c" {
			m.cancelled = true
			m.cancel()
			return m, tea.Quit
		}
	case eventMsg:
		if !message.ok {
			return m, tea.Quit
		}
		value := message.value
		if value.AllComplete {
			m.finished = true
			return m, waitEvent(m.events)
		}
		if value.Group < 0 || value.Group >= len(m.groups) {
			return m, waitEvent(m.events)
		}
		m.current = value.Group
		if value.HasMemoUpdate && value.Index >= 0 && value.Index < len(m.groups[m.current].Memos) {
			memo := &m.groups[m.current].Memos[value.Index]
			memo.Status, memo.Percent, memo.Detail = value.Status, value.Percent, value.Detail
		}
		if value.HasCombine {
			m.combines[m.current], m.combined[m.current] = value.Combine, value.CombinedPath
		}
		m.frame++
		return m, waitEvent(m.events)
	}
	return m, nil
}

func (m model) View() tea.View {
	completedCount := m.current
	if m.finished {
		completedCount = len(m.groups)
	}
	completed := make([]ui.Lecture, completedCount)
	for index := range completed {
		completed[index] = m.lecture(index)
	}
	if m.finished {
		return tea.NewView(ui.Shell(ui.CompletedTranscriptionQueue(completed)))
	}
	return tea.NewView(ui.Shell(ui.TranscriptionQueue(completed, m.lecture(m.current))))
}

func (m model) lecture(groupIndex int) ui.Lecture {
	group := m.groups[groupIndex]
	memos := make([]ui.Memo, len(group.Memos))
	for index, memo := range group.Memos {
		memos[index] = ui.Memo{
			Part: memo.Part, Duration: memo.Duration, Status: ui.Status(memo.Status),
			Percent: memo.Percent, Detail: memo.Detail,
		}
	}
	return ui.Lecture{
		Course: group.Course, Date: group.Date, Memos: memos,
		Combine: ui.Status(m.combines[groupIndex]), CombinedPath: m.combined[groupIndex],
		TranscriptDir: group.TranscriptDir, Frame: m.frame,
	}
}
