package transcribe

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mamuzad/lectr/internal/ui"
)

type recordingSelection struct {
	group int
	memo  int
}

type selectionModel struct {
	groups      []group
	recordings  []recordingSelection
	selector    ui.RecordingSelector
	result      []group
	done        bool
	exited      bool
	interrupted bool
}

func chooseGroups(ctx context.Context, groups []group, today time.Time) ([]group, error) {
	model := newSelectionModel(groups, today.Format("2006-01-02"))
	returned, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	state := returned.(selectionModel)
	if state.interrupted {
		return nil, ErrCancelled
	}
	if state.exited {
		return nil, nil
	}
	return state.result, nil
}

func newSelectionModel(groups []group, today string) selectionModel {
	model := selectionModel{groups: groups}
	items := make([]ui.SelectionItem, 0)
	for groupIndex, value := range groups {
		for memoIndex, memo := range value.Memos {
			if memo.Status != waiting {
				continue
			}
			model.recordings = append(model.recordings, recordingSelection{group: groupIndex, memo: memoIndex})
			items = append(items, ui.SelectionItem{
				Label: recordingLabel(value, memo), Duration: memo.Duration, Today: value.Date == today,
			})
		}
	}
	model.selector = ui.NewRecordingSelector(items, approximateAudio(groups))
	return model
}

func recordingLabel(value group, memo Memo) string {
	date, err := time.Parse("2006-01-02", value.Date)
	label := value.Date
	if err == nil {
		label = date.Format("Mon, Jan 2")
	}
	if len(value.Memos) > 1 {
		return label + " " + value.Course + " pt" + memo.Part
	}
	return label + "  " + value.Course
}

func (m selectionModel) Init() tea.Cmd { return nil }

func (m selectionModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	selector, action := m.selector.Update(key.String())
	m.selector = selector
	switch action {
	case ui.SelectionConfirmed:
		m.done = true
		selected := make(map[recordingSelection]bool, len(m.recordings))
		for _, index := range selector.SelectedIndices() {
			selected[m.recordings[index]] = true
		}
		m.result = filterGroupsWithFailures(m.groups, func(groupIndex, memoIndex int) bool {
			return selected[recordingSelection{group: groupIndex, memo: memoIndex}]
		}, len(selected) == len(m.recordings))
		return m, tea.Quit
	case ui.SelectionExited:
		m.done = true
		m.exited = true
		return m, tea.Quit
	case ui.SelectionInterrupted:
		m.done = true
		m.interrupted = true
		return m, tea.Quit
	}
	return m, nil
}

func (m selectionModel) View() tea.View {
	return tea.NewView(ui.Shell(m.selector.View()))
}

func filterGroups(groups []group, include func(groupIndex, memoIndex int) bool) []group {
	return filterGroupsWithFailures(groups, include, false)
}

func filterGroupsWithFailures(groups []group, include func(groupIndex, memoIndex int) bool, keepFailures bool) []group {
	result := make([]group, 0, len(groups))
	for groupIndex, value := range groups {
		filtered := valueCopy(value)
		filtered.Memos = filtered.Memos[:0]
		for memoIndex, memo := range value.Memos {
			if memo.Status == skipped || keepFailures && memo.Status == failed || include(groupIndex, memoIndex) {
				filtered.Memos = append(filtered.Memos, memo)
			} else {
				filtered.SkipCombine = true
			}
		}
		if hasPendingMemos(filtered) || hasFailedMemos(filtered) || needsCombine(filtered) {
			result = append(result, filtered)
		}
	}
	return result
}

func approximateAudio(groups []group) string {
	total := 0
	for _, value := range groups {
		for _, memo := range value.Memos {
			if memo.Status != waiting {
				continue
			}
			parts := strings.Split(memo.Duration, ":")
			if len(parts) != 2 && len(parts) != 3 {
				continue
			}
			seconds := 0
			valid := true
			for _, part := range parts {
				value, err := strconv.Atoi(part)
				if err != nil {
					valid = false
					break
				}
				seconds = seconds*60 + value
			}
			if valid {
				total += seconds
			}
		}
	}
	if total == 0 {
		return "audio duration unavailable"
	}
	minutes := (total + 30) / 60
	if minutes < 60 {
		return fmt.Sprintf("about %dm audio", minutes)
	}
	return fmt.Sprintf("about %dh %dm audio", minutes/60, minutes%60)
}
