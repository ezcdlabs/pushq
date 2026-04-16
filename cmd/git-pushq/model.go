package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// channelClosedMsg is sent when the event channel closes without a Done event.
type channelClosedMsg struct{}

// model is the Bubble Tea model for the git pushq TUI.
type model struct {
	session  PushSession
	events   <-chan pushq.Event

	// State derived from events.
	entries  []pushq.EntryRecord
	phase    pushq.Phase
	logLines []string
	done     bool
	err      error

	// Terminal dimensions — set by WindowSizeMsg in normal operation,
	// overridden in tests to a fixed size.
	width  int
	height int
}

func initialModel(session PushSession) model {
	return model{
		session: session,
		events:  session.Start(),
		width:   80,
		height:  24,
	}
}

// --- Bubble Tea interface ----------------------------------------------------

func (m model) Init() tea.Cmd {
	return waitForEvent(m.events)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.session.Cancel()
			return m, tea.Quit
		}

	case pushq.QueueStateChanged:
		m.entries = msg.Entries
		return m, waitForEvent(m.events)

	case pushq.PhaseChanged:
		m.phase = msg.Phase
		return m, waitForEvent(m.events)

	case pushq.LogLine:
		m.logLines = append(m.logLines, msg.Text)
		return m, waitForEvent(m.events)

	case pushq.Done:
		m.done = true
		m.err = msg.Err
		return m, tea.Quit

	case channelClosedMsg:
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m model) View() string {
	leftW := m.width / 3
	rightW := m.width - leftW - 1 // 1 for the gap

	left := m.renderLeft(leftW, m.height)
	right := m.renderRight(rightW, m.height)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

// --- rendering ---------------------------------------------------------------

var (
	cyan  = lipgloss.Color("6")
	green = lipgloss.Color("2")
	red   = lipgloss.Color("1")
	gray  = lipgloss.Color("8")
)

func (m model) accentColor() lipgloss.Color {
	if m.done {
		if m.err != nil {
			return red
		}
		return green
	}
	return cyan
}

func (m model) renderLeft(w, h int) string {
	accent := m.accentColor()

	// Header: PQ badge + phase
	badgeStyle := lipgloss.NewStyle().
		Background(accent).
		Foreground(lipgloss.Color("0")).
		Bold(true).
		Padding(0, 1)
	badge := badgeStyle.Render(" PQ ")

	phaseText := string(m.phase)
	if m.done && m.err == nil {
		phaseText = "landed"
	} else if m.done && m.err != nil {
		phaseText = "failed"
	}
	header := badge + "  " + phaseText

	// Entry list
	var lines []string
	for _, e := range m.entries {
		icon := entryIcon(e.Status)
		line := fmt.Sprintf("  %s %s", icon, e.ID)
		lines = append(lines, line)
	}

	// Error line (shown below entries if present)
	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(red)
		lines = append(lines, "")
		lines = append(lines, errStyle.Render("  error: "+m.err.Error()))
	}

	body := strings.Join(lines, "\n")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Width(w - 2).
		Height(h - 4) // leave room for header

	return header + "\n" + borderStyle.Render(body)
}

func (m model) renderRight(w, h int) string {
	accent := m.accentColor()

	// Show recent log lines, newest at the bottom.
	innerH := h - 4
	lines := m.logLines
	if len(lines) > innerH {
		lines = lines[len(lines)-innerH:]
	}
	body := strings.Join(lines, "\n")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Width(w - 2).
		Height(h - 4)

	return borderStyle.Render(body)
}

func entryIcon(status string) string {
	switch status {
	case "waiting":
		return "·"
	case "testing":
		return "⠴"
	case "done":
		return "✔"
	default:
		return "·"
	}
}

// waitForEvent returns a Cmd that blocks until the next event arrives on ch.
func waitForEvent(ch <-chan pushq.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return channelClosedMsg{}
		}
		return ev
	}
}
