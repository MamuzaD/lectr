package transcribe

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const repetitionLoopCopies = 7

func transcriptQualityProblem(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	hasContent := false
	repeated := 0
	previous := ""
	for scanner.Scan() {
		line := strings.Join(strings.Fields(scanner.Text()), " ")
		if line != "" {
			hasContent = true
		}
		if len(line) > 3 && line == previous {
			repeated++
			if repeated >= repetitionLoopCopies-1 {
				return "repetition loop", nil
			}
		} else {
			repeated = 0
		}
		previous = line
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !hasContent {
		return "empty transcript", nil
	}
	return "", nil
}

func transcriptPassesQualityCheck(path string) (bool, error) {
	problem, err := transcriptQualityProblem(path)
	return problem == "", err
}

// repetitionLoopSample finds the longest run of consecutive, identical
// lines in the transcript, so a caller can show what actually got stuck in
// a loop instead of just naming the problem.
func repetitionLoopSample(path string) (line string, count int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	previous, runLine, runCount, bestLine, bestCount := "", "", 0, "", 0
	for scanner.Scan() {
		normalized := strings.Join(strings.Fields(scanner.Text()), " ")
		if len(normalized) > 3 && normalized == previous {
			runCount++
		} else {
			runLine, runCount = normalized, 1
		}
		if runCount > bestCount {
			bestLine, bestCount = runLine, runCount
		}
		previous = normalized
	}
	if err := scanner.Err(); err != nil {
		return "", 0, err
	}
	return bestLine, bestCount, nil
}

// trimRepetitionLoop collapses only runs long enough to trip the quality
// check. Short repetitions can be legitimate lecture content and are kept.
func trimRepetitionLoop(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var kept, run []string
	runNormalized := ""
	flush := func() {
		if len(run) >= repetitionLoopCopies && len(runNormalized) > 3 {
			kept = append(kept, run[0])
		} else {
			kept = append(kept, run...)
		}
		run = run[:0]
	}
	for scanner.Scan() {
		raw := scanner.Text()
		normalized := strings.Join(strings.Fields(raw), " ")
		if len(run) > 0 && normalized != runNormalized {
			flush()
		}
		if len(run) == 0 {
			runNormalized = normalized
		}
		run = append(run, raw)
	}
	flush()
	scanErr := scanner.Err()
	if closeErr := file.Close(); closeErr != nil && scanErr == nil {
		scanErr = closeErr
	}
	if scanErr != nil {
		return scanErr
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o644)
}

func combineParts(course, date, transcriptDir string) (string, error) {
	return combinePartsForGroup(course, date, transcriptDir, nil)
}

func combinePartsForGroup(course, date, transcriptDir string, memoStems map[string]bool) (string, error) {
	parts, err := filepath.Glob(filepath.Join(transcriptDir, date+"-pt[0-9][0-9].txt"))
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%s: no transcripts available to combine for %s", course, date)
	}
	sort.Strings(parts)
	for _, part := range parts {
		problem, err := transcriptQualityProblem(part)
		if err != nil {
			return "", err
		}
		if problem != "" {
			stem := strings.TrimSuffix(filepath.Base(part), filepath.Ext(part))
			if memoStems != nil && !memoStems[stem] {
				return "", fmt.Errorf("%s: %s detected in orphaned %s; move or remove that transcript before retrying", course, problem, filepath.Base(part))
			}
			return "", fmt.Errorf("%s: %s detected in %s; rerun with --force", course, problem, filepath.Base(part))
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
