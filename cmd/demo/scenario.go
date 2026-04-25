package main

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// fixed join times for the demo — stable across runs.
var (
	demoBase  = time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	idYou   = pushq.EntryID("you", demoBase, "add auth endpoint")
	idBob   = pushq.EntryID("bob", demoBase.Add(-90*time.Second), "fix navbar")
	idCarol = pushq.EntryID("carol", demoBase.Add(-3*time.Minute), "update deps")
)

// shellPrompt renders a Ubuntu-style coloured bash prompt.
func shellPrompt(user, host, path string) string {
	userHost := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(user + "@" + host)
	dir := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(path)
	return userHost + ":" + dir + "$ "
}

// PreludeLine is a line of terminal output printed before the TUI starts.
type PreludeLine struct {
	Text      string
	Delay     time.Duration // pause before printing
	Typing    bool          // print char-by-char (simulates user typing a response)
	NoNewline bool          // suppress trailing newline (for prompt prefixes)
}

// Frame is a single TUI state shown for a fixed duration during autoplay.
type Frame struct {
	Screen screen
	Hold   time.Duration
}

// Scenario is a named sequence used for demo recording and screenshots.
type Scenario struct {
	Name    string
	Prelude []PreludeLine
	Frames  []Frame
}

// happyPath shows the common case: one person in the queue, tests pass, lands on main.
var happyPath = Scenario{
	Name: "happy-path",
	Prelude: []PreludeLine{
		{Text: shellPrompt("you", "workstation", "~/Projects/your-project"), Delay: 400 * time.Millisecond, NoNewline: true},
		{Text: "git pushq", Delay: 300 * time.Millisecond, Typing: true},
		{Text: "", Delay: 200 * time.Millisecond},
		{Text: "Commits to push:", Delay: 200 * time.Millisecond},
		{Text: "  a1b2c3d  add user auth endpoint", Delay: 60 * time.Millisecond},
		{Text: "  e4f5a6b  fix token expiry bug", Delay: 60 * time.Millisecond},
		{Text: "", Delay: 400 * time.Millisecond},
		{Text: "Commit message [fix token expiry bug]: ", Delay: 300 * time.Millisecond, NoNewline: true},
		{Text: "add auth endpoint", Delay: 100 * time.Millisecond, Typing: true},
	},
	Frames: []Frame{
		{
			Screen: screen{
				phase: "joining",
				entries: []entry{
					{name: idYou, status: statusTesting, isYou: true},
					{name: idBob, status: statusWaiting},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  > go test ./...",
					"",
				},
			},
			Hold: 600 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "running tests  (1 / 3)",
				entries: []entry{
					{name: idYou, status: statusTesting, isYou: true},
					{name: idBob, status: statusWaiting},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  > go test ./...",
					"",
					"  ok   github.com/acme/app/api    1.204s",
				},
			},
			Hold: 700 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "running tests  (1 / 3)",
				entries: []entry{
					{name: idYou, status: statusTesting, isYou: true},
					{name: idBob, status: statusWaiting},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  > go test ./...",
					"",
					"  ok   github.com/acme/app/api    1.204s",
					"  ok   github.com/acme/app/auth   0.812s",
					"  ---  github.com/acme/app/db     [running]",
				},
			},
			Hold: 1200 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "running tests  (1 / 3)",
				entries: []entry{
					{name: idYou, status: statusTesting, isYou: true},
					{name: idBob, status: statusWaiting},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  > go test ./...",
					"",
					"  ok   github.com/acme/app/api    1.204s",
					"  ok   github.com/acme/app/auth   0.812s",
					"  ok   github.com/acme/app/db     2.001s",
					"",
					"  All tests passed.",
				},
			},
			Hold: 900 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "waiting for " + idBob + "  (2 / 3)",
				entries: []entry{
					{name: idBob, status: statusTesting},
					{name: idYou, status: statusPassed, isYou: true},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  > go test ./...",
					"",
					"  ok   github.com/acme/app/api    1.204s",
					"  ok   github.com/acme/app/auth   0.812s",
					"  ok   github.com/acme/app/db     2.001s",
					"",
					"  All tests passed.",
					"  Waiting for " + idBob + "...",
				},
			},
			Hold: 2200 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "landing  (1 / 2)",
				entries: []entry{
					{name: idYou, status: statusLanding, isYou: true},
					{name: idCarol, status: statusWaiting},
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
			Hold: 1200 * time.Millisecond,
		},
		{
			Screen: screen{
				phase: "landed",
				entries: []entry{
					{name: idYou, status: statusLanded, isYou: true},
					{name: idCarol, status: statusWaiting},
				},
				panelLines: []string{
					"  Landed on main.",
					"",
					"  a1b2c3d  add auth endpoint",
				},
			},
			Hold: 2000 * time.Millisecond,
		},
	},
}

var allScenarios = map[string]*Scenario{
	"happy-path": &happyPath,
}
