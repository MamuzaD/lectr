package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
	"github.com/mamuzad/lectr/internal/watch"
)

const permissionExitCode = 77

func runConfigFreeWatch(configPath string, arguments []string) (bool, error) {
	if len(arguments) == 0 {
		return true, printWatcherStatus(configPath)
	}
	if len(arguments) != 1 {
		return true, errors.New("usage: lectr watch install|uninstall|status")
	}
	switch arguments[0] {
	case "uninstall":
		fmt.Print(ui.Page("Watcher", ui.MutedText("Removing the Voice Memos watcher…")))
		return true, watch.UninstallWatcher()
	case "status":
		return true, printWatcherStatus(configPath)
	case "install":
		return false, nil
	default:
		return true, errors.New("usage: lectr watch install|uninstall|status")
	}
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

func runWatch(settings config.Config, arguments []string) error {
	if len(arguments) != 1 || arguments[0] != "install" {
		return errors.New("usage: lectr watch install|uninstall|status")
	}
	fmt.Print(ui.Page("Watcher", ui.MutedText("Installing the optional Voice Memos watcher…")))
	watching, log, err := watch.InstallWatcher(settings.Path(), settings.Source)
	if err != nil {
		return err
	}
	state := watch.WaitForInitialScan(5 * time.Second)
	if state.HasLastExit && state.LastExitCode == permissionExitCode {
		executable, pathErr := watch.ExecutablePath()
		if pathErr != nil {
			return pathErr
		}
		fmt.Println(ui.ErrorLine("The watcher cannot read Apple Voice Memos."))
		fmt.Println(ui.MutedText("Open System Settings → Privacy & Security → Full Disk Access."))
		fmt.Println(ui.MutedText("Add this executable: ") + ui.Gradient(executable))
		if !term.IsTerminal(os.Stdin.Fd()) {
			return errors.New("grant Full Disk Access, then run `lectr watch install` again")
		}
		fmt.Print("\nGrant access, then press Enter to retry: ")
		if _, readErr := bufio.NewReader(os.Stdin).ReadString('\n'); readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		previousRuns := state.Runs
		if err := watch.RetryWatcher(); err != nil {
			return err
		}
		state = watch.WaitForWatcherRun(previousRuns, 5*time.Second)
		if watcherRetryInProgress(state, previousRuns) {
			fmt.Println(ui.NeutralLine("Retry started; the initial scan is still running."))
			fmt.Println(ui.MutedText("Run `lectr watch status` in a moment to see the result."))
		} else if !state.HasLastExit || state.LastExitCode != 0 {
			return errors.New("the watcher still cannot read Voice Memos; verify Full Disk Access and try again")
		} else {
			fmt.Println(ui.SuccessLine("Access confirmed. Installed and started."))
		}
	} else if state.HasLastExit && state.LastExitCode != 0 {
		return fmt.Errorf("initial watcher scan failed (exit code %d); see %s", state.LastExitCode, log)
	} else if !state.HasLastExit {
		fmt.Println(ui.NeutralLine("Installed; the initial scan is still running."))
		fmt.Println(ui.MutedText("Run `lectr watch status` in a moment to see the result."))
	} else {
		fmt.Println(ui.SuccessLine("Installed and started."))
	}
	fmt.Println(ui.MutedText("Watching  ") + ui.Gradient(watching))
	fmt.Println(ui.MutedText("Log       ") + ui.Gradient(log))
	return nil
}

func watcherRetryInProgress(state watch.WatcherState, previousRuns int) bool {
	return state.Enabled && state.Running && state.Runs > previousRuns
}

func printWatcherStatus(configPath string) error {
	state := watch.WatcherStatus()
	settings, err := config.Load(configPath)
	if err != nil {
		lines := watcherStateLines(state)
		lines = append(lines,
			ui.MutedText("Agent     ")+ui.Gradient(state.AgentPath),
			ui.MutedText("Log       ")+ui.Gradient(state.LogPath),
			"",
			ui.ErrorLine("Config unavailable"),
			ui.MutedText(err.Error()),
		)
		fmt.Print(ui.Page("Watcher", lines...))
		return nil
	}
	pending, err := pendingForSettings(settings)
	if err != nil {
		return err
	}
	lines := watcherStatusLines(state, settings, pending)
	fmt.Print(ui.Page("Watcher", lines...))
	return nil
}

func pendingSinceTranscribe(configPath string) (int, error) {
	settings, err := config.Load(configPath)
	if err != nil {
		return 0, err
	}
	return pendingForSettings(settings)
}

func pendingForSettings(settings config.Config) (int, error) {
	total := 0
	for _, course := range settings.Courses {
		counts, err := transcribe.Inventory(settings.Root, course.Name)
		if err != nil {
			return 0, err
		}
		total += counts.Pending
	}
	return total, nil
}

func watcherStatusLines(state watch.WatcherState, settings config.Config, pending int) []string {
	lines := watcherStateLines(state)
	lines = append(lines,
		ui.MutedText("Watching  ")+ui.Gradient(settings.Source),
		ui.MutedText("Routing   ")+ui.Gradient(settings.Root),
		ui.MutedText("Config    ")+ui.Gradient(settings.Path()),
		ui.MutedText("Agent     ")+ui.Gradient(state.AgentPath),
		ui.MutedText("Log       ")+ui.Gradient(state.LogPath),
		"",
		ui.MutedText("Backlog   ")+backlogMessage(pending),
	)
	return lines
}

func watcherStateLines(state watch.WatcherState) []string {
	if state.Enabled && state.HasLastExit && state.LastExitCode != 0 {
		return []string{
			ui.ErrorLine(fmt.Sprintf("Installed, but the last scan failed (exit code %d).", state.LastExitCode)),
			ui.MutedText("If Voice Memos access was denied, run `lectr watch install` to repair it."),
			"",
		}
	}
	if state.Enabled {
		return []string{ui.SuccessLine("Enabled and watching for Voice Memos"), ""}
	}
	return []string{ui.NeutralLine("Disabled"), ui.MutedText("Run lectr watch install to enable automatic routing."), ""}
}

func backlogMessage(count int) string {
	if count == 0 {
		return "No recordings awaiting transcription"
	}
	return fmt.Sprintf("%s awaiting transcription", recordingCount(count))
}

func recordingCount(count int) string {
	if count == 1 {
		return "1 recording"
	}
	return fmt.Sprintf("%d recordings", count)
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
