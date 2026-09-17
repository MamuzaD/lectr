package demo

import (
	tea "charm.land/bubbletea/v2"
	"github.com/mamuzad/lectr/internal/ui"
)

func Run(profile string) error {
	model, err := NewProfile(profile)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(model).Run()
	return err
}

func (m Model) View() tea.View { return tea.NewView(m.render()) }

func (m Model) render() string {
	body := m.selector.View()
	if m.screen == workingScreen {
		body = m.workingView()
	}
	return ui.Shell(body)
}

func (m Model) workingView() string {
	lecture := m.queue[m.current]
	memo := ui.Memo{Part: "01", Duration: lecture.duration}
	combine := ui.Waiting
	combined := ""
	switch m.phase {
	case transcribing:
		detail := "Transcribing"
		if m.percent < 12 {
			detail = "Starting Whisper"
		} else if m.percent < 28 {
			detail = "Preparing model"
		}
		memo.Status, memo.Percent, memo.Detail = ui.Active, m.percent, detail
	case combining:
		memo.Status, memo.Percent, combine = ui.Complete, 100, ui.Active
	case finished:
		memo.Status, memo.Percent, combine = ui.Complete, 100, ui.Complete
		combined = lecture.course + "/transcripts/" + lecture.date + ".txt"
	}
	current := ui.Lecture{
		Course: lecture.course, Date: lecture.date, Memos: []ui.Memo{memo},
		Combine: combine, CombinedPath: combined, TranscriptDir: lecture.course + "/transcripts", Frame: m.frame,
	}
	completedCount := m.current
	if m.phase == finished {
		completedCount++
	}
	completed := make([]ui.Lecture, completedCount)
	for index, lecture := range m.queue[:completedCount] {
		completed[index] = ui.Lecture{
			Course: lecture.course, Date: lecture.date,
			Memos:   []ui.Memo{{Part: "01", Duration: lecture.duration, Status: ui.Complete, Percent: 100}},
			Combine: ui.Complete, CombinedPath: lecture.course + "/transcripts/" + lecture.date + ".txt",
			TranscriptDir: lecture.course + "/transcripts",
		}
	}
	if m.phase == finished {
		return ui.CompletedTranscriptionQueue(completed)
	}
	return ui.TranscriptionQueue(completed, current)
}
