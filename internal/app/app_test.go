package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/ui"
	"github.com/mamuzad/lectr/internal/watch"
)

func TestPendingSinceTranscribeCountsUntranscribedMemos(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "MATH351", "memos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MATH351", "memos", "2026-08-25-pt01.m4a"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := fmt.Sprintf(`{
		"source": %q, "root": %q, "timezone": "America/Los_Angeles",
		"semester": {"start": "2026-08-24", "end": "2026-12-18"},
		"courses": [{"name": "MATH351", "meetings": []}]
	}`, root, root)
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	pending, err := pendingSinceTranscribe(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending = %d, want 1", pending)
	}
}

func TestWatcherStatusLinesDescribeTheWholeWorkflow(t *testing.T) {
	settings := loadTestConfig(t)
	state := watch.WatcherState{Enabled: true, AgentPath: "/tmp/local.lectr.route.plist", LogPath: "/tmp/lectr.log"}
	view := strings.Join(watcherStatusLines(state, settings, 2), "\n")
	for _, want := range []string{"Enabled and watching for Voice Memos", "2 recordings awaiting transcription"} {
		if !strings.Contains(view, want) {
			t.Errorf("watcher status missing %q:\n%s", want, view)
		}
	}
	for _, path := range []string{settings.Source, settings.Root, settings.Path(), state.AgentPath, state.LogPath} {
		if !strings.Contains(view, ui.Gradient(path)) {
			t.Errorf("watcher status missing path %q", path)
		}
	}
}

func TestDemoDoesNotRequireConfig(t *testing.T) {
	err := run(context.Background(), []string{"demo", "unknown", "--config", filepath.Join(t.TempDir(), "missing.json")})
	if err == nil || !strings.Contains(err.Error(), "unknown demo") {
		t.Fatalf("demo error = %v", err)
	}
}

func TestConfigFlagBeforeCommandIsRejected(t *testing.T) {
	err := run(context.Background(), []string{"--config", "/tmp/lectr.json", "demo"})
	if err == nil || !strings.Contains(err.Error(), "--config must come after a command") {
		t.Fatalf("expected --config before the command to be rejected, got %v", err)
	}
}

func TestExtractConfigFlagWorksBeforeOrAfterCommand(t *testing.T) {
	path, arguments, err := extractConfigFlag([]string{"transcribe", "MATH351", "--config", "/tmp/lectr.json", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/lectr.json" || !reflect.DeepEqual(arguments, []string{"transcribe", "MATH351", "--force"}) {
		t.Fatalf("path=%q arguments=%v", path, arguments)
	}
}

func TestCompletionScriptsCoverPublicCommands(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"transcribe", "watch", "configure", "completion", "help", "install", "uninstall", "status"} {
			if !strings.Contains(script, value) {
				t.Fatalf("%s completion missing %q", shell, value)
			}
		}
	}
	if _, err := completionScript("fish"); err == nil {
		t.Fatal("unsupported shell was accepted")
	}
}

func TestZshCompletionRegistersLectr(t *testing.T) {
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "compdef _lectr lectr") {
		t.Fatal("zsh completion does not register _lectr for lectr")
	}
}

func TestEachPublicCommandHasFocusedHelp(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{"transcribe", []string{"lectr transcribe [--config PATH]", "--force", "--dry-run", "YYYY-MM-DD"}},
		{"watch", []string{"lectr watch [--config PATH]", "install", "uninstall", "~/Library/Logs/lectr.log"}},
		{"configure", []string{"lectr configure [--config PATH]", "ACCESSIBLE=1", "cancelling writes nothing"}},
		{"completion", []string{"lectr completion zsh|bash", "source <(lectr completion zsh)"}},
	}
	for _, test := range tests {
		view, err := commandUsage(test.command, "/tmp/lectr.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range test.want {
			if !strings.Contains(view, value) {
				t.Fatalf("%s help missing %q", test.command, value)
			}
		}
		if strings.Contains(view, "Commands\n") {
			t.Fatalf("%s help fell back to the command overview", test.command)
		}
	}
}

func TestScheduleFromConfig(t *testing.T) {
	settings := loadTestConfig(t)
	schedule := scheduleFrom(settings)
	if len(schedule.Meetings) != 1 || schedule.Meetings[0].Start != 9*60+30 || schedule.Meetings[0].Days[0] != time.Wednesday {
		t.Fatalf("schedule = %#v", schedule)
	}
}

func loadTestConfig(t *testing.T) config.Config {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	contents := `{"source":"` + directory + `","root":"` + directory + `","timezone":"UTC","semester":{"start":"2026-01-01","end":"2026-05-01"},"courses":[{"name":"HIST200","meetings":[{"days":["Wednesday"],"start":"09:30","end":"10:45"}]}]}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}
