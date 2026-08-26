package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	contents := `{
  "source": "` + filepath.Join(directory, "source") + `",
  "root": "` + filepath.Join(directory, "semester") + `",
  "timezone": "America/Los_Angeles",
  "semester": {"start": "2026-08-24", "end": "2026-12-18"},
  "courses": [{"name": "MATH351", "meetings": [{"days": ["Tue", "Thursday"], "start": "11:30", "end": "12:45"}]}]
}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if value.Model != DefaultModel || value.Location().String() != "America/Los_Angeles" || value.EndDate().Weekday() != time.Friday {
		t.Fatalf("loaded config = %#v", value)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"source":"a","root":"b","timezone":"UTC","unknown":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}
