package app

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
	"github.com/mamuzad/lectr/internal/watch"
)

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
	fmt.Println(ui.SuccessLine("Installed and started."))
	fmt.Println(ui.MutedText("Watching  ") + ui.Gradient(watching))
	fmt.Println(ui.MutedText("Log       ") + ui.Gradient(log))
	return nil
}

func printWatcherStatus(configPath string) error {
	installed, path := watch.WatcherStatus()
	lines := []string{}
	if installed {
		lines = append(lines, ui.SuccessLine("Installed"), ui.Gradient(path))
	} else {
		lines = append(lines, ui.NeutralLine("Not installed"), ui.MutedText("Run lectr watch install to enable automatic routing."))
	}
	if pending, err := pendingSinceTranscribe(configPath); err == nil {
		message := fmt.Sprintf("%s synced since your last transcribe", recordingCount(pending))
		if pending == 0 {
			message = "Nothing synced since your last transcribe"
		}
		lines = append(lines, "", ui.MutedText(message))
	}
	fmt.Print(ui.Page("Watcher", lines...))
	return nil
}

func pendingSinceTranscribe(configPath string) (int, error) {
	settings, err := config.Load(configPath)
	if err != nil {
		return 0, err
	}
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
