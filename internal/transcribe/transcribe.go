package transcribe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
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
	return runGroups(ctx, groups, options)
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
			passes, err := transcriptPassesQualityCheck(transcript)
			if err != nil {
				return err
			}
			if !passes {
				return fmt.Errorf("%s: repetition loop detected in %s; run again with --force", value.Course, filepath.Base(transcript))
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

func runGroups(parent context.Context, values []group, options Options) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	events := make(chan event, 128)
	result := make(chan error, 1)
	emit := func(message event) bool {
		select {
		case events <- message:
			return true
		case <-ctx.Done():
			return false
		}
	}
	terminal := isTerminal(os.Stdout) && isTerminal(os.Stdin)
	var interfaceModel model
	if terminal {
		interfaceModel = newModel(values, events, cancel)
	}
	go func() {
		result <- processGroups(ctx, values, options, emit)
		close(events)
	}()

	if !terminal {
		for message := range events {
			printEvent(&values[message.Group], message)
		}
		return normalizeResult(<-result)
	}

	program := tea.NewProgram(interfaceModel)
	returned, err := program.Run()
	if err != nil {
		cancel()
		<-result
		return err
	}
	state := returned.(model)
	if state.cancelled {
		cancel()
	}
	return normalizeResult(<-result)
}

func processGroups(ctx context.Context, values []group, options Options, emit func(event) bool) error {
	for groupIndex := range values {
		forward := func(message event) bool {
			message.Group = groupIndex
			return emit(message)
		}
		if err := processGroup(ctx, &values[groupIndex], options, forward); err != nil {
			return err
		}
	}
	if !emit(event{AllComplete: true}) {
		return ctx.Err()
	}
	return nil
}

func normalizeResult(err error) error {
	if errors.Is(err, context.Canceled) {
		return ErrCancelled
	}
	return err
}

func isTerminal(file *os.File) bool {
	return term.IsTerminal(file.Fd())
}

func printEvent(value *group, message event) {
	if message.HasMemoUpdate && (message.Status == complete || message.Status == failed) {
		memo := value.Memos[message.Index]
		label := "complete"
		if message.Status == failed {
			label = "failed: " + message.Detail
		}
		fmt.Printf("%s: %s: %s\n", value.Course, label, filepath.Base(memo.Path))
	}
	if message.HasCombine && message.Combine == complete {
		fmt.Printf("%s: combined -> %s\n", value.Course, message.CombinedPath)
	}
}

func processGroup(ctx context.Context, value *group, options Options, emit func(event) bool) error {
	for index := range value.Memos {
		if value.Memos[index].Status == skipped {
			continue
		}
		if err := transcribeMemo(ctx, value, index, options, emit); err != nil {
			return err
		}
	}
	if !emit(event{Combine: active, HasCombine: true}) {
		return ctx.Err()
	}
	combined, err := combineParts(value.Course, value.Date, value.TranscriptDir)
	if err != nil {
		emit(event{Combine: failed, Detail: err.Error(), HasCombine: true})
		return err
	}
	emit(event{Combine: complete, CombinedPath: combined, HasCombine: true})
	return nil
}

func transcribeMemo(ctx context.Context, value *group, index int, options Options, emit func(event) bool) error {
	memo := &value.Memos[index]
	transcript := filepath.Join(value.TranscriptDir, memo.Stem+".txt")
	temporaryStem := fmt.Sprintf("%s-new-%d-%d", memo.Stem, os.Getpid(), time.Now().UnixNano())
	temporaryTranscript := filepath.Join(value.TranscriptDir, temporaryStem+".txt")
	defer os.Remove(temporaryTranscript)
	memo.Status, memo.Percent, memo.Detail = active, 0, "Starting Whisper"
	if !emit(event{Index: index, Status: active, Percent: 0, Detail: memo.Detail, HasMemoUpdate: true}) {
		return ctx.Err()
	}
	command := whisperCommand(*memo, value.TranscriptDir, temporaryStem, options.Model, options.Prompts[memo.Course])
	returnCode, logs, err := streamProcess(ctx, command, memo, emit, index)
	if err != nil {
		return err
	}
	if returnCode != 0 || !fileExists(temporaryTranscript) {
		memo.Status = failed
		memo.Detail = lastError(logs)
		if memo.Detail == "" {
			memo.Detail = "Whisper did not create a transcript"
		}
		emit(event{Index: index, Status: failed, Detail: memo.Detail, HasMemoUpdate: true})
		return fmt.Errorf("%s: transcription failed for %s", value.Course, filepath.Base(memo.Path))
	}
	passes, err := transcriptPassesQualityCheck(temporaryTranscript)
	if err != nil {
		return err
	}
	if !passes {
		rejected := filepath.Join(value.TranscriptDir, memo.Stem+".rejected.txt")
		if renameErr := os.Rename(temporaryTranscript, rejected); renameErr != nil {
			return renameErr
		}
		memo.Status, memo.Detail = failed, "Repetition loop detected; output saved as .rejected.txt"
		emit(event{Index: index, Status: failed, Detail: memo.Detail, HasMemoUpdate: true})
		return fmt.Errorf("%s: repetition loop detected in %s", value.Course, filepath.Base(rejected))
	}
	if err := os.Rename(temporaryTranscript, transcript); err != nil {
		return err
	}
	memo.Status, memo.Percent, memo.Detail = complete, 100, ""
	emit(event{Index: index, Status: complete, Percent: 100, HasMemoUpdate: true})
	return nil
}

func whisperCommand(memo Memo, transcriptDir, outputStem, model, prompt string) []string {
	if prompt == "" {
		prompt = "University lecture. Use appropriate subject terminology."
	}
	return []string{
		"mlx_whisper", memo.Path, "--model", model, "--language", "en",
		"--initial-prompt", prompt, "--condition-on-previous-text", "False",
		"--word-timestamps", "True", "--hallucination-silence-threshold", "2",
		"--output-dir", transcriptDir, "--output-name", outputStem,
		"--output-format", "txt", "--verbose", "False",
	}
}

func streamProcess(ctx context.Context, command []string, memo *Memo, emit func(event) bool, index int) (int, []string, error) {
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Cancel = func() error {
		if process.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-process.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	process.WaitDelay = 2 * time.Second
	pipe, err := process.StdoutPipe()
	if err != nil {
		return -1, nil, err
	}
	process.Stderr = process.Stdout
	if err := process.Start(); err != nil {
		return -1, nil, err
	}
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(pipe)
		var pending string
		buffer := make([]byte, 1024)
		for {
			n, readErr := reader.Read(buffer)
			if n > 0 {
				pending += string(buffer[:n])
				parts := strings.FieldsFunc(pending, func(r rune) bool { return r == '\r' || r == '\n' })
				if len(parts) > 0 {
					if !strings.HasSuffix(pending, "\r") && !strings.HasSuffix(pending, "\n") {
						pending = parts[len(parts)-1]
						parts = parts[:len(parts)-1]
					} else {
						pending = ""
					}
					for _, part := range parts {
						if part != "" {
							select {
							case lines <- part:
							case <-ctx.Done():
								return
							}
						}
					}
				}
			}
			if readErr != nil {
				if pending != "" {
					select {
					case lines <- pending:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()

	logs := make([]string, 0, 30)
	for line := range lines {
		clean := strings.TrimSpace(ansiPattern.ReplaceAllString(line, ""))
		if clean != "" {
			logs = append(logs, clean)
			if len(logs) > 30 {
				logs = logs[len(logs)-30:]
			}
		}
		parseProgress(line, memo)
		detail := memo.Detail
		if detail == "" {
			detail = "Transcribing"
		}
		if !emit(event{Index: index, Status: active, Percent: memo.Percent, Detail: detail, HasMemoUpdate: true}) {
			_ = process.Process.Kill()
		}
	}
	waitErr := process.Wait()
	if ctx.Err() != nil {
		return -1, logs, ctx.Err()
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return exitErr.ExitCode(), logs, nil
		}
		return -1, logs, waitErr
	}
	return 0, logs, nil
}

func parseProgress(message string, memo *Memo) {
	clean := strings.TrimSpace(ansiPattern.ReplaceAllString(message, ""))
	if match := percentPattern.FindStringSubmatch(clean); match != nil {
		if percent, err := strconv.Atoi(match[1]); err == nil && percent <= 100 {
			memo.Percent = percent
		}
	}
	if strings.Contains(clean, "frames/s") {
		memo.Detail = "Transcribing"
	} else if strings.Contains(clean, "Reconstruct") {
		memo.Detail = "Preparing model"
	} else if strings.Contains(clean, "Downloading") || strings.Contains(clean, "Fetching") {
		memo.Detail = "Downloading model"
	}
}

func lastError(logs []string) string {
	for index := len(logs) - 1; index >= 0; index-- {
		if strings.Contains(strings.ToLower(logs[index]), "error") {
			return logs[index]
		}
	}
	return ""
}

func transcriptPassesQualityCheck(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	repeated := 0
	previous := ""
	for scanner.Scan() {
		line := strings.Join(strings.Fields(scanner.Text()), " ")
		if len(line) > 3 && line == previous {
			repeated++
			if repeated >= 6 {
				return false, nil
			}
		} else {
			repeated = 0
		}
		previous = line
	}
	return true, scanner.Err()
}

func combineParts(course, date, transcriptDir string) (string, error) {
	parts, err := filepath.Glob(filepath.Join(transcriptDir, date+"-pt[0-9][0-9].txt"))
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%s: no transcripts available to combine for %s", course, date)
	}
	sort.Strings(parts)
	for _, part := range parts {
		passes, err := transcriptPassesQualityCheck(part)
		if err != nil {
			return "", err
		}
		if !passes {
			return "", fmt.Errorf("%s: repetition loop detected in %s; rerun with --force", course, filepath.Base(part))
		}
	}
	temporary, err := os.CreateTemp(transcriptDir, ".combine-*.txt")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	for _, part := range parts {
		partNumber := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(part), date+"-pt"), ".txt")
		if _, err := fmt.Fprintf(temporary, "===== Part %s =====\n\n", partNumber); err != nil {
			temporary.Close()
			return "", err
		}
		contents, err := os.ReadFile(part)
		if err != nil {
			temporary.Close()
			return "", err
		}
		if _, err := fmt.Fprintf(temporary, "%s\n\n", strings.TrimRight(string(contents), "\r\n")); err != nil {
			temporary.Close()
			return "", err
		}
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	destination := filepath.Join(transcriptDir, date+".txt")
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyStream(destination io.Writer, source io.Reader) error {
	_, err := io.Copy(destination, source)
	return err
}
