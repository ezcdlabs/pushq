package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- colours -----------------------------------------------------------------

var (
	colCyan   = lipgloss.Color("51")
	colGreen  = lipgloss.Color("78")
	colRed    = lipgloss.Color("203")
	colDim    = lipgloss.Color("240")
	colMuted  = lipgloss.Color("245")
	colBright = lipgloss.Color("255")
	colDark   = lipgloss.Color("232")
)

// --- data model --------------------------------------------------------------

type status int

const (
	statusWaiting status = iota
	statusTesting
	statusPassed // tests passed, waiting for entries above to land
	statusLanding
	statusLanded
	statusFailed
	statusConflict
	statusCancelled
)

type entry struct {
	name   string
	status status
	isYou  bool
}

type screen struct {
	phase      string   // shown next to the pushq badge
	entries    []entry  // left panel
	panelLines []string // right panel content
}

var screens = []screen{
	// D1 — commit review
	{
		phase:  "review commits",
		entries: []entry{
			{name: "a1b2c3d  add user auth endpoint", status: statusWaiting},
			{name: "e4f5a6b  fix token expiry bug", status: statusWaiting},
		},
		panelLines: []string{
			"  Squash commit message",
			"",
			"  > add user authentication with token expiry fix_",
			"",
			"",
			"  (edit above, then press enter to push)",
		},
	},
	// D2 — running tests (alice already landed and vanished)
	{
		phase: "running tests  (1 / 3)",
		entries: []entry{
			{name: "you/add-auth", status: statusTesting, isYou: true},
			{name: "bob/fix-navbar", status: statusWaiting},
			{name: "carol/update-deps", status: statusWaiting},
		},
		panelLines: []string{
			"  > go test ./...",
			"",
			"  ok   github.com/acme/app/api    1.204s",
			"  ok   github.com/acme/app/auth   0.812s",
			"  ---  github.com/acme/app/db     [running]",
		},
	},
	// D2b — retesting after entry above was ejected (bob vanished)
	{
		phase: "retesting  (1 / 2)",
		entries: []entry{
			{name: "you/add-auth", status: statusTesting, isYou: true},
			{name: "carol/update-deps", status: statusWaiting},
		},
		panelLines: []string{
			"  bob/fix-navbar was removed from the queue.",
			"  Previous test run invalidated — retesting on new base.",
			"",
			"  > go test ./...",
			"",
			"  ok   github.com/acme/app/api    1.204s",
			"  ---  github.com/acme/app/auth   [running]",
		},
	},
	// D3 — tests passed, waiting for entry ahead
	{
		phase: "waiting for bob/fix-navbar  (2 / 3)",
		entries: []entry{
			{name: "bob/fix-navbar", status: statusTesting},
			{name: "you/add-auth", status: statusPassed, isYou: true},
			{name: "carol/update-deps", status: statusWaiting},
		},
		panelLines: []string{
			"  > go test ./...",
			"",
			"  ok   github.com/acme/app/api    1.204s",
			"  ok   github.com/acme/app/auth   0.812s",
			"  ok   github.com/acme/app/db     2.001s",
			"",
			"  All tests passed.",
			"  Waiting for bob/fix-navbar...",
		},
	},
	// D4 — landing (bob landed and vanished, you're now first)
	{
		phase: "landing  (1 / 2)",
		entries: []entry{
			{name: "you/add-auth", status: statusLanding, isYou: true},
			{name: "carol/update-deps", status: statusWaiting},
		},
		panelLines: []string{
			"  > go test ./...",
			"",
			"  ok   github.com/acme/app/api    1.204s",
			"  ok   github.com/acme/app/auth   0.812s",
			"  ok   github.com/acme/app/db     2.001s",
			"",
			"  All tests passed.",
			"",
			"  Pushing to main...",
		},
	},
	// D5 — success (only your entry remains, showing final state before TUI exits)
	{
		phase: "landed",
		entries: []entry{
			{name: "you/add-auth", status: statusLanded, isYou: true},
			{name: "carol/update-deps", status: statusWaiting},
		},
		panelLines: []string{
			"  Landed on main.",
			"",
			"  a1b2c3d  add user authentication with token expiry fix",
		},
	},
	// D6 — test failure
	{
		phase:  "tests failed",
		entries: []entry{
			{name: "you/add-auth", status: statusFailed, isYou: true},
			{name: "bob/fix-navbar", status: statusWaiting},
		},
		panelLines: []string{
			"  > go test ./...",
			"",
			"  ok   github.com/acme/app/api    1.204s",
			"  FAIL github.com/acme/app/auth   0.912s",
			"",
			"    auth_test.go:42: expected token TTL 3600, got 0",
			"",
			"  Ejected from queue.",
		},
	},
	// D7 — conflict
	{
		phase:  "conflict",
		entries: []entry{
			{name: "you/add-auth", status: statusConflict, isYou: true},
			{name: "bob/fix-navbar", status: statusTesting},
		},
		panelLines: []string{
			"  Conflict with bob/fix-navbar.",
			"",
			"  Conflicting file:",
			"    internal/auth/token.go",
			"",
			"  Ejected from queue.",
		},
	},
	// D8 — cancelled
	{
		phase: "cancelled",
		entries: []entry{
			{name: "you/add-auth", status: statusCancelled, isYou: true},
			{name: "bob/fix-navbar", status: statusWaiting},
		},
		panelLines: []string{
			"  Cancelled.",
			"",
			"  Your uncommitted changes have been restored.",
		},
	},
}

// --- left panel --------------------------------------------------------------

func entryIcon(e entry) (icon string, col lipgloss.Color) {
	switch e.status {
	case statusLanded:
		return "✔", colGreen
	case statusPassed:
		return "✔", colCyan
	case statusTesting:
		return "⠴", colCyan
	case statusLanding:
		return "↑", colCyan
	case statusFailed, statusConflict:
		return "✗", colRed
	case statusCancelled:
		return "✗", colDim
	default: // waiting
		return "·", colDim
	}
}

func renderLeftPanel(s screen, w, h int, accent lipgloss.Color) string {
	// queue entries only — logo moved to header bar
	activeMarker := lipgloss.NewStyle().Foreground(accent).Render(">")

	var entryRows []string
	for _, e := range s.entries {
		icon, col := entryIcon(e)
		iconStr := lipgloss.NewStyle().Foreground(col).Render(icon)

		var nameStr string
		switch {
		case e.isYou:
			nameStr = lipgloss.NewStyle().Foreground(colBright).Bold(true).Render(e.name)
		case e.status == statusLanded:
			nameStr = lipgloss.NewStyle().Foreground(colMuted).Render(e.name)
		default:
			nameStr = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(e.name)
		}

		marker := " "
		if e.isYou {
			marker = activeMarker
		}
		entryRows = append(entryRows, fmt.Sprintf("%s  %s  %s", marker, iconStr, nameStr))
	}

	content := strings.Join(entryRows, "\n")

	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		PaddingTop(1).
		PaddingLeft(1).
		Render(content)
}

// --- right panel -------------------------------------------------------------

func renderRightPanel(lines []string, w, h int, accent lipgloss.Color) string {
	borderSt := lipgloss.NewStyle().Foreground(accent)
	textSt := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	innerW := w - 2
	innerH := h - 2

	top := borderSt.Render("┏" + strings.Repeat("━", innerW) + "┓")
	bot := borderSt.Render("┗" + strings.Repeat("━", innerW) + "┛")

	rows := make([]string, innerH)
	for i := range rows {
		var text string
		if i < len(lines) {
			text = lines[i]
		}
		runes := []rune(text)
		if len(runes) > innerW {
			runes = runes[:innerW]
		}
		padded := string(runes) + strings.Repeat(" ", innerW-len(runes))
		rows[i] = borderSt.Render("┃") + textSt.Render(padded) + borderSt.Render("┃")
	}

	all := []string{top}
	all = append(all, rows...)
	all = append(all, bot)
	return strings.Join(all, "\n")
}

// --- Bubble Tea model --------------------------------------------------------

type model struct {
	index  int
	width  int
	height int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right", "l":
			m.index = (m.index + 1) % len(screens)
		case "left", "h":
			m.index = (m.index - 1 + len(screens)) % len(screens)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	s := screens[m.index]
	w := m.width

	// derive accent colour from your entry's state
	accent := colCyan
	for _, e := range s.entries {
		if e.isYou {
			switch e.status {
			case statusLanded:
				accent = colGreen
			case statusFailed, statusConflict, statusCancelled:
				accent = colRed
			}
		}
	}

	// header bar
	headerBg := lipgloss.Color("235")
	badge := lipgloss.NewStyle().
		Background(accent).
		Foreground(colDark).
		Bold(true).
		Padding(0, 2).
		Render("PQ")
	phaseText := lipgloss.NewStyle().
		Background(headerBg).
		Foreground(accent).
		Padding(0, 1).
		Render(s.phase)
	headerFill := lipgloss.NewStyle().
		Background(headerBg).
		Width(w - lipgloss.Width(badge) - lipgloss.Width(phaseText)).
		Render("")
	header := lipgloss.JoinHorizontal(lipgloss.Top, badge, phaseText, headerFill)

	panelH := m.height - 1 // header only
	if panelH < 4 {
		panelH = 4
	}

	leftW := 42
	rightW := w - leftW

	left := renderLeftPanel(s, leftW, panelH, accent)
	right := renderRightPanel(s.panelLines, rightW, panelH, accent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func padBetween(left, right string, w int) string {
	gap := w - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func main() {
	p := tea.NewProgram(model{}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
