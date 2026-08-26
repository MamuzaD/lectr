package configure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/mamuzad/lectr/internal/config"
	"github.com/mamuzad/lectr/internal/ui"
)

var ErrCancelled = errors.New("configuration cancelled")

type courseInput struct {
	name         string
	prompt       string
	meetingCount string
	meetings     []meetingInput
}

type meetingInput struct {
	days  []string
	start string
	end   string
}

func Run(ctx context.Context, requestedPath string) (string, error) {
	path, err := config.ResolvePath(requestedPath)
	if err != nil {
		return "", err
	}
	draft := config.Starter()
	if _, statErr := os.Stat(path); statErr == nil {
		draft, err = config.Read(path)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	applyDefaults(&draft)

	courseCount := strconv.Itoa(max(1, len(draft.Courses)))
	if err := runForm(ctx,
		huh.NewGroup(
			huh.NewInput().Title("Voice Memos source").Description("Apple's synced recordings directory").Value(&draft.Source).Validate(defaulted(&draft.Source, required("source"))),
			huh.NewInput().Title("Lecture root").Description("Course folders and transcripts live here").Value(&draft.Root).Validate(defaulted(&draft.Root, required("root"))),
			huh.NewInput().Title("Timezone").Value(&draft.Timezone).Suggestions([]string{"America/Los_Angeles", "America/Denver", "America/Chicago", "America/New_York", "UTC"}).Validate(defaulted(&draft.Timezone, validTimezone)),
		).Title("Storage"),
		huh.NewGroup(
			huh.NewInput().Title("Semester start").Description("YYYY-MM-DD").Value(&draft.Semester.Start).Validate(defaulted(&draft.Semester.Start, validDate)),
			huh.NewInput().Title("Semester end").Description("YYYY-MM-DD").Value(&draft.Semester.End).Validate(defaulted(&draft.Semester.End, validDate)),
			huh.NewSelect[string]().Title("Theme").Options(
				huh.NewOption("Autumn pumpkin spice", "pumpkin-spice"),
				huh.NewOption("Minecraft enchanting table", "enchanting-table"),
			).Value(&draft.Theme),
			huh.NewInput().Title("Whisper model").Value(&draft.Model).Validate(defaulted(&draft.Model, required("model"))),
			huh.NewInput().Title("Number of courses").Value(&courseCount).Validate(defaulted(&courseCount, countBetween("courses", 1, 20))),
		).Title("Semester"),
	); err != nil {
		return "", err
	}

	count, _ := strconv.Atoi(courseCount)
	courses := initialCourses(draft.Courses, count)
	groups := make([]*huh.Group, len(courses))
	for index := range courses {
		course := &courses[index]
		groups[index] = huh.NewGroup(
			huh.NewInput().Title("Course name").Description("For example: MATH351").Value(&course.name).Validate(defaulted(&course.name, courseNameValidator(courses, index))),
			huh.NewText().Title("Whisper prompt (optional)").Description("Vocabulary or context that helps transcription").Value(&course.prompt).CharLimit(500),
			huh.NewInput().Title("Number of time slots").Description("One slot can cover multiple weekdays with the same hours").Value(&course.meetingCount).Validate(defaulted(&course.meetingCount, countBetween("time slots", 1, 8))),
		).Title(fmt.Sprintf("Course %d of %d", index+1, len(courses)))
	}
	if err := runForm(ctx, groups...); err != nil {
		return "", err
	}

	meetingGroups := make([]*huh.Group, 0)
	for courseIndex := range courses {
		course := &courses[courseIndex]
		meetingCount, _ := strconv.Atoi(course.meetingCount)
		course.meetings = initialMeetings(draft.Courses, courseIndex, meetingCount)
		for meetingIndex := range course.meetings {
			meeting := &course.meetings[meetingIndex]
			meetingGroups = append(meetingGroups, huh.NewGroup(
				huh.NewMultiSelect[string]().Title("Class days").Options(weekdayOptions()...).Value(&meeting.days).Validate(atLeastOneDay),
				huh.NewInput().Title("Start time").Description("24-hour HH:MM").Value(&meeting.start).Validate(defaulted(&meeting.start, validClock)),
				huh.NewInput().Title("End time").Description("24-hour HH:MM").Value(&meeting.end).Validate(defaulted(&meeting.end, validEnd(&meeting.start))),
			).Title(fmt.Sprintf("%s · time slot %d of %d", course.name, meetingIndex+1, meetingCount)))
		}
	}
	if err := runForm(ctx, meetingGroups...); err != nil {
		return "", err
	}

	result := buildConfig(draft, courses)
	confirmed := true
	if err := runForm(ctx, huh.NewGroup(
		huh.NewNote().Title("Ready to save").Description(summary(path, result)),
		huh.NewConfirm().Title("Save this configuration?").Affirmative("Save").Negative("Go back").Value(&confirmed),
	).Title("Review")); err != nil {
		return "", err
	}
	if !confirmed {
		return "", ErrCancelled
	}
	if err := config.Save(path, result); err != nil {
		return "", err
	}
	return path, nil
}

func runForm(ctx context.Context, groups ...*huh.Group) error {
	form := huh.NewForm(groups...).
		WithTheme(huh.ThemeFunc(ui.FormTheme)).
		WithAccessible(os.Getenv("ACCESSIBLE") != "").
		WithOutput(os.Stdout)
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrCancelled
		}
		return err
	}
	return nil
}

func applyDefaults(value *config.Config) {
	starter := config.Starter()
	if value.Source == "" {
		value.Source = starter.Source
	}
	if value.Root == "" {
		value.Root = starter.Root
	}
	if value.Timezone == "" {
		value.Timezone = starter.Timezone
	}
	if value.Model == "" {
		value.Model = starter.Model
	}
	if value.Theme == "" {
		value.Theme = starter.Theme
	}
	if value.Semester.Start == "" {
		value.Semester.Start = starter.Semester.Start
	}
	if value.Semester.End == "" {
		value.Semester.End = starter.Semester.End
	}
}

func initialCourses(existing []config.Course, count int) []courseInput {
	values := make([]courseInput, count)
	for index := range values {
		values[index] = courseInput{name: fmt.Sprintf("COURSE%d", index+1), meetingCount: "1"}
		if index < len(existing) {
			values[index].name = existing[index].Name
			values[index].prompt = existing[index].Prompt
			values[index].meetingCount = strconv.Itoa(max(1, len(existing[index].Meetings)))
		}
	}
	return values
}

func initialMeetings(existing []config.Course, courseIndex, count int) []meetingInput {
	values := make([]meetingInput, count)
	for index := range values {
		values[index] = meetingInput{days: []string{"Monday"}, start: "09:00", end: "10:00"}
		if courseIndex < len(existing) && index < len(existing[courseIndex].Meetings) {
			meeting := existing[courseIndex].Meetings[index]
			values[index] = meetingInput{days: append([]string(nil), meeting.Days...), start: meeting.Start, end: meeting.End}
		}
	}
	return values
}

func buildConfig(base config.Config, courses []courseInput) config.Config {
	base.Courses = make([]config.Course, len(courses))
	for courseIndex, input := range courses {
		course := config.Course{Name: input.name, Prompt: input.prompt, Meetings: make([]config.Meeting, len(input.meetings))}
		for meetingIndex, meeting := range input.meetings {
			course.Meetings[meetingIndex] = config.Meeting{Days: append([]string(nil), meeting.days...), Start: meeting.start, End: meeting.end}
		}
		base.Courses[courseIndex] = course
	}
	return base
}

func summary(path string, value config.Config) string {
	meetings := 0
	for _, course := range value.Courses {
		meetings += len(course.Meetings)
	}
	return fmt.Sprintf("%d courses · %d time slots\n%s to %s · %s\n%s", len(value.Courses), meetings, value.Semester.Start, value.Semester.End, value.Theme, path)
}

func required(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

func defaulted(current *string, validate func(string) error) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" && strings.TrimSpace(*current) != "" {
			return nil
		}
		return validate(value)
	}
}

func validTimezone(value string) error {
	if _, err := time.LoadLocation(value); err != nil {
		return errors.New("enter an IANA timezone such as America/Los_Angeles")
	}
	return nil
}

func validDate(value string) error {
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return errors.New("use YYYY-MM-DD")
	}
	return nil
}

func validClock(value string) error {
	if _, err := config.ClockMinutes(value); err != nil {
		return errors.New("use 24-hour HH:MM")
	}
	return nil
}

func validEnd(start *string) func(string) error {
	return func(end string) error {
		startMinutes, startErr := config.ClockMinutes(*start)
		endMinutes, endErr := config.ClockMinutes(end)
		if startErr != nil || endErr != nil {
			return errors.New("use 24-hour HH:MM")
		}
		if endMinutes <= startMinutes {
			return errors.New("end must be after start")
		}
		return nil
	}
}

func countBetween(name string, low, high int) func(string) error {
	return func(value string) error {
		count, err := strconv.Atoi(value)
		if err != nil || count < low || count > high {
			return fmt.Errorf("%s must be between %d and %d", name, low, high)
		}
		return nil
	}
}

func courseNameValidator(courses []courseInput, current int) func(string) error {
	return func(value string) error {
		if err := config.ValidateCourseName(value); err != nil {
			return err
		}
		for index, course := range courses {
			if index != current && strings.EqualFold(course.name, value) {
				return errors.New("course names must be unique")
			}
		}
		return nil
	}
}

func atLeastOneDay(days []string) error {
	if len(days) == 0 {
		return errors.New("choose at least one day")
	}
	return nil
}

func weekdayOptions() []huh.Option[string] {
	return huh.NewOptions("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")
}
