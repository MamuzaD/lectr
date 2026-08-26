package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/mamuzad/transcribe-go/internal/demo"
)

func main() {
	if _, err := tea.NewProgram(demo.New()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "transcribe-demo: %v\n", err)
		os.Exit(1)
	}
}
