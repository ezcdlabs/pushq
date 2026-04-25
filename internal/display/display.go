// Package display renders push queue progress to a terminal inline (no alt-screen).
package display

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

var (
	colorCyan  = lipgloss.Color("6")
	colorGreen = lipgloss.Color("2")
	colorGray  = lipgloss.Color("8")
	colorWhite = lipgloss.Color("15")
)

var spinnerFrames = []string{"⠴", "⠦", "⠧", "⠇", "⠏", "⠋", "⠙", "⠹", "⠸", "⠼"}

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
	var landed *pushq.EntryRecord
	var finalErr error
	spinnerIdx := 0
	joined := false

	inPlace := !verbose && isTerminal(out)
	printer := &snapshotPrinter{out: out, inPlace: inPlace}

	printSnapshot := func() {
		now := time.Now()
		if !joined {
			printer.print("\n" + renderJoining(spinnerIdx) + "\n")
		} else {
			printer.print("\n" + renderJoined() + "\n\nQueue\n" + RenderQueueState(entries, username, landed, spinnerIdx, now))
		}
	}

	var tickCh <-chan time.Time
	if inPlace {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		tickCh = ticker.C
	}

	eventCh := session.Start()
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				return finalErr
			}
			switch e := ev.(type) {
			case pushq.PhaseChanged:
				printSnapshot()
			case pushq.QueueStateChanged:
				joined = true
				entries = e.Entries
				landed = e.Landed
				printSnapshot()
			case pushq.LogLine:
				if verbose {
					fmt.Fprintln(out, e.Text)
				}
			case pushq.Done:
				finalErr = e.Err
			}
		case <-tickCh:
			spinnerIdx++
			printSnapshot()
		}
	}
}

func renderJoining(spinnerIdx int) string {
	spinner := lipgloss.NewStyle().Foreground(colorCyan).Render(spinnerFrames[spinnerIdx%len(spinnerFrames)])
	return spinner + " joining queue"
}

func renderJoined() string {
	check := lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
	return check + " joined"
}

// RenderQueueState returns a formatted string of queue entries. Entries are
// displayed in reverse queue order (last to land at top, first to land at
// bottom) so the display reads like git log — newest above, oldest below.
// Entries belonging to username are marked with ">". If landed is non-nil it
// is rendered as the bottom row with a fixed elapsed duration.
func RenderQueueState(entries []pushq.EntryRecord, username string, landed *pushq.EntryRecord, spinnerIdx int, now time.Time) string {
	var sb strings.Builder

	ownPrefix := username + "-"
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		isOwn := strings.HasPrefix(e.ID, ownPrefix)
		sb.WriteString(renderEntryLine(e, isOwn, EntryIcon(e.Status, spinnerIdx), now))
	}

	if landed != nil {
		check := lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
		sb.WriteString(renderEntryLine(*landed, false, check, now))
	}

	return sb.String()
}

func renderEntryLine(e pushq.EntryRecord, isOwn bool, icon string, now time.Time) string {
	marker := "  "
	if isOwn {
		marker = lipgloss.NewStyle().Foreground(colorCyan).Render("> ")
	}

	var authorStyle, msgStyle lipgloss.Style
	if isOwn {
		authorStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
		msgStyle = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	} else {
		authorStyle = lipgloss.NewStyle().Foreground(colorGray)
		msgStyle = lipgloss.NewStyle().Foreground(colorGray)
	}

	elapsed := entryElapsed(e, now)
	elapsedStr := ""
	if elapsed != "" {
		elapsedStr = "  " + lipgloss.NewStyle().Foreground(colorGray).Render(elapsed)
	}

	return fmt.Sprintf("%s%s  %s  %s%s\n",
		marker, icon,
		authorStyle.Render(e.Author),
		msgStyle.Render(e.Message),
		elapsedStr)
}

func entryElapsed(e pushq.EntryRecord, now time.Time) string {
	if e.JoinedAt.IsZero() {
		return ""
	}
	if !e.LandedAt.IsZero() {
		return formatElapsed(e.LandedAt.Sub(e.JoinedAt))
	}
	return formatElapsed(now.Sub(e.JoinedAt))
}

// EntryIcon returns the status icon for a queue entry, using spinnerIdx to
// select the current animation frame for active entries.
func EntryIcon(status string, spinnerIdx int) string {
	switch status {
	case "testing":
		return lipgloss.NewStyle().Foreground(colorCyan).Render(spinnerFrames[spinnerIdx%len(spinnerFrames)])
	case "done":
		return lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("·")
	}
}
