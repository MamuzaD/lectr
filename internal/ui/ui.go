package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

var (
	Orange       color.Color
	Amber        color.Color
	Brown        color.Color
	Green        color.Color
	Muted        color.Color
	Dim          color.Color
	White        color.Color
	Red          color.Color
	stops        [3]string
	verticalLogo bool
)

type Theme struct {
	Name                    string
	Accent, Highlight, Deep string
	Success, Muted, Dim     string
	Text, Error             string
	VerticalLogo            bool
}

var themes = map[string]Theme{
	"pumpkin-spice": {
		Name: "pumpkin-spice", Accent: "#F97316", Highlight: "#FBBF24", Deep: "#92400E",
		Success: "#A3B18A", Muted: "#A8A29E", Dim: "#57534E", Text: "#FFF7ED", Error: "#F87171",
	},
	"enchanting-table": {
		Name: "enchanting-table", Accent: "#55FFFF", Highlight: "#FF55FF", Deep: "#3B1C59",
		Success: "#55FFAA", Muted: "#A89BC2", Dim: "#554568", Text: "#F2ECFF", Error: "#FF5555",
		VerticalLogo: true,
	},
}

func init() { _ = UseTheme("pumpkin-spice") }

func UseTheme(name string) error {
	theme, ok := themes[name]
	if !ok {
		return fmt.Errorf("unknown theme %q", name)
	}
	Orange, Amber, Brown = lipgloss.Color(theme.Accent), lipgloss.Color(theme.Highlight), lipgloss.Color(theme.Deep)
	Green, Muted, Dim = lipgloss.Color(theme.Success), lipgloss.Color(theme.Muted), lipgloss.Color(theme.Dim)
	White, Red = lipgloss.Color(theme.Text), lipgloss.Color(theme.Error)
	stops = [3]string{theme.Deep, theme.Accent, theme.Highlight}
	verticalLogo = theme.VerticalLogo
	return nil
}

func ThemeNames() []string { return []string{"pumpkin-spice", "enchanting-table"} }

func FormTheme(bool) *huh.Styles {
	styles := huh.ThemeBase(true)
	styles.Focused.Base = styles.Focused.Base.BorderForeground(Brown)
	styles.Focused.Card = styles.Focused.Base
	styles.Focused.Title = styles.Focused.Title.Foreground(Orange).Bold(true)
	styles.Focused.NoteTitle = styles.Focused.NoteTitle.Foreground(Orange).Bold(true)
	styles.Focused.Description = styles.Focused.Description.Foreground(Muted)
	styles.Focused.ErrorIndicator = styles.Focused.ErrorIndicator.Foreground(Red)
	styles.Focused.ErrorMessage = styles.Focused.ErrorMessage.Foreground(Red)
	styles.Focused.SelectSelector = styles.Focused.SelectSelector.Foreground(Amber).SetString("› ")
	styles.Focused.MultiSelectSelector = styles.Focused.MultiSelectSelector.Foreground(Amber).SetString("› ")
	styles.Focused.SelectedOption = styles.Focused.SelectedOption.Foreground(Green)
	styles.Focused.SelectedPrefix = styles.Focused.SelectedPrefix.Foreground(Green).SetString("■ ")
	styles.Focused.UnselectedOption = styles.Focused.UnselectedOption.Foreground(White)
	styles.Focused.UnselectedPrefix = styles.Focused.UnselectedPrefix.Foreground(Dim).SetString("□ ")
	styles.Focused.Option = styles.Focused.Option.Foreground(White)
	styles.Focused.FocusedButton = styles.Focused.FocusedButton.Foreground(White).Background(Orange).Bold(true)
	styles.Focused.BlurredButton = styles.Focused.BlurredButton.Foreground(Muted).Background(Brown)
	styles.Focused.TextInput.Cursor = styles.Focused.TextInput.Cursor.Foreground(Amber)
	styles.Focused.TextInput.Prompt = styles.Focused.TextInput.Prompt.Foreground(Orange)
	styles.Focused.TextInput.Text = styles.Focused.TextInput.Text.Foreground(White)
	styles.Focused.TextInput.Placeholder = styles.Focused.TextInput.Placeholder.Foreground(Dim)
	styles.Blurred = styles.Focused
	styles.Blurred.Base = styles.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	styles.Blurred.Card = styles.Blurred.Base
	styles.Blurred.Title = styles.Blurred.Title.Foreground(Muted).Bold(false)
	styles.Blurred.NextIndicator = lipgloss.NewStyle()
	styles.Blurred.PrevIndicator = lipgloss.NewStyle()
	styles.Group.Title = styles.Focused.Title
	styles.Group.Description = styles.Focused.Description
	return styles
}

type Status uint8

const (
	Waiting Status = iota
	Active
	Complete
	Skipped
	Failed
)

type Memo struct {
	Part     string
	Duration string
	Status   Status
	Percent  int
	Detail   string
}

type Lecture struct {
	Course        string
	Date          string
	Memos         []Memo
	Combine       Status
	CombinedPath  string
	TranscriptDir string
	Frame         int
}

// TranscriptionQueue keeps completed lectures visible as compact receipts while
// the current lecture retains the detailed progress view.
func TranscriptionQueue(completed []Lecture, current Lecture) string {
	lines := make([]string, 0, len(completed)+2)
	for _, lecture := range completed {
		lines = append(lines, completionReceipt(lecture))
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, current.Render())
	return strings.Join(lines, "\n")
}

// CompletedTranscriptionQueue renders the settled state after the final
// lecture has finished, with no detailed card left pretending to be active.
func CompletedTranscriptionQueue(completed []Lecture) string {
	lines := make([]string, 0, len(completed)+4)
	for _, lecture := range completed {
		lines = append(lines, completionReceipt(lecture))
	}
	lines = append(lines, "")
	lines = append(lines, processingFooter()...)
	return strings.Join(lines, "\n")
}

func (lecture Lecture) Render() string {
	lines := []string{
		lipgloss.NewStyle().Foreground(Muted).Render(lecture.Course + "  ·  " + LectureTitle(lecture.Date) + "  ·  " + recordingCount(len(lecture.Memos))),
		"",
	}
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}[lecture.Frame%10]
	partLabel := "part"
	if len(lecture.Memos) != 1 {
		partLabel = "parts"
	}
	bar := NewProgress()
	for _, memo := range lecture.Memos {
		switch memo.Status {
		case Complete:
			lines = append(lines, statusLine("✓", Green, "pt"+memo.Part, "Complete", memo.Duration+" audio"), "")
		case Skipped:
			lines = append(lines, statusLine("✓", Green, "pt"+memo.Part, "Already exists", memo.Duration+" audio"), "")
		case Failed:
			lines = append(lines, statusLine("×", Red, "pt"+memo.Part, "Failed", memo.Duration+" audio"), "     "+lipgloss.NewStyle().Foreground(Red).Render(memo.Detail))
		case Active:
			detail := memo.Detail
			if detail == "" {
				detail = "Transcribing"
			}
			lines = append(lines,
				lipgloss.NewStyle().Foreground(Orange).Render(spinner)+"  pt"+memo.Part+"  "+lipgloss.NewStyle().Foreground(White).Render(detail)+MutedText("  "+memo.Duration+" audio"),
				"     "+bar.ViewAs(float64(memo.Percent)/100), "")
		default:
			lines = append(lines, lipgloss.NewStyle().Foreground(Dim).Render("○  pt"+memo.Part+"  Waiting        "+memo.Duration+" audio"), "")
		}
	}
	if lecture.Combine == Active {
		parts := make([]string, len(lecture.Memos))
		for index, memo := range lecture.Memos {
			parts[index] = "pt" + memo.Part
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(Orange).Render(spinner)+"  "+lipgloss.NewStyle().Foreground(White).Render(fmt.Sprintf("Combining %d %s", len(lecture.Memos), partLabel)), MutedText("     "+strings.Join(parts, " + ")+"  →  "+lecture.Date+".txt"))
	} else if lecture.Combine == Complete {
		lines = append(lines, lipgloss.NewStyle().Foreground(Green).Render("✓")+"  "+lipgloss.NewStyle().Foreground(White).Render(fmt.Sprintf("Combined %d %s", len(lecture.Memos), partLabel)), "     "+Gradient(relativeCombined(lecture.CombinedPath, lecture.TranscriptDir)))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(Dim).Render("○  Combine parts  Waiting"), "")
	}
	lines = append(lines, "")
	lines = append(lines, processingFooter()...)
	return strings.Join(lines, "\n")
}

func processingFooter() []string {
	return []string{MutedText("Local processing  ·  Recordings stay on this Mac"), "", Divider(52)}
}

func Shell(body string) string {
	return "\n" + Indent(Logo()) + "\n\n" + Indent(body) + "\n"
}

func NewProgress() progress.Model {
	bar := progress.New(progress.WithWidth(39), progress.WithColors(Brown, Orange, Amber), progress.WithFillCharacters('━', '─'))
	bar.PercentageStyle = lipgloss.NewStyle().Foreground(White).Bold(true)
	bar.EmptyColor = Dim
	return bar
}

func Logo() string {
	lines := []string{
		"▜     ▗   ",
		"▐ █▌▛▘▜▘▛▘",
		"▐▖▙▖▙▖▐▖▌ ",
	}
	if !verticalLogo {
		for index, line := range lines {
			lines[index] = Gradient(line)
		}
		return strings.Join(lines, "\n")
	}
	total := 0
	for _, line := range lines {
		total += len([]rune(line))
	}
	seen := 0
	for index, line := range lines {
		runes := []rune(line)
		var output strings.Builder
		for _, char := range runes {
			position := float64(seen) / float64(total-1)
			segment := position * float64(len(stops)-1)
			left := int(segment)
			if left >= len(stops)-1 {
				left, segment = len(stops)-2, float64(len(stops)-1)
			}
			color := blend(stops[left], stops[left+1], segment-float64(left))
			output.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(string(char)))
			seen++
		}
		lines[index] = output.String()
	}
	return strings.Join(lines, "\n")
}

func Gradient(value string) string {
	runes := []rune(value)
	if len(runes) < 2 {
		return value
	}
	var output strings.Builder
	for index, char := range runes {
		position := float64(index) / float64(len(runes)-1)
		segment := position * float64(len(stops)-1)
		left := int(segment)
		if left >= len(stops)-1 {
			left, segment = len(stops)-2, float64(len(stops)-1)
		}
		output.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(blend(stops[left], stops[left+1], segment-float64(left)))).Render(string(char)))
	}
	return output.String()
}

func Divider(width int) string {
	if width < 2 {
		return ""
	}
	return Gradient("╺" + strings.Repeat("━", width-2) + "╸")
}

func MutedText(value string) string { return lipgloss.NewStyle().Foreground(Muted).Render(value) }

func Indent(value string) string { return lipgloss.NewStyle().MarginLeft(2).Render(value) }

func LectureTitle(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%s %d, %d", parsed.Month(), parsed.Day(), parsed.Year())
}

func recordingCount(count int) string {
	if count == 1 {
		return "1 recording"
	}
	return fmt.Sprintf("%d recordings", count)
}

func completionReceipt(lecture Lecture) string {
	result := " transcribed"
	if len(lecture.Memos) > 0 {
		allSkipped := true
		for _, memo := range lecture.Memos {
			allSkipped = allSkipped && memo.Status == Skipped
		}
		if allSkipped {
			result = " already transcribed"
		}
	}
	return lipgloss.NewStyle().Foreground(Green).Render("✓") + "  " +
		lipgloss.NewStyle().Foreground(White).Bold(true).Render(lecture.Course) +
		MutedText("  ·  "+LectureTitle(lecture.Date)+"  ·  "+recordingCount(len(lecture.Memos))+result)
}

func statusLine(icon string, iconColor color.Color, part, status, detail string) string {
	return lipgloss.NewStyle().Foreground(iconColor).Render(icon) + "  " + part + "  " + lipgloss.NewStyle().Foreground(White).Render(status) + "       " + MutedText(detail)
}

func relativeCombined(path, directory string) string {
	if path == "" {
		return ""
	}
	if relative, err := filepath.Rel(directory, path); err == nil {
		return filepath.Join(filepath.Base(filepath.Dir(directory)), filepath.Base(directory), relative)
	}
	return path
}

func blend(from, to string, amount float64) string {
	var fr, fg, fb, tr, tg, tb int
	fmt.Sscanf(from, "#%02x%02x%02x", &fr, &fg, &fb)
	fmt.Sscanf(to, "#%02x%02x%02x", &tr, &tg, &tb)
	channel := func(start, end int) int { return start + int(float64(end-start)*amount) }
	return fmt.Sprintf("#%02x%02x%02x", channel(fr, tr), channel(fg, tg), channel(fb, tb))
}
