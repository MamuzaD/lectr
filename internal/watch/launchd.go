package watch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const LaunchAgentLabel = "local.lectr.route"

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

func launchLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "lectr.log")
}

func launchAgentConfigFor(configPath, source, executable string) (string, error) {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	escape := func(value string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>route</string><string>--config</string><string>%s</string><string>--quiet</string></array>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string></dict>
<key>WatchPaths</key><array><string>%s</string></array><key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Background</string>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, LaunchAgentLabel, escape(executable), escape(configPath), escape(source), escape(launchLogPath()), escape(launchLogPath())), nil
}

func InstallWatcher(configPath, source string) (string, string, error) {
	executable, err := stableExecutable()
	if err != nil {
		return "", "", err
	}
	return installWatcher(configPath, source, executable, runLaunchctl)
}

type launchctlCommand func(...string) error

func runLaunchctl(arguments ...string) error {
	output, err := exec.Command("launchctl", arguments...).CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("launchctl %s: %w", arguments[0], err)
	}
	return fmt.Errorf("launchctl %s: %w: %s", arguments[0], err, detail)
}

func installWatcher(configPath, source, executable string, launchctl launchctlCommand) (string, string, error) {
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	config, err := launchAgentConfigFor(configPath, source, executable)
	if err != nil {
		return "", "", err
	}
	previous, readErr := os.ReadFile(path)
	hadPrevious := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", "", readErr
	}
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		return "", "", err
	}
	rollback := func() {
		if hadPrevious {
			_ = os.WriteFile(path, previous, 0o644)
			return
		}
		_ = os.Remove(path)
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = launchctl("bootout", uid, path)
	if err := launchctl("bootstrap", uid, path); err != nil {
		rollback()
		return "", "", err
	}
	if err := launchctl("kickstart", uid+"/"+LaunchAgentLabel); err != nil {
		_ = launchctl("bootout", uid, path)
		rollback()
		return "", "", err
	}
	return source, launchLogPath(), nil
}

func stableExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	return validateStableExecutable(executable)
}

func validateStableExecutable(executable string) (string, error) {
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("lectr executable is not a runnable regular file: %s", executable)
	}
	for _, component := range strings.Split(filepath.Clean(executable), string(filepath.Separator)) {
		if strings.HasPrefix(component, "go-build") {
			return "", errors.New("cannot install from go run; run `make build`, then `./lectr watch install`")
		}
	}
	return executable, nil
}

func UninstallWatcher() error {
	path := launchAgentPath()
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = runLaunchctl("bootout", uid, path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Uninstalled %s\n", LaunchAgentLabel)
	return nil
}

type WatcherState struct {
	Enabled   bool
	AgentPath string
	LogPath   string
}

func WatcherStatus() WatcherState {
	return watcherStatus(runLaunchctl)
}

func watcherStatus(launchctl launchctlCommand) WatcherState {
	path := launchAgentPath()
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	return WatcherState{
		Enabled:   launchctl("print", uid+"/"+LaunchAgentLabel) == nil,
		AgentPath: path,
		LogPath:   launchLogPath(),
	}
}
