package configure

import (
	"path/filepath"
	"testing"

	"github.com/mamuzad/lectr/internal/config"
)

func TestBuildConfigPreservesMultipleMeetingInputs(t *testing.T) {
	base := config.Starter()
	base.Theme = "enchanting-table"
	courses := []courseInput{
		{
			name: "CS401", prompt: "Algorithms vocabulary", meetingCount: "2",
			meetings: []meetingInput{
				{days: []string{"Monday", "Wednesday"}, start: "09:00", end: "10:15"},
				{days: []string{"Friday"}, start: "11:00", end: "11:50"},
			},
		},
		{
			name: "MATH402", meetingCount: "1",
			meetings: []meetingInput{{days: []string{"Tuesday", "Thursday"}, start: "13:00", end: "14:15"}},
		},
	}
	result := buildConfig(base, courses)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, result); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Courses) != 2 || len(loaded.Courses[0].Meetings) != 2 || loaded.Courses[0].Meetings[1].Days[0] != "Friday" {
		t.Fatalf("saved courses = %#v", loaded.Courses)
	}
	if loaded.Theme != "enchanting-table" || loaded.Courses[0].Prompt != "Algorithms vocabulary" {
		t.Fatalf("saved config = %#v", loaded)
	}
}

func TestWizardValidators(t *testing.T) {
	if err := validTimezone("America/Los_Angeles"); err != nil {
		t.Fatal(err)
	}
	if err := validDate("2027-01-19"); err != nil {
		t.Fatal(err)
	}
	start := "11:30"
	if err := validEnd(&start)("12:45"); err != nil {
		t.Fatal(err)
	}
	if err := validEnd(&start)("10:45"); err == nil {
		t.Fatal("end time before start was accepted")
	}
	if err := atLeastOneDay(nil); err == nil {
		t.Fatal("empty weekday selection was accepted")
	}
}
