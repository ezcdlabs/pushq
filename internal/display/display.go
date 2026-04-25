// Package display renders push queue progress to a terminal inline (no alt-screen).
package display

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

var (
	colorCyan  = lipgloss.Color("6")
	colorGreen = lipgloss.Color("2")
	colorGray  = lipgloss.Color("8")
)

// PushSession is the contract between the display layer and the push library.
// Start returns a channel of events that ends with a Done event. Cancel
// triggers a graceful self-ejection.
type PushSession interface {
	Start() <-chan pushq.Event
	Cancel()
}

// snapshotPrinter writes queue snapshots to out. In in-place mode it uses ANSI
// cursor movement to overwrite the previous snapshot rather than appending.
type snapshotPrinter struct {
	out       io.Writer
	inPlace   bool
	lastLines int
}

func (p *snapshotPrinter) print(snapshot string) {
	lines := strings.Split(snapshot, "\n")
	// strings.Split on "a\nb\n" gives ["a","b",""] — drop trailing empty
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	nNew := len(lines)

	if p.inPlace {
		if p.lastLines > 0 {
			fmt.Fprintf(p.out, "\033[%dA\r", p.lastLines)
		}
		for _, line := range lines {
			fmt.Fprintf(p.out, "%s\033[K\n", line)
		}
		// Clear lines left over from a longer previous snapshot
		extra := p.lastLines - nNew
		for i := 0; i < extra; i++ {
			fmt.Fprintf(p.out, "\033[K\n")
		}
		// Move cursor back above the cleared area so the next update overwrites it
		if extra > 0 {
			fmt.Fprintf(p.out, "\033[%dA", extra)
		}
		p.lastLines = nNew
	} else {
		fmt.Fprint(p.out, snapshot)
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// RunInline processes push events and writes progress to out. While joining,
// a single status line is shown. Once queue state is received, a "joined" +
// Queue block is rendered and updated in-place on each change. Test output is
// suppressed unless verbose is true.
func RunInline(session PushSession, out io.Writer, username string, verbose bool) error {
	var entries []pushq.EntryRecord
	var finalErr error

	printer := &snapshotPrinter{out: out, inPlace: !verbose && isTerminal(out)}

	printSnapshot := func() {
		if len(entries) == 0 {
			printer.print("\n" + renderJoining() + "\n")
		} else {
			printer.print("\n" + renderJoined() + "\n\nQueue\n" + RenderQueueState(entries, username))
		}
	}

	for ev := range session.Start() {
		switch e := ev.(type) {
		case pushq.PhaseChanged:
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

func renderJoining() string {
	spinner := lipgloss.NewStyle().Foreground(colorCyan).Render("⠴")
	return spinner + " joining queue"
}

func renderJoined() string {
	check := lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
	return check + " joined"
}

// RenderQueueState returns a formatted string of queue entries. Entries
// belonging to username are marked with ">".
func RenderQueueState(entries []pushq.EntryRecord, username string) string {
	var sb strings.Builder

	ownPrefix := username + "-"
	for _, e := range entries {
		isOwn := strings.HasPrefix(e.ID, ownPrefix)

		marker := "  "
		if isOwn {
			marker = lipgloss.NewStyle().Foreground(colorCyan).Render("> ")
		}

		icon := EntryIcon(e.Status)

		idStyle := lipgloss.NewStyle()
		if isOwn {
			idStyle = idStyle.Bold(true)
		} else {
			idStyle = idStyle.Foreground(colorGray)
		}

		sb.WriteString(fmt.Sprintf("%s%s  %s\n", marker, icon, idStyle.Render(e.ID)))
	}

	return sb.String()
}

// EntryIcon returns the status icon for a queue entry.
func EntryIcon(status string) string {
	switch status {
	case "testing":
		return lipgloss.NewStyle().Foreground(colorCyan).Render("⠴")
	case "done":
		return lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("·")
	}
}
