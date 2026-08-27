package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/mamuzad/lectr/internal/config"
	configureui "github.com/mamuzad/lectr/internal/configure"
	"github.com/mamuzad/lectr/internal/ui"
)

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
