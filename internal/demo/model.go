package demo

import (
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
)

type status uint8

const (
	complete status = iota
	active
	ready
	missing
)

type day uint8

const (
	tuesday day = iota
	thursday
)

type screen uint8

const (
	backlogScreen screen = iota
	pickerScreen
	workingScreen
)

type phase uint8

const (
	transcribing phase = iota
	combining
	finished
)

type week struct {
	month    string
	date     string
	tuesday  [2]status
	thursday [2]status
}

type lecture struct {
	course      string
	date        string
	label       string
	duration    string
	week        int
	day         day
	courseIndex int
}

var readyLectures = []lecture{
	{course: "MATH351", date: "2026-11-10", label: "Tue, Nov 10", duration: "1:11:08", week: 11, day: tuesday, courseIndex: 0},
	{course: "MATH451", date: "2026-11-10", label: "Tue, Nov 10", duration: "1:13:26", week: 11, day: tuesday, courseIndex: 1},
	{course: "MATH351", date: "2026-11-12", label: "Thu, Nov 12", duration: "1:09:41", week: 11, day: thursday, courseIndex: 0},
	{course: "MATH451", date: "2026-11-12", label: "Thu, Nov 12", duration: "1:14:02", week: 11, day: thursday, courseIndex: 1},
	{course: "MATH351", date: "2026-11-17", label: "Tue, Nov 17", duration: "1:10:35", week: 12, day: tuesday, courseIndex: 0},
	{course: "MATH451", date: "2026-11-17", label: "Tue, Nov 17", duration: "1:12:19", week: 12, day: tuesday, courseIndex: 1},
	{course: "MATH351", date: "2026-11-19", label: "Thu, Nov 19", duration: "1:12:44", week: 12, day: thursday, courseIndex: 0},
	{course: "MATH451", date: "2026-11-19", label: "Thu, Nov 19", duration: "1:14:18", week: 12, day: thursday, courseIndex: 1},
}

type tickMsg struct{}

type model struct {
	width       int
	weeks       []week
	screen      screen
	cursor      int
	selected    [8]bool
	queue       []lecture
	current     int
	phase       phase
	percent     int
	frame       int
	initialized bool
	progress    progress.Model
}

func New() model {
	return model{
		width:    80,
		weeks:    semester(),
		screen:   backlogScreen,
		progress: newProgress(),
	}
}

func semester() []week {
	months := []string{"   ", "Sep", "   ", "   ", "   ", "   ", "Oct", "   ", "   ", "   ", "Nov", "   ", "   "}
	dates := []string{"25", "01", "08", "15", "22", "29", "06", "13", "20", "27", "03", "10", "17"}
	weeks := make([]week, len(dates))
	for index := range weeks {
		weeks[index] = week{
			month:    months[index],
			date:     dates[index],
			tuesday:  [2]status{complete, complete},
			thursday: [2]status{complete, complete},
		}
	}
	weeks[2].thursday = [2]status{missing, missing}
	weeks[6].tuesday = [2]status{missing, missing}
	weeks[9].thursday = [2]status{missing, missing}
	for index := 11; index <= 12; index++ {
		weeks[index].tuesday = [2]status{ready, ready}
		weeks[index].thursday = [2]status{ready, ready}
	}
	return weeks
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.initialized = true
		return m, nil

	case tea.KeyPressMsg:
		if message.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case backlogScreen:
			return m.updateBacklog(message)
		case pickerScreen:
			return m.updatePicker(message)
		}

	case tickMsg:
		return m.updateProgress()
	}
	return m, nil
}

func (m model) updateBacklog(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.cursor = (m.cursor + 3) % 4
	case "down", "j":
		m.cursor = (m.cursor + 1) % 4
	case "enter":
		switch m.cursor {
		case 0:
			return m.start(append([]lecture(nil), readyLectures...))
		case 1:
			return m.start(append([]lecture(nil), readyLectures[6:]...))
		case 2:
			m.screen = pickerScreen
			m.cursor = 0
			m.selected = [8]bool{}
		case 3:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updatePicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.cursor = (m.cursor + 7) % 8
	case "down", "j":
		m.cursor = (m.cursor + 1) % 8
	case "space", " ":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "esc":
		m.screen = backlogScreen
		m.cursor = 0
	case "enter":
		queue := make([]lecture, 0, len(readyLectures))
		for index, selected := range m.selected {
			if selected {
				queue = append(queue, readyLectures[index])
			}
		}
		if len(queue) > 0 {
			return m.start(queue)
		}
	}
	return m, nil
}

func (m model) start(queue []lecture) (tea.Model, tea.Cmd) {
	m.screen = workingScreen
	m.queue = queue
	m.current = 0
	m.phase = transcribing
	m.percent = 0
	m.frame = 0
	m.setStatus(queue[0], active)
	return m, tickAfter(25 * time.Millisecond)
}

func (m model) updateProgress() (tea.Model, tea.Cmd) {
	lecture := m.queue[m.current]
	m.frame++
	switch m.phase {
	case transcribing:
		m.percent += 4
		if m.percent >= 100 {
			m.percent = 100
			m.phase = combining
			m.frame = 0
			return m, tickAfter(60 * time.Millisecond)
		}
		return m, tickAfter(25 * time.Millisecond)

	case combining:
		if m.frame < 12 {
			return m, tickAfter(60 * time.Millisecond)
		}
		m.setStatus(lecture, complete)
		m.phase = finished
		return m, tickAfter(650 * time.Millisecond)

	case finished:
		if m.current+1 >= len(m.queue) {
			return m, tea.Quit
		}
		m.current++
		m.phase = transcribing
		m.percent = 0
		m.frame = 0
		m.setStatus(m.queue[m.current], active)
		return m, tickAfter(25 * time.Millisecond)
	}
	return m, nil
}

func (m *model) setStatus(lecture lecture, value status) {
	if lecture.day == tuesday {
		m.weeks[lecture.week].tuesday[lecture.courseIndex] = value
		return
	}
	m.weeks[lecture.week].thursday[lecture.courseIndex] = value
}

func tickAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return tickMsg{} })
}
