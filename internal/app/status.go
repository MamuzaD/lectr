package app

import (
	"fmt"
	"os/exec"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
	"github.com/mamuzad/lectr/internal/watch"
)

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
		counts, err := transcribe.Inventory(settings.Root, course.Name)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%-10s %d recordings  ·  %d awaiting transcription", course.Name, counts.Recordings, counts.Pending))
	}
	if sourceCount, err := watch.SourceInventory(settings.Source); err != nil {
		lines = append(lines, "", ui.ErrorLine("Source    ")+err.Error())
	} else {
		lines = append(lines, "", ui.MutedText("Source    ")+fmt.Sprintf("%d Voice Memos visible to this process", sourceCount))
	}
	watcherState := watch.WatcherStatus()
	watcher := "not installed"
	if watcherState.Enabled {
		watcher = "enabled"
	}
	whisper := dependencyStatus("mlx_whisper")
	ffprobe := dependencyStatus("ffprobe")
	lines = append(lines, "", ui.MutedText("Watcher  ")+watcher, ui.MutedText("Tools    ")+fmt.Sprintf("mlx_whisper %s  ·  ffprobe %s", whisper, ffprobe))
	fmt.Print(ui.Page("Status", lines...))
	return nil
}

func dependencyStatus(name string) string {
	if _, err := exec.LookPath(name); err == nil {
		return "ready"
	}
	return "missing"
}
