package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mamuzad/lectr/internal/ui"
)

const DefaultModel = "mlx-community/whisper-large-v3-turbo"

var (
	memoPattern     = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-pt(\d{2})(\.m4a|\.mp3|\.wav)$`)
	selectorPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:-pt\d{2}\.(?:m4a|mp3|wav))?$`)
	ansiPattern     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	percentPattern  = regexp.MustCompile(`(?:^|[^0-9])(\d{1,3})%\|`)
	coursePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
)

var ErrCancelled = errors.New("transcription cancelled")

type Options struct {
	Root     string
	Courses  []string
	Prompts  map[string]string
	Selector string
	Model    string
	Force    bool
	DryRun   bool
}

type Counts struct {
	Recordings int
	Pending    int
}

type memoStatus = ui.Status

const (
	waiting  = ui.Waiting
	active   = ui.Active
	complete = ui.Complete
	skipped  = ui.Skipped
	failed   = ui.Failed
)

type Memo struct {
	Course   string
	Path     string
	Date     string
	Part     string
	Stem     string
	Duration string
	Status   memoStatus
	Percent  int
	Detail   string
}

type group struct {
	Course        string
	Date          string
	Memos         []Memo
	TranscriptDir string
}

type event struct {
	Group         int
	Index         int
	Status        memoStatus
	Percent       int
	Detail        string
	Combine       memoStatus
	CombinedPath  string
	HasMemoUpdate bool
	HasCombine    bool
	AllComplete   bool
}

func ValidateSelector(value string) bool {
	return value == "" || selectorPattern.MatchString(value)
}

func Inventory(root, course string) (Counts, error) {
	entries, err := os.ReadDir(filepath.Join(root, course, "memos"))
	if os.IsNotExist(err) {
		return Counts{}, nil
	}
	if err != nil {
		return Counts{}, err
	}
	var counts Counts
	for _, entry := range entries {
		if entry.IsDir() || !memoPattern.MatchString(entry.Name()) {
			continue
		}
		counts.Recordings++
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if _, err := os.Stat(filepath.Join(root, course, "transcripts", stem+".txt")); os.IsNotExist(err) {
			counts.Pending++
		} else if err != nil {
			return Counts{}, err
		}
	}
	return counts, nil
}

func Run(ctx context.Context, options Options) error {
	if options.Root == "" {
		return errors.New("root is required")
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return err
	}
	options.Root = root
	if len(options.Courses) == 0 {
		return errors.New("at least one course is required")
	}
	for _, course := range options.Courses {
		if !coursePattern.MatchString(course) {
			return fmt.Errorf("invalid course directory name: %q", course)
		}
	}
	if options.Model == "" {
		options.Model = DefaultModel
	}
	if !options.DryRun {
		if _, err := exec.LookPath("mlx_whisper"); err != nil {
			return errors.New("mlx_whisper is missing; install it with: uv tool install mlx-whisper")
		}
	}

	groups, err := discoverGroups(options)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		target := "."
		if options.Selector != "" {
			target = ": " + options.Selector
		}
		return fmt.Errorf("no correctly named memos found%s", target)
	}
	if options.DryRun {
		return printDryRun(groups, options)
	}
	for index := range groups {
		if err := prepareGroup(&groups[index], options.Force); err != nil {
			return err
		}
	}
	if !options.Force {
		groups = pendingGroups(groups)
	}
	if len(groups) == 0 {
		fmt.Print(ui.Page("Nothing to transcribe", ui.NeutralLine("Every recording is already transcribed.")))
		return nil
	}
	return runGroups(ctx, groups, options)
}

// pendingGroups drops groups whose memos were all already transcribed on a
// previous run, so today's queue only shows what actually runs today.
func pendingGroups(values []group) []group {
	pending := make([]group, 0, len(values))
	for _, value := range values {
		hasPending := false
		for _, memo := range value.Memos {
			if memo.Status != skipped {
				hasPending = true
				break
			}
		}
		if hasPending {
			pending = append(pending, value)
		}
	}
	return pending
}

func discoverGroups(options Options) ([]group, error) {
	groupsByKey := make(map[string]*group)
	keys := make([]string, 0)
	for _, course := range options.Courses {
		courseDir := filepath.Join(options.Root, course)
		info, err := os.Stat(courseDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("course directory not found: %s", courseDir)
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("course directory not found: %s", courseDir)
		}
		memoDir := filepath.Join(courseDir, "memos")
		entries, err := os.ReadDir(memoDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("%s: no memos directory; skipping\n", course)
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			matches := memoPattern.FindStringSubmatch(entry.Name())
			if matches == nil {
				if ext := strings.ToLower(filepath.Ext(entry.Name())); ext == ".m4a" || ext == ".mp3" || ext == ".wav" {
					fmt.Fprintf(os.Stderr, "%s: skipping incorrectly named memo: %s\n", course, entry.Name())
				}
				continue
			}
			if options.Selector != "" && entry.Name() != options.Selector && matches[1] != options.Selector {
				continue
			}
			key := course + "\x00" + matches[1]
			if groupsByKey[key] == nil {
				groupsByKey[key] = &group{
					Course: course, Date: matches[1],
					TranscriptDir: filepath.Join(courseDir, "transcripts"),
				}
				keys = append(keys, key)
			}
			path := filepath.Join(memoDir, entry.Name())
			groupsByKey[key].Memos = append(groupsByKey[key].Memos, Memo{
				Course: course, Path: path, Date: matches[1], Part: matches[2],
				Stem:     strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
				Duration: audioDuration(path), Status: waiting,
			})
		}
	}
	sort.Strings(keys)
	groups := make([]group, 0, len(keys))
	for _, key := range keys {
		value := groupsByKey[key]
		sort.Slice(value.Memos, func(i, j int) bool { return value.Memos[i].Path < value.Memos[j].Path })
		groups = append(groups, *value)
	}
	return groups, nil
}

func audioDuration(path string) string {
	command := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	output, err := command.Output()
	if err != nil {
		return "--:--"
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return "--:--"
	}
	total := int(seconds + 0.5)
	hours, minutes, remainder := total/3600, total%3600/60, total%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remainder)
	}
	return fmt.Sprintf("%02d:%02d", minutes, remainder)
}

func prepareGroup(value *group, force bool) error {
	if err := os.MkdirAll(value.TranscriptDir, 0o755); err != nil {
		return err
	}
	for index := range value.Memos {
		transcript := filepath.Join(value.TranscriptDir, value.Memos[index].Stem+".txt")
		if _, err := os.Stat(transcript); err == nil && !force {
			problem, err := transcriptQualityProblem(transcript)
			if err != nil {
				return err
			}
			if problem != "" {
				return fmt.Errorf("%s: %s detected in %s; run again with --force", value.Course, problem, filepath.Base(transcript))
			}
			value.Memos[index].Status = skipped
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func printDryRun(groups []group, options Options) error {
	for _, value := range groups {
		for _, memo := range value.Memos {
			transcript := filepath.Join(value.TranscriptDir, memo.Stem+".txt")
			_, err := os.Stat(transcript)
			action := "transcribe"
			if err == nil && options.Force {
				action = "replace"
			} else if err == nil {
				action = "skip existing"
			} else if !os.IsNotExist(err) {
				return err
			}
			fmt.Printf("%s: would %s: %s\n", value.Course, action, filepath.Base(memo.Path))
		}
		fmt.Printf("%s: would combine parts -> transcripts/%s.txt\n", value.Course, value.Date)
	}
	return nil
}
