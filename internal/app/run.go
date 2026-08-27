package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/demo"
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
		if errors.Is(err, os.ErrPermission) {
			return permissionExitCode
		}
		return 1
	}
	return 0
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		printUsage("")
		return nil
	}
	command := arguments[0]
	configPath, rest, err := extractConfigFlag(arguments[1:])
	if err != nil {
		return err
	}
	useThemeIfConfigured(configPath)
	if command == "-h" || command == "--help" {
		printUsage(configPath)
		return nil
	}
	if command == "help" {
		if len(rest) == 0 {
			printUsage(configPath)
			return nil
		}
		if len(rest) != 1 {
			return errors.New("usage: lectr help [COMMAND]")
		}
		return printCommandUsage(rest[0], configPath)
	}
	if !isKnownCommand(command) {
		if command == "--config" || strings.HasPrefix(command, "--config=") {
			return errors.New("--config must come after a command, e.g. lectr transcribe --config PATH")
		}
		return fmt.Errorf("unknown command %q; run lectr help", command)
	}
	if contains(rest, "-h") || contains(rest, "--help") {
		return printCommandUsage(command, configPath)
	}
	if command == "configure" {
		return runConfigure(ctx, configPath, rest)
	}
	if command == "completion" {
		return printCompletion(rest)
	}
	if command == "demo" {
		if len(rest) > 1 {
			return errors.New("usage: lectr demo [fall|spring|stress]")
		}
		profile := "fall"
		if len(rest) == 1 {
			profile = rest[0]
		}
		return demo.Run(profile)
	}
	if command == "status" && len(rest) != 0 {
		return errors.New("usage: lectr status")
	}
	if command == "watch" {
		handled, err := runConfigFreeWatch(configPath, rest)
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
	switch command {
	case "route":
		return runRoute(ctx, settings, rest)
	case "transcribe":
		return runTranscribe(ctx, settings, rest)
	case "watch":
		return runWatch(settings, rest)
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
