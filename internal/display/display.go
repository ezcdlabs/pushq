// Package display renders push queue progress to a terminal inline (no alt-screen).
package display

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

var (
	colorCyan  = lipgloss.Color("6")
	colorGreen = lipgloss.Color("2")
	colorRed   = lipgloss.Color("1")
	colorGray  = lipgloss.Color("8")
	colorWhite = lipgloss.Color("15")
)

var spinnerFrames = []string{"⠴", "⠦", "⠧", "⠇", "⠏", "⠋", "⠙", "⠹", "⠸", "⠼"}

// SquashCommit is a pending commit shown in the SQUASH section.
type SquashCommit struct {
	Hash    string
	Subject string
}

// PrintSquash writes the SQUASH section to w: header, commit list, and message
// prompt. The caller reads the user's reply immediately after this returns.
func PrintSquash(w io.Writer, commits []SquashCommit, defaultMsg string) {
	fmt.Fprint(w, "\n"+RenderSectionHeader("SQUASH"))
	for _, c := range commits {
		fmt.Fprintf(w, "  %s  %s\n", c.Hash, c.Subject)
	}
	fmt.Fprintf(w, "\n  Commit message [%s]: ", defaultMsg)
}

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

// RunInline processes push events and writes progress to out. Renders a QUEUE
// section (in-place when terminal) and a RESULT section on completion. Test
// output is suppressed unless verbose is true. nowFn provides the current time
// for elapsed calculations; when nil it defaults to time.Now.
func RunInline(session PushSession, out io.Writer, username string, verbose bool, nowFn func() time.Time) error {
	if nowFn == nil {
		nowFn = time.Now
	}
	var entries []pushq.EntryRecord
	var landed *pushq.EntryRecord
	var finalErr error
	var done bool
	spinnerIdx := 0
	joined := false

	inPlace := !verbose && isTerminal(out)
	printer := &snapshotPrinter{out: out, inPlace: inPlace}

	termWidth := func() int {
		if !inPlace {
			return 0
		}
		w, _, err := term.GetSize(os.Stdout.Fd())
		if err != nil || w <= 0 {
			return 0
		}
		return w
	}

	printSnapshot := func() {
		now := nowFn()
		var sb strings.Builder
		sb.WriteString("\n")
		sb.WriteString(RenderSectionHeader("QUEUE"))
		if !joined {
			sb.WriteString(renderJoiningLine(spinnerIdx))
		} else {
			sb.WriteString(RenderQueueState(entries, username, landed, spinnerIdx, now, termWidth()))
		}
		if done {
			sb.WriteString("\n")
			sb.WriteString(RenderSectionHeader("RESULT"))
			if finalErr != nil {
				sb.WriteString(renderResultLine("✗", errorSummary(finalErr), colorRed))
			} else {
				sb.WriteString(renderResultLine("✔", "landed", colorGreen))
			}
		}
		printer.print(sb.String())
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
			case pushq.Note:
				fmt.Fprintln(out, e.Text)
			case pushq.Done:
				done = true
				finalErr = e.Err
				printSnapshot()
			}
		case <-tickCh:
			spinnerIdx++
			printSnapshot()
		}
	}
}

// RenderSectionHeader returns a styled bold section label for use in the
// inline display. Exported so callers outside the display package can render
// consistent headers for sections they own (e.g. SQUASH in the CLI).
func RenderSectionHeader(label string) string {
	return lipgloss.NewStyle().Bold(true).Render(label) + "\n"
}

func renderJoiningLine(spinnerIdx int) string {
	spinner := lipgloss.NewStyle().Foreground(colorCyan).Render(spinnerFrames[spinnerIdx%len(spinnerFrames)])
	return "  " + spinner + " joining...\n"
}

func renderResultLine(icon, text string, color lipgloss.Color) string {
	return "  " + lipgloss.NewStyle().Foreground(color).Render(icon) + "  " + text + "\n"
}

func errorSummary(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "\n"); i > 0 {
		return msg[:i]
	}
	return msg
}

// RenderQueueState returns a formatted string of queue entries. Entries are
// displayed in reverse queue order (last to land at top, first to land at
// bottom) so the display reads like git log — newest above, oldest below.
// Entries belonging to username are marked with ">". If landed is non-nil it
// is rendered as the bottom row with a fixed elapsed duration. When width > 0,
// elapsed timers are right-aligned to that column.
func RenderQueueState(entries []pushq.EntryRecord, username string, landed *pushq.EntryRecord, spinnerIdx int, now time.Time, width int) string {
	var sb strings.Builder

	ownPrefix := username + "-"
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		isOwn := strings.HasPrefix(e.ID, ownPrefix)
		sb.WriteString(renderEntryLine(e, isOwn, EntryIcon(e.Status, spinnerIdx), now, width))
	}

	if landed != nil {
		check := lipgloss.NewStyle().Foreground(colorGreen).Render("✔")
		sb.WriteString(renderEntryLine(*landed, false, check, now, width))
	}

	return sb.String()
}

func renderEntryLine(e pushq.EntryRecord, isOwn bool, icon string, now time.Time, width int) string {
	ownEjected := isOwn && e.Status == "ejected"
	markerColor := colorCyan
	if ownEjected {
		markerColor = colorRed
	}

	marker := "  "
	if isOwn {
		marker = lipgloss.NewStyle().Foreground(markerColor).Render("> ")
	}

	var authorStyle, msgStyle lipgloss.Style
	if ownEjected {
		authorStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
		msgStyle = lipgloss.NewStyle().Foreground(colorRed)
	} else if isOwn {
		authorStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
		msgStyle = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	} else {
		authorStyle = lipgloss.NewStyle().Foreground(colorGray)
		msgStyle = lipgloss.NewStyle().Foreground(colorGray)
	}

	elapsed := entryElapsed(e, now)
	elapsedRendered := ""
	if elapsed != "" {
		elapsedRendered = lipgloss.NewStyle().Foreground(colorGray).Render(elapsed)
	}

	left := fmt.Sprintf("%s%s  %s  %s",
		marker, icon,
		authorStyle.Render(e.Author),
		msgStyle.Render(e.Message))

	if elapsedRendered == "" {
		if width > 0 {
			pad := width - lipgloss.Width(left)
			if pad > 0 {
				left += strings.Repeat(" ", pad)
			}
		}
		return left + "\n"
	}

	if width > 0 {
		pad := width - lipgloss.Width(left) - lipgloss.Width(elapsedRendered)
		if pad < 2 {
			pad = 2
		}
		return left + strings.Repeat(" ", pad) + elapsedRendered + "\n"
	}

	return left + "  " + elapsedRendered + "\n"
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
	case "ejected":
		return lipgloss.NewStyle().Foreground(colorRed).Render("✗")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("·")
	}
}
