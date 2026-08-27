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
<key>ProgramArguments</key><array><string>%s</string><string>--config</string><string>%s</string><string>route</string><string>--quiet</string></array>
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
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	config, err := launchAgentConfigFor(configPath, source, executable)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		return "", "", err
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", uid, path).Run()
	if err := exec.Command("launchctl", "bootstrap", uid, path).Run(); err != nil {
		return "", "", err
	}
	if err := exec.Command("launchctl", "kickstart", uid+"/"+LaunchAgentLabel).Run(); err != nil {
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
	_ = exec.Command("launchctl", "bootout", uid, path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Uninstalled %s\n", LaunchAgentLabel)
	return nil
}

func WatcherStatus() (bool, string) {
	path := launchAgentPath()
	_, err := os.Stat(path)
	return err == nil, path
}
