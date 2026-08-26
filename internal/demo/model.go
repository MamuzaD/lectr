package demo

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
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

type lecture struct {
	course   string
	date     string
	label    string
	duration string
}

type fixture struct {
	lectures   []lecture
	todayCount int
	audio      string
}

type tickMsg struct{}

type Model struct {
	fixture  fixture
	screen   screen
	cursor   int
	selected []bool
	queue    []lecture
	current  int
	phase    phase
	percent  int
	frame    int
}

func New() Model { return newModel(fallFixture()) }

func NewProfile(profile string) (Model, error) {
	var value fixture
	switch profile {
	case "", "fall":
		value = fallFixture()
	case "spring":
		value = springFixture()
	case "stress":
		value = stressFixture()
	default:
		return Model{}, fmt.Errorf("unknown demo %q; choose fall, spring, or stress", profile)
	}
	return newModel(value), nil
}

func newModel(value fixture) Model {
	return Model{fixture: value, screen: backlogScreen, selected: make([]bool, len(value.lectures))}
}

func fallFixture() fixture {
	return fixture{
		todayCount: 2,
		audio:      "about 9h 45m audio",
		lectures: []lecture{
			{course: "MATH351", date: "2026-11-10", label: "Tue, Nov 10", duration: "1:11:08"},
			{course: "MATH451", date: "2026-11-10", label: "Tue, Nov 10", duration: "1:13:26"},
			{course: "MATH351", date: "2026-11-12", label: "Thu, Nov 12", duration: "1:09:41"},
			{course: "MATH451", date: "2026-11-12", label: "Thu, Nov 12", duration: "1:14:02"},
			{course: "MATH351", date: "2026-11-17", label: "Tue, Nov 17", duration: "1:10:35"},
			{course: "MATH451", date: "2026-11-17", label: "Tue, Nov 17", duration: "1:12:19"},
			{course: "MATH351", date: "2026-11-19", label: "Thu, Nov 19", duration: "1:12:44"},
			{course: "MATH451", date: "2026-11-19", label: "Thu, Nov 19", duration: "1:14:18"},
		},
	}
}

func springFixture() fixture {
	courses := []string{"CS401", "MATH402", "PHYS310", "HIST220", "ENGL305"}
	dates := []string{"2027-04-27", "2027-04-28", "2027-04-29", "2027-04-30", "2027-05-03", "2027-05-04", "2027-05-05", "2027-05-06", "2027-05-07", "2027-05-10"}
	durations := []string{"1:08:14", "1:16:42", "1:02:31", "52:18", "1:21:06", "1:11:27", "1:14:09", "1:05:44", "55:02", "1:18:36"}
	lectures := make([]lecture, len(dates))
	for index, date := range dates {
		lectures[index] = demoLecture(courses[index%len(courses)], date, durations[index])
	}
	return fixture{lectures: lectures, todayCount: 5, audio: "about 11h 25m audio"}
}

func stressFixture() fixture {
	courses := [][2]string{{"CS401", "MATH402"}, {"PHYS310", "HIST220"}, {"CS401", "ENGL305"}, {"MATH402", "PHYS310"}, {"HIST220", "ENGL305"}}
	durations := []string{
		"1:08:14", "1:16:42", "1:02:31", "52:18", "1:21:06", "1:11:27", "1:14:09", "1:05:44", "48:12", "51:36",
		"1:09:55", "1:13:08", "58:47", "1:04:29", "1:17:33", "1:10:02", "1:06:51", "1:00:14", "46:38", "49:21",
	}
	lectures := make([]lecture, 0, len(durations))
	start := time.Date(2027, time.April, 26, 0, 0, 0, 0, time.Local)
	for week := 0; week < 2; week++ {
		for weekday, pair := range courses {
			date := start.AddDate(0, 0, week*7+weekday)
			for _, course := range pair {
				index := len(lectures)
				lectures = append(lectures, demoLecture(course, date.Format("2006-01-02"), durations[index]))
			}
		}
	}
	return fixture{lectures: lectures, todayCount: 2, audio: "about 21h audio"}
}

func demoLecture(course, date, duration string) lecture {
	parsed, _ := time.Parse("2006-01-02", date)
	label := parsed.Format("Mon, Jan 2")
	return lecture{course: course, date: date, label: label, duration: duration}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
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

func (m Model) updateBacklog(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cursor = (m.cursor + 3) % 4
	case "down", "j":
		m.cursor = (m.cursor + 1) % 4
	case "enter":
		switch m.cursor {
		case 0:
			return m.start(append([]lecture(nil), m.fixture.lectures...))
		case 1:
			start := max(0, len(m.fixture.lectures)-m.fixture.todayCount)
			return m.start(append([]lecture(nil), m.fixture.lectures[start:]...))
		case 2:
			m.screen, m.cursor = pickerScreen, 0
			m.selected = make([]bool, len(m.fixture.lectures))
		case 3:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) updatePicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	count := len(m.fixture.lectures)
	if count == 0 {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.cursor = (m.cursor + count - 1) % count
	case "down", "j":
		m.cursor = (m.cursor + 1) % count
	case "space", " ":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "esc":
		m.screen, m.cursor = backlogScreen, 0
	case "enter":
		queue := make([]lecture, 0, count)
		for index, selected := range m.selected {
			if selected {
				queue = append(queue, m.fixture.lectures[index])
			}
		}
		if len(queue) > 0 {
			return m.start(queue)
		}
	}
	return m, nil
}

func (m Model) start(queue []lecture) (tea.Model, tea.Cmd) {
	m.screen, m.queue, m.current = workingScreen, queue, 0
	m.phase, m.percent, m.frame = transcribing, 0, 0
	return m, tickAfter(25 * time.Millisecond)
}

func (m Model) updateProgress() (tea.Model, tea.Cmd) {
	m.frame++
	switch m.phase {
	case transcribing:
		m.percent += 4
		if m.percent >= 100 {
			m.percent, m.phase, m.frame = 100, combining, 0
			return m, tickAfter(60 * time.Millisecond)
		}
		return m, tickAfter(25 * time.Millisecond)
	case combining:
		if m.frame < 12 {
			return m, tickAfter(60 * time.Millisecond)
		}
		m.phase = finished
		return m, tickAfter(650 * time.Millisecond)
	case finished:
		if m.current+1 >= len(m.queue) {
			return m, tea.Quit
		}
		m.current++
		m.phase, m.percent, m.frame = transcribing, 0, 0
		return m, tickAfter(25 * time.Millisecond)
	}
	return m, nil
}

func tickAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return tickMsg{} })
}
