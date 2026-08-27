package transcribe

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
			if repeated >= 6 {
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
		problem, err := transcriptQualityProblem(part)
		if err != nil {
			return "", err
		}
		if problem != "" {
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
