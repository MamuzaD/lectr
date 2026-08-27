package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
)

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
