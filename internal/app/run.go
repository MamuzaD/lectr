package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/transcribe"
	"github.com/mamuzad/lectr/internal/ui"
)

func Run(ctx context.Context, arguments []string) int {
	if err := run(ctx, arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, transcribe.ErrCancelled) {
			fmt.Fprint(os.Stderr, ui.Page("Cancelled", ui.ErrorLine("lectr stopped safely")))
			return 130
		}
		fmt.Fprint(os.Stderr, ui.Page("Something went wrong", ui.ErrorLine(err.Error()), ui.MutedText("Run lectr help to see available commands.")))
		return 1
	}
	return 0
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
	if !isKnownCommand(arguments[0]) {
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
	if arguments[0] == "status" && len(arguments) != 1 {
		return errors.New("usage: lectr status")
	}
	if arguments[0] == "watch" {
		handled, err := runConfigFreeWatch(arguments[1:])
		if handled {
			return err
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
