package transcribe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

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
	options.Interactive = terminal
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
	var groupErrors []error
	for groupIndex := range values {
		forward := func(message event) bool {
			message.Group = groupIndex
			return emit(message)
		}
		if err := processGroup(ctx, &values[groupIndex], options, forward); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			groupErrors = append(groupErrors, flattenErrors(err)...)
		}
	}
	if !emit(event{AllComplete: true}) {
		return ctx.Err()
	}
	if len(groupErrors) > 0 {
		return &BatchError{Failures: groupErrors}
	}
	return nil
}

// BatchError reports recordings that failed while the rest of the batch still
// ran. Each failure was already shown in the queue as it happened.
type BatchError struct{ Failures []error }

func (e *BatchError) Error() string   { return errors.Join(e.Failures...).Error() }
func (e *BatchError) Unwrap() []error { return e.Failures }

func flattenErrors(err error) []error {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}
	var flat []error
	for _, inner := range joined.Unwrap() {
		flat = append(flat, flattenErrors(inner)...)
	}
	return flat
}

func normalizeResult(err error) error {
	if errors.Is(err, context.Canceled) {
		return ErrCancelled
	}
	return err
}

func isTerminal(file *os.File) bool { return term.IsTerminal(file.Fd()) }

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
	var memoErrors []error
	for index := range value.Memos {
		if value.Memos[index].Status == failed {
			emit(event{Index: index, Status: failed, Detail: value.Memos[index].Detail, HasMemoUpdate: true})
			memoErrors = append(memoErrors, memoFailure(value, index))
			continue
		}
		if value.Memos[index].Status != waiting {
			continue
		}
		if err := transcribeMemo(ctx, value, index, options, emit); err != nil {
			if ctx.Err() != nil {
				return err
			}
			value.Memos[index].Status = failed
			value.Memos[index].Detail = err.Error()
			emit(event{Index: index, Status: failed, Detail: err.Error(), HasMemoUpdate: true})
		}
		if value.Memos[index].Status == failed {
			memoErrors = append(memoErrors, memoFailure(value, index))
		}
	}
	if len(memoErrors) > 0 {
		return errors.Join(memoErrors...)
	}
	if value.SkipCombine {
		return nil
	}
	if !emit(event{Combine: active, HasCombine: true}) {
		return ctx.Err()
	}
	stems := make(map[string]bool, len(value.Memos))
	for _, memo := range value.Memos {
		stems[memo.Stem] = true
	}
	combined, err := combinePartsForGroup(value.Course, value.Date, value.TranscriptDir, stems)
	if err != nil {
		emit(event{Combine: failed, Detail: err.Error(), HasCombine: true})
		return err
	}
	emit(event{Combine: complete, CombinedPath: combined, HasCombine: true})
	return nil
}

func memoFailure(value *group, index int) error {
	memo := value.Memos[index]
	detail := memo.Detail
	if detail == "" {
		detail = "transcription failed"
	}
	name := filepath.Base(memo.Path)
	if memo.Path == "" {
		name = memo.Stem + ".txt"
	}
	// A date-level retry recombines the date afterwards; a memo-level one
	// would skip that. --force is only needed to replace a bad transcript
	// that is still in place (a rejected one was already moved aside).
	retry := "lectr transcribe " + value.Course + " " + value.Date
	if fileExists(filepath.Join(value.TranscriptDir, memo.Stem+".txt")) {
		return fmt.Errorf("%s: %s: %s; replace it with: %s --force", value.Course, name, detail, retry)
	}
	return fmt.Errorf("%s: %s: %s; retry with: %s", value.Course, name, detail, retry)
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
		return nil
	}
	problem, err := transcriptQualityProblem(temporaryTranscript)
	if err != nil {
		return err
	}
	if problem == "repetition loop" && options.Interactive {
		accepted, err := confirmTrimmedTranscript(ctx, value, index, temporaryTranscript, emit)
		if err != nil {
			return err
		}
		if accepted {
			problem = ""
		}
	}
	if problem != "" {
		rejected := filepath.Join(value.TranscriptDir, memo.Stem+".rejected.txt")
		if renameErr := os.Rename(temporaryTranscript, rejected); renameErr != nil {
			return renameErr
		}
		memo.Status, memo.Detail = failed, "Rejected "+problem+"; output saved as .rejected.txt"
		emit(event{Index: index, Status: failed, Detail: memo.Detail, HasMemoUpdate: true})
		return nil
	}
	if err := os.Rename(temporaryTranscript, transcript); err != nil {
		return err
	}
	memo.Status, memo.Percent, memo.Detail = complete, 100, ""
	emit(event{Index: index, Status: complete, Percent: 100, HasMemoUpdate: true})
	return nil
}

// truncateForDisplay quotes text for a confirm prompt, shortening it (by
// rune, not byte, to avoid splitting multi-byte characters) if it's long.
func truncateForDisplay(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return fmt.Sprintf("%q", text)
	}
	return fmt.Sprintf("%q…", string(runes[:limit]))
}

// confirmTrimmedTranscript asks whether to trim a detected repetition loop
// out of path and use the transcript anyway, blocking until the interactive
// UI answers or ctx is cancelled. It reports true only if trimming actually
// cleared the quality problem.
func confirmTrimmedTranscript(ctx context.Context, value *group, index int, path string, emit func(event) bool) (bool, error) {
	memo := &value.Memos[index]
	response := make(chan bool, 1)
	prompt := "Repetition loop detected. Trim it and use this transcript?"
	if sample, count, err := repetitionLoopSample(path); err == nil && sample != "" {
		prompt = fmt.Sprintf("Repeated %dx: %s — trim it and use this transcript?", count, truncateForDisplay(sample, 60))
	}
	memo.Status, memo.Detail = confirming, prompt
	if !emit(event{
		Index: index, Status: confirming, Detail: prompt, HasMemoUpdate: true,
		HasConfirm: true, ConfirmPrompt: prompt, ConfirmResponse: response,
	}) {
		return false, ctx.Err()
	}
	select {
	case accept := <-response:
		if !accept {
			return false, nil
		}
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if err := trimRepetitionLoop(path); err != nil {
		return false, err
	}
	problem, err := transcriptQualityProblem(path)
	if err != nil {
		return false, err
	}
	return problem == "", nil
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
