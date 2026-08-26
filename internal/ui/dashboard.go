package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type MenuOption struct {
	Label  string
	Detail string
}

type Command struct {
	Name        string
	Description string
}

func Help(description, usage, configPath string, commands []Command) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(White).Render(description), "",
		lipgloss.NewStyle().Foreground(Muted).Render("Usage"),
		"  " + lipgloss.NewStyle().Foreground(White).Bold(true).Render(usage), "",
		lipgloss.NewStyle().Foreground(Muted).Render("Commands"),
	}
	for _, command := range commands {
		name := lipgloss.NewStyle().Foreground(White).Bold(true).Width(13).Render(command.Name)
		lines = append(lines, "  "+name+MutedText(command.Description))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(Muted).Render("Config"), "  "+Gradient(configPath))
	return Shell(strings.Join(lines, "\n"))
}

func CommandHelp(description, usage, section string, entries []Command, configPath string, notes ...string) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(White).Render(description), "",
		lipgloss.NewStyle().Foreground(Muted).Render("Usage"),
		"  " + lipgloss.NewStyle().Foreground(White).Bold(true).Render(usage),
	}
	if len(entries) > 0 {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(Muted).Render(section))
		for _, entry := range entries {
			name := lipgloss.NewStyle().Foreground(White).Bold(true).Width(16).Render(entry.Name)
			lines = append(lines, "  "+name+MutedText(entry.Description))
		}
	}
	if len(notes) > 0 {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(Muted).Render("Notes"))
		for _, note := range notes {
			lines = append(lines, "  "+MutedText(note))
		}
	}
	if configPath != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(Muted).Render("Config"), "  "+Gradient(configPath))
	}
	return Shell(strings.Join(lines, "\n"))
}

func Page(title string, lines ...string) string {
	body := lipgloss.NewStyle().Foreground(White).Bold(true).Render(title)
	if len(lines) > 0 {
		body += "\n\n" + strings.Join(lines, "\n")
	}
	return Shell(body)
}

func SuccessLine(value string) string {
	return lipgloss.NewStyle().Foreground(Green).Render("◆") + "  " + lipgloss.NewStyle().Foreground(White).Render(value)
}

func ErrorLine(value string) string {
	return lipgloss.NewStyle().Foreground(Red).Render("■") + "  " + lipgloss.NewStyle().Foreground(White).Render(value)
}

func NeutralLine(value string) string {
	return lipgloss.NewStyle().Foreground(Muted).Render("◇") + "  " + MutedText(value)
}

func BacklogMenu(summary, question string, options []MenuOption, cursor int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(White).Render(summary), "",
		lipgloss.NewStyle().Foreground(White).Bold(true).Render(question), "",
	}
	for index, option := range options {
		pointer := "  "
		style := lipgloss.NewStyle().Foreground(White)
		if index == cursor {
			pointer = lipgloss.NewStyle().Foreground(Orange).Render("› ")
			style = style.Bold(true)
		}
		lines = append(lines, pointer+style.Width(24).Render(option.Label)+MutedText(option.Detail))
	}
	return strings.Join(append(lines, "", Controls("↑↓ move", "enter select", "q/esc exit"), MutedText("Local processing  ·  Recordings stay on this Mac")), "\n")
}

type RecordingChoice struct {
	Label    string
	Duration string
	Selected bool
}

func RecordingPicker(choices []RecordingChoice, cursor int) string {
	lines := []string{lipgloss.NewStyle().Foreground(White).Bold(true).Render("Choose recordings"), ""}
	const windowSize = 8
	start := 0
	if cursor >= windowSize {
		start = cursor - windowSize + 1
	}
	if len(choices)-start < windowSize && len(choices) > windowSize {
		start = len(choices) - windowSize
	}
	end := min(start+windowSize, len(choices))
	if start > 0 {
		lines = append(lines, MutedText(fmt.Sprintf("  ↑ %d more", start)))
	}
	for index := start; index < end; index++ {
		choice := choices[index]
		pointer := "  "
		if index == cursor {
			pointer = lipgloss.NewStyle().Foreground(Orange).Render("› ")
		}
		box := lipgloss.NewStyle().Foreground(Dim).Render("□")
		if choice.Selected {
			box = lipgloss.NewStyle().Foreground(Green).Render("■")
		}
		label := lipgloss.NewStyle().Foreground(White).Width(23).Render(choice.Label)
		lines = append(lines, pointer+box+" "+label+MutedText(choice.Duration))
	}
	if end < len(choices) {
		lines = append(lines, MutedText(fmt.Sprintf("  ↓ %d more", len(choices)-end)))
	}
	return strings.Join(append(lines, "", Controls("↑↓ move", "space toggle", "enter confirm", "esc back")), "\n")
}

func Controls(parts ...string) string { return MutedText(strings.Join(parts, "  ·  ")) }
