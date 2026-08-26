package demo

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	orange = lipgloss.Color("#F97316")
	amber  = lipgloss.Color("#FBBF24")
	brown  = lipgloss.Color("#92400E")
	green  = lipgloss.Color("#A3B18A")
	muted  = lipgloss.Color("#A8A29E")
	dim    = lipgloss.Color("#57534E")
	white  = lipgloss.Color("#FFF7ED")
)

func newProgress() progress.Model {
	bar := progress.New(
		progress.WithWidth(39),
		progress.WithColors(brown, orange, amber),
		progress.WithFillCharacters('━', '─'),
	)
	bar.PercentageStyle = lipgloss.NewStyle().Foreground(white).Bold(true)
	bar.EmptyColor = dim
	return bar
}

func (m model) View() tea.View {
	return tea.NewView(m.render())
}

func (m model) render() string {
	var body string
	switch m.screen {
	case pickerScreen:
		body = m.pickerView()
	case workingScreen:
		body = m.workingView()
	default:
		body = m.backlogView()
	}

	return "\n" + indent(logo()) + "\n\n" + indent(m.graphView()) + "\n\n" + indent(divider(52)) + "\n\n" + indent(body) + "\n"
}

func (m model) graphView() string {
	var months strings.Builder
	var dates strings.Builder
	var tuesdays strings.Builder
	var thursdays strings.Builder

	for _, week := range m.weeks {
		months.WriteString(fmt.Sprintf("%-4s", week.month))
		dates.WriteString(fmt.Sprintf("%-4s", week.date))
		tuesdays.WriteString(pair(week.tuesday))
		thursdays.WriteString(pair(week.thursday))
	}

	title := lipgloss.NewStyle().Foreground(white).Bold(true).Render("Lecture activity")
	rangeText := lipgloss.NewStyle().Foreground(muted).Render("Aug–Nov 2026")
	label := lipgloss.NewStyle().Foreground(muted).Width(11)

	return strings.Join([]string{
		title + lipgloss.NewStyle().Foreground(dim).Render("  ·  ") + rangeText,
		label.Render("") + lipgloss.NewStyle().Foreground(brown).Render(months.String()),
		label.Render("") + lipgloss.NewStyle().Foreground(muted).Render(dates.String()),
		label.Render("Tuesday") + tuesdays.String(),
		label.Render("Thursday") + thursdays.String(),
		label.Render("") + lipgloss.NewStyle().Foreground(muted).Render("each pair: 351 451"),
		label.Render("") + square(complete) + mutedText(" transcribed   ") + square(active) + mutedText(" active   ") + square(ready) + mutedText(" ready   ") + square(missing) + mutedText(" no memo"),
	}, "\n")
}

func (m model) backlogView() string {
	options := []struct {
		label string
		note  string
	}{
		{"Catch up everything", "8 recordings"},
		{"Today only", "2 recordings"},
		{"Choose recordings", ""},
		{"Exit", ""},
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(white).Render("8 recordings ready") + mutedText("  ·  4 lecture days  ·  about 9h 45m audio"),
		"",
		lipgloss.NewStyle().Foreground(white).Bold(true).Render("What do you want to transcribe?"),
		"",
	}
	for index, option := range options {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(white)
		if index == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(orange).Render("› ")
			style = style.Foreground(white).Bold(true)
		}
		lines = append(lines, cursor+style.Width(26).Render(option.label)+mutedText(option.note))
	}
	lines = append(lines, "", controls("↑↓ move", "enter select"), mutedText("Local processing  ·  Recordings stay on this Mac"))
	return strings.Join(lines, "\n")
}

func (m model) pickerView() string {
	lines := []string{
		lipgloss.NewStyle().Foreground(white).Bold(true).Render("Choose recordings"),
		"",
	}
	for index, lecture := range readyLectures {
		cursor := "  "
		if index == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(orange).Render("› ")
		}
		box := lipgloss.NewStyle().Foreground(dim).Render("□")
		if m.selected[index] {
			box = lipgloss.NewStyle().Foreground(green).Render("■")
		}
		label := lipgloss.NewStyle().Foreground(white).Width(25).Render(lecture.label + "  " + lecture.course)
		lines = append(lines, cursor+box+" "+label+mutedText(lecture.duration))
	}
	lines = append(lines, "", controls("↑↓ move", "space toggle", "enter confirm", "esc back"))
	return strings.Join(lines, "\n")
}

func (m model) workingView() string {
	lecture := m.queue[m.current]
	heading := mutedText(lecture.course + "  ·  " + lectureTitle(lecture.date) + "  ·  1 recording")
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}[m.frame%10]

	lines := []string{heading, ""}
	switch m.phase {
	case transcribing:
		detail := "Transcribing"
		if m.percent < 12 {
			detail = "Starting Whisper"
		} else if m.percent < 28 {
			detail = "Preparing model"
		}
		lines = append(lines,
			lipgloss.NewStyle().Foreground(orange).Render(spinner)+"  pt01  "+lipgloss.NewStyle().Foreground(white).Render(detail)+mutedText("  "+lecture.duration+" audio"),
			"   "+m.progress.ViewAs(float64(m.percent)/100),
			"",
			lipgloss.NewStyle().Foreground(dim).Render("○  Combine parts  Waiting"),
		)
	case combining:
		lines = append(lines,
			lipgloss.NewStyle().Foreground(green).Render("✓")+"  pt01  "+lipgloss.NewStyle().Foreground(white).Render("Complete")+mutedText("       "+lecture.duration+" audio"),
			"",
			lipgloss.NewStyle().Foreground(orange).Render(spinner)+"  "+lipgloss.NewStyle().Foreground(white).Render("Combining 1 part"),
			mutedText("   pt01  →  "+lecture.date+".txt"),
		)
	case finished:
		lines = append(lines,
			lipgloss.NewStyle().Foreground(green).Render("✓")+"  pt01  "+lipgloss.NewStyle().Foreground(white).Render("Complete")+mutedText("       "+lecture.duration+" audio"),
			"",
			lipgloss.NewStyle().Foreground(green).Render("✓")+"  "+lipgloss.NewStyle().Foreground(white).Render("Combined 1 part"),
			"   "+gradient(lecture.course+"/transcripts/"+lecture.date+".txt", []string{"#92400E", "#EA580C", "#FBBF24"}),
		)
	}
	lines = append(lines, "", mutedText("Local processing  ·  Recordings stay on this Mac"))
	return strings.Join(lines, "\n")
}

func pair(values [2]status) string {
	return square(values[0]) + square(values[1]) + " "
}

func square(value status) string {
	style := lipgloss.NewStyle()
	switch value {
	case complete:
		return style.Foreground(green).Render("■")
	case active:
		return style.Foreground(orange).Render("■")
	case ready:
		return style.Foreground(dim).Render("□")
	default:
		return style.Foreground(dim).Render("·")
	}
}

func controls(parts ...string) string {
	return mutedText(strings.Join(parts, "  ·  "))
}

func mutedText(value string) string {
	return lipgloss.NewStyle().Foreground(muted).Render(value)
}

func lectureTitle(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%s %d, %d", parsed.Month(), parsed.Day(), parsed.Year())
}

func divider(width int) string {
	return gradient("╺"+strings.Repeat("━", width-2)+"╸", []string{"#92400E", "#EA580C", "#FBBF24"})
}

func logo() string {
	lines := []string{
		"╺┳╸┏━┓┏━┓┏┓╻┏━┓┏━╸┏━┓╻┏┓ ┏━╸",
		" ┃ ┣┳┛┣━┫┃┗┫┗━┓┃  ┣┳┛┃┣┻┓┣╸ ",
		" ╹ ╹┗╸╹ ╹╹ ╹┗━┛┗━╸╹┗╸╹┗━┛┗━╸",
	}
	for index, line := range lines {
		lines[index] = gradient(line, []string{"#92400E", "#EA580C", "#FBBF24"})
	}
	return strings.Join(lines, "\n")
}

func gradient(value string, colors []string) string {
	runes := []rune(value)
	if len(runes) < 2 {
		return value
	}
	var output strings.Builder
	for index, char := range runes {
		position := float64(index) / float64(len(runes)-1)
		segment := position * float64(len(colors)-1)
		left := int(segment)
		if left >= len(colors)-1 {
			left = len(colors) - 2
			segment = float64(len(colors) - 1)
		}
		color := blend(colors[left], colors[left+1], segment-float64(left))
		output.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(string(char)))
	}
	return output.String()
}

func blend(from, to string, amount float64) string {
	var fr, fg, fb, tr, tg, tb int
	fmt.Sscanf(from, "#%02x%02x%02x", &fr, &fg, &fb)
	fmt.Sscanf(to, "#%02x%02x%02x", &tr, &tg, &tb)
	channel := func(start, end int) int { return start + int(float64(end-start)*amount) }
	return fmt.Sprintf("#%02x%02x%02x", channel(fr, tr), channel(fg, tg), channel(fb, tb))
}

func indent(value string) string {
	return lipgloss.NewStyle().MarginLeft(2).Render(value)
}
