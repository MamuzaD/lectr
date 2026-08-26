package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultModel = "mlx-community/whisper-large-v3-turbo"

var courseNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Config struct {
	Source    string   `json:"source"`
	Root      string   `json:"root"`
	Timezone  string   `json:"timezone"`
	Model     string   `json:"model"`
	Theme     string   `json:"theme"`
	Semester  Semester `json:"semester"`
	Courses   []Course `json:"courses"`
	path      string
	location  *time.Location
	startDate time.Time
	endDate   time.Time
}

type Semester struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Course struct {
	Name     string    `json:"name"`
	Prompt   string    `json:"prompt,omitempty"`
	Meetings []Meeting `json:"meetings"`
}

type Meeting struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

func DefaultPath() string {
	if value := os.Getenv("LECTR_CONFIG"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(home, ".config", "lectr", "config.json")
}

func ResolvePath(path string) (string, error) {
	if path == "" {
		path = DefaultPath()
	}
	return expandPath(path)
}

func Starter() Config {
	year := time.Now().Year()
	return Config{
		Source:   "~/Library/Group Containers/group.com.apple.VoiceMemos.shared/Recordings",
		Root:     "~/lectures",
		Timezone: localTimezone(),
		Model:    DefaultModel,
		Theme:    "pumpkin-spice",
		Semester: Semester{Start: fmt.Sprintf("%d-01-01", year), End: fmt.Sprintf("%d-12-31", year)},
		Courses: []Course{{
			Name:     "COURSE101",
			Meetings: []Meeting{{Days: []string{"Monday"}, Start: "09:00", End: "10:00"}},
		}},
	}
}

func localTimezone() string {
	if value := os.Getenv("TZ"); value != "" {
		return value
	}
	if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if index := strings.LastIndex(path, "/zoneinfo/"); index >= 0 {
			return path[index+len("/zoneinfo/"):]
		}
	}
	if value := time.Now().Location().String(); value != "Local" {
		return value
	}
	return "UTC"
}

func Load(path string) (Config, error) {
	value, err := Read(path)
	if err != nil {
		return Config{}, err
	}
	if err := value.prepare(); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", value.path, err)
	}
	return value, nil
}

func Read(path string) (Config, error) {
	path, err := ResolvePath(path)
	if err != nil {
		return Config{}, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config not found: %s (create it from config.example.json)", path)
		}
		return Config{}, err
	}
	var value Config
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("read %s: unexpected trailing JSON", path)
	}
	value.path = path
	return value, nil
}

func Save(path string, value Config) error {
	resolved, err := ResolvePath(path)
	if err != nil {
		return err
	}
	validated := value
	validated.path = resolved
	if err := validated.prepare(); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(resolved), ".config-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, resolved)
}

func (c *Config) prepare() error {
	var err error
	c.Source, err = expandPath(c.Source)
	if err != nil {
		return err
	}
	c.Root, err = expandPath(c.Root)
	if err != nil {
		return err
	}
	if c.Source == "" || c.Root == "" {
		return errors.New("source and root are required")
	}
	if c.Timezone == "" {
		return errors.New("timezone is required")
	}
	c.location, err = time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q", c.Timezone)
	}
	c.startDate, err = time.ParseInLocation(time.DateOnly, c.Semester.Start, c.location)
	if err != nil {
		return fmt.Errorf("semester.start must be YYYY-MM-DD")
	}
	c.endDate, err = time.ParseInLocation(time.DateOnly, c.Semester.End, c.location)
	if err != nil {
		return fmt.Errorf("semester.end must be YYYY-MM-DD")
	}
	c.endDate = c.endDate.AddDate(0, 0, 1).Add(-time.Nanosecond)
	if c.endDate.Before(c.startDate) {
		return errors.New("semester.end must not precede semester.start")
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Theme == "" {
		c.Theme = "pumpkin-spice"
	}
	if c.Theme != "pumpkin-spice" && c.Theme != "enchanting-table" {
		return errors.New(`theme must be "pumpkin-spice" or "enchanting-table"`)
	}
	if len(c.Courses) == 0 {
		return errors.New("at least one course is required")
	}
	seen := make(map[string]bool)
	for _, course := range c.Courses {
		if !courseNamePattern.MatchString(course.Name) {
			return fmt.Errorf("invalid course name %q", course.Name)
		}
		if seen[course.Name] {
			return fmt.Errorf("duplicate course %q", course.Name)
		}
		seen[course.Name] = true
		for _, meeting := range course.Meetings {
			start, startErr := clockMinutes(meeting.Start)
			end, endErr := clockMinutes(meeting.End)
			if startErr != nil || endErr != nil || end <= start {
				return fmt.Errorf("%s meeting must have valid HH:MM start and end times", course.Name)
			}
			if len(meeting.Days) == 0 {
				return fmt.Errorf("%s meeting requires at least one day", course.Name)
			}
			for _, day := range meeting.Days {
				if _, err := ParseWeekday(day); err != nil {
					return fmt.Errorf("%s: %w", course.Name, err)
				}
			}
		}
	}
	return nil
}

func (c Config) Path() string             { return c.path }
func (c Config) Location() *time.Location { return c.location }
func (c Config) StartDate() time.Time     { return c.startDate }
func (c Config) EndDate() time.Time       { return c.endDate }

func (c Config) CourseNames() []string {
	names := make([]string, len(c.Courses))
	for index, course := range c.Courses {
		names[index] = course.Name
	}
	return names
}

func (c Config) Prompts() map[string]string {
	prompts := make(map[string]string, len(c.Courses))
	for _, course := range c.Courses {
		prompts[course.Name] = course.Prompt
	}
	return prompts
}

func ClockMinutes(value string) (int, error) { return clockMinutes(value) }

func ValidateCourseName(value string) error {
	if !courseNamePattern.MatchString(value) {
		return errors.New("use letters, numbers, hyphens, or underscores")
	}
	return nil
}

func clockMinutes(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, errors.New("invalid time")
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, errors.New("invalid time")
	}
	return hour*60 + minute, nil
}

func ParseWeekday(value string) (time.Weekday, error) {
	for day := time.Sunday; day <= time.Saturday; day++ {
		if strings.EqualFold(value, day.String()) || strings.EqualFold(value, day.String()[:3]) {
			return day, nil
		}
	}
	return 0, fmt.Errorf("invalid weekday %q", value)
}

func expandPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return filepath.Abs(value)
}
