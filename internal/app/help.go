package app

import (
	"fmt"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/ui"
)

var publicCommands = []ui.Command{
	{Name: "transcribe", Description: "Transcribe pending recordings"},
	{Name: "watch", Description: "Manage automatic Voice Memo routing"},
	{Name: "configure", Description: "Configure lectr interactively"},
	{Name: "help", Description: "Show this help"},
	{Name: "completion", Description: "Print zsh or bash completion"},
}

func isKnownCommand(name string) bool {
	if contains([]string{"route", "status", "demo"}, name) {
		return true
	}
	for _, command := range publicCommands {
		if command.Name == name {
			return true
		}
	}
	return false
}

func printUsage(configPath string) {
	path, err := config.ResolvePath(configPath)
	if err != nil {
		path = config.DefaultPath()
	}
	fmt.Print(ui.Help(
		"Route and transcribe lecture recordings locally on this Mac.",
		"lectr <command> [--config PATH] [options]",
		path,
		publicCommands,
	))
}

func printCommandUsage(command, configPath string) error {
	view, err := commandUsage(command, configPath)
	if err != nil {
		return err
	}
	fmt.Print(view)
	return nil
}

func commandUsage(command, configPath string) (string, error) {
	path, err := config.ResolvePath(configPath)
	if err != nil {
		path = config.DefaultPath()
	}
	switch command {
	case "transcribe":
		return ui.CommandHelp(
			"Transcribe pending lecture recordings locally with MLX Whisper.",
			"lectr transcribe [--config PATH] [--force] [--dry-run] [COURSE] [DATE|MEMO]",
			"Options",
			[]ui.Command{
				{Name: "--force", Description: "Replace existing part transcripts"},
				{Name: "--dry-run", Description: "Preview work without changing files"},
				{Name: "--config PATH", Description: "Use another config file"},
			},
			path,
			"With no filters, transcribes every pending recording.",
			"DATE uses YYYY-MM-DD; MEMO uses YYYY-MM-DD-ptNN.m4a.",
		), nil
	case "watch":
		return ui.CommandHelp(
			"Manage the optional launchd watcher for synced Apple Voice Memos.",
			"lectr watch [--config PATH] [install|uninstall|status]",
			"Actions",
			[]ui.Command{
				{Name: "install", Description: "Install and start the watcher"},
				{Name: "uninstall", Description: "Stop and remove the watcher"},
				{Name: "status", Description: "Show whether the watcher is installed"},
			},
			path,
			"Running lectr watch without an action shows status.",
			"Watcher logs are written to ~/Library/Logs/lectr.log.",
		), nil
	case "configure":
		return ui.CommandHelp(
			"Interactively configure storage, semester dates, courses, and schedules.",
			"lectr configure [--config PATH]",
			"Options",
			[]ui.Command{{Name: "--config PATH", Description: "Configure another file"}},
			path,
			"Existing values are prefilled; cancelling writes nothing.",
			"Set ACCESSIBLE=1 to use screen-reader-friendly prompts.",
		), nil
	case "completion":
		return ui.CommandHelp(
			"Print a shell completion script to standard output.",
			"lectr completion zsh|bash",
			"Shells",
			[]ui.Command{
				{Name: "zsh", Description: "Print Zsh completion"},
				{Name: "bash", Description: "Print Bash completion"},
			},
			"",
			"Example: source <(lectr completion zsh)",
		), nil
	case "route":
		return ui.CommandHelp(
			"Route synced Voice Memos into configured course folders.",
			"lectr route [--config PATH] [--dry-run] [--quiet]",
			"Options",
			[]ui.Command{
				{Name: "--dry-run", Description: "Preview copies without writing"},
				{Name: "--quiet", Description: "Hide the no-new-recordings message"},
			},
			path,
			"This internal command is used by the launchd watcher.",
		), nil
	case "status":
		return ui.CommandHelp(
			"Show configured paths, backlog counts, dependencies, and watcher state.",
			"lectr status [--config PATH]",
			"", nil, path,
			"This is a hidden diagnostic command.",
		), nil
	case "demo":
		return ui.CommandHelp(
			"Preview the shared terminal UI using fake recording data.",
			"lectr demo [fall|spring|stress]",
			"Profiles",
			[]ui.Command{
				{Name: "fall", Description: "Two-course fall queue"},
				{Name: "spring", Description: "Five-course spring queue"},
				{Name: "stress", Description: "Twenty-recording stress queue"},
			},
			"",
		), nil
	case "help":
		return ui.CommandHelp(
			"Show the command overview or detailed help for one command.",
			"lectr help [COMMAND]",
			"", nil, "",
		), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", command)
	}
}
