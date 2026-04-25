package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

var (
	inlineCyan  = lipgloss.Color("6")
	inlineGreen = lipgloss.Color("2")
	inlineRed   = lipgloss.Color("1")
	inlineGray  = lipgloss.Color("8")
)

// renderQueueState returns a formatted multi-line string showing the current
// phase and queue entries. Own entries (matching username prefix) are marked.
func renderQueueState(entries []pushq.EntryRecord, username string, phase pushq.Phase) string {
	var sb strings.Builder

	phaseStyle := lipgloss.NewStyle().Foreground(inlineCyan).Bold(true)
	sb.WriteString(phaseStyle.Render(string(phase)))
	sb.WriteString("\n")

	ownPrefix := username + "-"
	for _, e := range entries {
		isOwn := strings.HasPrefix(e.ID, ownPrefix)

		marker := "  "
		if isOwn {
			marker = lipgloss.NewStyle().Foreground(inlineCyan).Render("> ")
		}

		icon := inlineEntryIcon(e.Status)

		idStyle := lipgloss.NewStyle()
		if isOwn {
			idStyle = idStyle.Bold(true)
		} else {
			idStyle = idStyle.Foreground(inlineGray)
		}

		sb.WriteString(fmt.Sprintf("%s%s  %s\n", marker, icon, idStyle.Render(e.ID)))
	}

	return sb.String()
}

func inlineEntryIcon(status string) string {
	switch status {
	case "testing":
		return lipgloss.NewStyle().Foreground(inlineCyan).Render("⠴")
	case "done":
		return lipgloss.NewStyle().Foreground(inlineGreen).Render("✔")
	default: // waiting
		return lipgloss.NewStyle().Foreground(inlineGray).Render("·")
	}
}

// runInline processes push events and writes progress to out. Test command
// output is suppressed unless verbose is true.
func runInline(session PushSession, out io.Writer, username string, verbose bool) error {
	var phase pushq.Phase
	var entries []pushq.EntryRecord
	var finalErr error

	printSnapshot := func() {
		if len(entries) == 0 {
			return
		}
		fmt.Fprintln(out)
		fmt.Fprint(out, renderQueueState(entries, username, phase))
	}

	for ev := range session.Start() {
		switch e := ev.(type) {
		case pushq.PhaseChanged:
			phase = e.Phase
			printSnapshot()
		case pushq.QueueStateChanged:
			entries = e.Entries
			printSnapshot()
		case pushq.LogLine:
			if verbose {
				fmt.Fprintln(out, e.Text)
			}
		case pushq.Done:
			finalErr = e.Err
		}
	}

	return finalErr
}
