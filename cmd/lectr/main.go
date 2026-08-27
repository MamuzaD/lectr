package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/mamuzad/lectr/internal/config"
	configureui "github.com/mamuzad/lectr/internal/configure"
	"github.com/mamuzad/lectr/internal/demo"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
	"github.com/mamuzad/lectr/internal/watch"
)

var memoName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-pt\d{2}\.(m4a|mp3|wav)$`)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, transcribe.ErrCancelled) {
			fmt.Fprint(os.Stderr, ui.Page("Cancelled", ui.ErrorLine("lectr stopped safely")))
			os.Exit(130)
		}
		fmt.Fprint(os.Stderr, ui.Page("Something went wrong", ui.ErrorLine(err.Error()), ui.MutedText("Run lectr help to see available commands.")))
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	configPath, arguments, err := extractConfigFlag(arguments)
	if err != nil {
		return err
	}
	useThemeIfConfigured(configPath)
	if len(arguments) == 0 || arguments[0] == "-h" || arguments[0] == "--help" {
		printUsage(configPath)
		return nil
	}
	if arguments[0] == "help" {
		if len(arguments) == 1 {
			printUsage(configPath)
			return nil
		}
		if len(arguments) != 2 {
			return errors.New("usage: lectr help [COMMAND]")
		}
		return printCommandUsage(arguments[1], configPath)
	}
	if !contains([]string{"completion", "configure", "route", "transcribe", "watch", "status", "demo"}, arguments[0]) {
		return fmt.Errorf("unknown command %q; run lectr help", arguments[0])
	}
	if contains(arguments[1:], "-h") || contains(arguments[1:], "--help") {
		return printCommandUsage(arguments[0], configPath)
	}
	if arguments[0] == "configure" {
		return runConfigure(ctx, configPath, arguments[1:])
	}
	if arguments[0] == "completion" {
		return printCompletion(arguments[1:])
	}
	if arguments[0] == "demo" {
		if len(arguments) > 2 {
			return errors.New("usage: lectr demo [fall|spring|stress]")
		}
	}
	if arguments[0] == "status" && len(arguments) != 1 {
		return errors.New("usage: lectr status")
	}
	if arguments[0] == "watch" {
		if len(arguments) == 1 {
			return printWatcherStatus()
		}
		if len(arguments) != 2 {
			return errors.New("usage: lectr watch install|uninstall|status")
		}
		switch arguments[1] {
		case "uninstall":
			fmt.Print(ui.Page("Watcher", ui.MutedText("Removing the Voice Memos watcher…")))
			return watch.UninstallWatcher()
		case "status":
			return printWatcherStatus()
		case "install":
		default:
			return errors.New("usage: lectr watch install|uninstall|status")
		}
	}
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := ui.UseTheme(settings.Theme); err != nil {
		return err
	}
	switch arguments[0] {
	case "route":
		return runRoute(ctx, settings, arguments[1:])
	case "transcribe":
		return runTranscribe(ctx, settings, arguments[1:])
	case "watch":
		return runWatch(settings, arguments[1:])
	case "status":
		return printStatus(settings)
	case "demo":
		profile := "fall"
		if len(arguments) == 2 {
			profile = arguments[1]
		}
		return demo.Run(profile)
	}
	return nil
}

func useThemeIfConfigured(path string) {
	settings, err := config.Load(path)
	if err == nil {
		_ = ui.UseTheme(settings.Theme)
	}
}

func extractConfigFlag(arguments []string) (string, []string, error) {
	path := ""
	remaining := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--config" {
			if index+1 >= len(arguments) {
				return "", nil, errors.New("--config needs a path")
			}
			index++
			path = arguments[index]
		} else if strings.HasPrefix(argument, "--config=") {
			path = strings.TrimPrefix(argument, "--config=")
		} else {
			remaining = append(remaining, argument)
		}
	}
	return path, remaining, nil
}

func runRoute(ctx context.Context, settings config.Config, arguments []string) error {
	set := flag.NewFlagSet("lectr route", flag.ContinueOnError)
	dryRun := set.Bool("dry-run", false, "show copies without writing")
	quiet := set.Bool("quiet", false, "hide the no-new-recordings message")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if len(set.Args()) != 0 {
		return errors.New("usage: lectr route [--dry-run] [--quiet]")
	}
	_, err := watch.Route(ctx, watch.Options{
		Root: settings.Root, Source: settings.Source, Location: settings.Location(),
		Schedule: scheduleFrom(settings), DryRun: *dryRun, Quiet: *quiet,
	})
	return err
}

func runTranscribe(ctx context.Context, settings config.Config, arguments []string) error {
	flags, positionals, err := normalizeTranscribeArgs(arguments)
	if err != nil {
		return err
	}
	set := flag.NewFlagSet("lectr transcribe", flag.ContinueOnError)
	force := set.Bool("force", false, "replace existing part transcripts")
	dryRun := set.Bool("dry-run", false, "show work without changing files")
	if err := set.Parse(flags); err != nil {
		return err
	}
	if len(positionals) > 2 {
		return errors.New("usage: lectr transcribe [--force] [--dry-run] [COURSE] [DATE|MEMO]")
	}
	courses := settings.CourseNames()
	selector := ""
	if len(positionals) == 1 && transcribe.ValidateSelector(positionals[0]) {
		selector = positionals[0]
	} else if len(positionals) > 0 {
		if !contains(courses, positionals[0]) {
			return fmt.Errorf("course %q is not in the config", positionals[0])
		}
		courses = []string{positionals[0]}
		if len(positionals) == 2 {
			selector = positionals[1]
		}
	}
	if !transcribe.ValidateSelector(selector) {
		return errors.New("selector must be YYYY-MM-DD or YYYY-MM-DD-ptNN.m4a")
	}
	if *dryRun {
		fmt.Print(ui.Page("Transcribe", ui.MutedText("Previewing pending local recordings…")))
	}
	return transcribe.Run(ctx, transcribe.Options{
		Root: settings.Root, Courses: courses, Prompts: settings.Prompts(), Selector: selector,
		Model: settings.Model, Force: *force, DryRun: *dryRun,
	})
}

func normalizeTranscribeArgs(arguments []string) ([]string, []string, error) {
	flags, positionals := make([]string, 0), make([]string, 0)
	for _, argument := range arguments {
		switch argument {
		case "--force", "--dry-run", "-h", "--help":
			flags = append(flags, argument)
		default:
			if strings.HasPrefix(argument, "-") {
				return nil, nil, fmt.Errorf("unknown option %s", argument)
			}
			positionals = append(positionals, argument)
		}
	}
	return flags, positionals, nil
}

func runWatch(settings config.Config, arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: lectr watch install|uninstall|status")
	}
	switch arguments[0] {
	case "install":
		fmt.Print(ui.Page("Watcher", ui.MutedText("Installing the optional Voice Memos watcher…")))
		watching, log, err := watch.InstallWatcher(settings.Path(), settings.Source)
		if err != nil {
			return err
		}
		fmt.Println(ui.SuccessLine("Installed and started."))
		fmt.Println(ui.MutedText("Watching  ") + ui.Gradient(watching))
		fmt.Println(ui.MutedText("Log       ") + ui.Gradient(log))
		return nil
	case "uninstall":
		fmt.Print(ui.Page("Watcher", ui.MutedText("Removing the Voice Memos watcher…")))
		return watch.UninstallWatcher()
	case "status":
		return printWatcherStatus()
	default:
		return errors.New("usage: lectr watch install|uninstall|status")
	}
}

func printWatcherStatus() error {
	installed, path := watch.WatcherStatus()
	if installed {
		fmt.Print(ui.Page("Watcher", ui.SuccessLine("Installed"), ui.Gradient(path)))
	} else {
		fmt.Print(ui.Page("Watcher", ui.NeutralLine("Not installed"), ui.MutedText("Run lectr watch install to enable automatic routing.")))
	}
	return nil
}

func runConfigure(ctx context.Context, configPath string, arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("usage: lectr configure")
	}
	path, _ := config.ResolvePath(configPath)
	fmt.Print(ui.Page("Configure", ui.MutedText("Set up storage, semester dates, courses, and class schedules."), gradientPath(path), ""))
	savedPath, err := configureui.Run(ctx, configPath)
	if errors.Is(err, configureui.ErrCancelled) {
		fmt.Println(ui.NeutralLine("No changes saved."))
		return nil
	}
	if err != nil {
		return err
	}
	settings, _ := config.Load(savedPath)
	_ = ui.UseTheme(settings.Theme)
	fmt.Println(ui.SuccessLine("Configuration saved and valid."), ui.Gradient(savedPath))
	return nil
}

func gradientPath(path string) string { return "  " + ui.Gradient(path) }

func printCompletion(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: lectr completion zsh|bash")
	}
	script, err := completionScript(arguments[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "zsh":
		return `#compdef lectr

_lectr() {
  local -a commands
  commands=(
	'--config:Use another config file'
    'transcribe:Transcribe pending recordings'
    'watch:Manage automatic Voice Memo routing'
    'configure:Configure lectr interactively'
    'completion:Print shell completion'
    'help:Show help'
  )
  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi
  case $words[2] in
    transcribe) _arguments '--config[use another config file]:path:_files' '--force[replace existing part transcripts]' '--dry-run[preview without changing files]' '1:course or date:' '2:date or memo:' ;;
    watch) _values 'action' install uninstall status ;;
    completion) _values 'shell' zsh bash ;;
  esac
}

_lectr
`, nil
	case "bash":
		return `_lectr_completion() {
  local current command
  current="${COMP_WORDS[COMP_CWORD]}"
  command="${COMP_WORDS[1]}"
  if [[ $COMP_CWORD -eq 1 ]]; then
	COMPREPLY=( $(compgen -W '--config transcribe watch configure completion help' -- "$current") )
  elif [[ $command == watch ]]; then
    COMPREPLY=( $(compgen -W 'install uninstall status' -- "$current") )
  elif [[ $command == completion ]]; then
    COMPREPLY=( $(compgen -W 'zsh bash' -- "$current") )
  elif [[ $command == transcribe ]]; then
    COMPREPLY=( $(compgen -W '--force --dry-run' -- "$current") )
  fi
}
complete -F _lectr_completion lectr
`, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; choose zsh or bash", shell)
	}
}

func scheduleFrom(settings config.Config) watch.Schedule {
	schedule := watch.Schedule{Start: settings.StartDate(), End: settings.EndDate()}
	for _, course := range settings.Courses {
		for _, configured := range course.Meetings {
			start, _ := config.ClockMinutes(configured.Start)
			end, _ := config.ClockMinutes(configured.End)
			meeting := watch.ClassMeeting{Course: course.Name, Start: start, End: end}
			for _, name := range configured.Days {
				day, _ := config.ParseWeekday(name)
				meeting.Days = append(meeting.Days, day)
			}
			schedule.Meetings = append(schedule.Meetings, meeting)
		}
	}
	return schedule
}

func printStatus(settings config.Config) error {
	lines := []string{
		ui.MutedText("Config   ") + ui.Gradient(settings.Path()),
		ui.MutedText("Source   ") + ui.Gradient(settings.Source),
		ui.MutedText("Root     ") + ui.Gradient(settings.Root),
		ui.MutedText("Term     ") + fmt.Sprintf("%s to %s (%s)", settings.Semester.Start, settings.Semester.End, settings.Timezone),
		ui.MutedText("Theme    ") + settings.Theme,
		"",
	}
	for _, course := range settings.Courses {
		memos, pending, err := courseCounts(settings.Root, course.Name)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%-10s %d recordings  ·  %d awaiting transcription", course.Name, memos, pending))
	}
	installed, _ := watch.WatcherStatus()
	watcher := "not installed"
	if installed {
		watcher = "installed"
	}
	whisper := dependencyStatus("mlx_whisper")
	ffprobe := dependencyStatus("ffprobe")
	lines = append(lines, "", ui.MutedText("Watcher  ")+watcher, ui.MutedText("Tools    ")+fmt.Sprintf("mlx_whisper %s  ·  ffprobe %s", whisper, ffprobe))
	fmt.Print(ui.Page("Status", lines...))
	return nil
}

func courseCounts(root, course string) (int, int, error) {
	entries, err := os.ReadDir(filepath.Join(root, course, "memos"))
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	memos, pending := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !memoName.MatchString(entry.Name()) {
			continue
		}
		memos++
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if _, err := os.Stat(filepath.Join(root, course, "transcripts", stem+".txt")); os.IsNotExist(err) {
			pending++
		} else if err != nil {
			return 0, 0, err
		}
	}
	return memos, pending, nil
}

func dependencyStatus(name string) string {
	if _, err := exec.LookPath(name); err == nil {
		return "ready"
	}
	return "missing"
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
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
		"lectr [--config PATH] <command> [options]",
		path,
		[]ui.Command{
			{Name: "transcribe", Description: "Transcribe pending recordings"},
			{Name: "watch", Description: "Manage automatic Voice Memo routing"},
			{Name: "configure", Description: "Configure lectr interactively"},
			{Name: "completion", Description: "Print zsh or bash completion"},
			{Name: "help", Description: "Show this help"},
		},
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
			"lectr [--config PATH] transcribe [--force] [--dry-run] [COURSE] [DATE|MEMO]",
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
			"lectr [--config PATH] watch [install|uninstall|status]",
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
			"lectr [--config PATH] configure",
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
			"lectr [--config PATH] route [--dry-run] [--quiet]",
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
			"lectr [--config PATH] status",
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
