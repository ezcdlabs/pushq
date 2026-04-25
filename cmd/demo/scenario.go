package main

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// shellPrompt renders a Ubuntu-style coloured bash prompt.
func shellPrompt(user, host, path string) string {
	userHost := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(user + "@" + host)
	dir := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(path)
	return userHost + ":" + dir + "$ "
}

// PreludeLine is a line of terminal output printed before the push session starts.
type PreludeLine struct {
	Text      string
	Delay     time.Duration // pause before printing
	Typing    bool          // print char-by-char (simulates user typing a response)
	NoNewline bool          // suppress trailing newline (for prompt prefixes)
}

// Frame is a single event emitted to the display, held for a duration before
// the next event is sent.
type Frame struct {
	Event pushq.Event
	Hold  time.Duration
}

// Scenario is a named sequence used for demo recording and screenshots.
type Scenario struct {
	Name    string
	Prelude []PreludeLine
	Frames  []Frame
}

// fixed join/land times for the demo — stable across runs.
// carol joined earliest (first to land), then bob, then you (last to land).
var (
	demoBase = time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)

	idYou   = pushq.EntryID("you", demoBase, "add auth endpoint")
	idBob   = pushq.EntryID("bob", demoBase.Add(-90*time.Second), "fix navbar")
	idCarol = pushq.EntryID("carol", demoBase.Add(-3*time.Minute), "update deps")

	recYou = pushq.EntryRecord{
		ID: idYou, Author: "you", Message: "add auth endpoint",
		JoinedAt: demoBase,
	}
	recBob = pushq.EntryRecord{
		ID: idBob, Author: "bob", Message: "fix navbar",
		JoinedAt: demoBase.Add(-90 * time.Second),
	}
	recCarol = pushq.EntryRecord{
		ID: idCarol, Author: "carol", Message: "update deps",
		JoinedAt: demoBase.Add(-3 * time.Minute),
	}

	landedBefore = &pushq.EntryRecord{
		Author: "sam", Message: "refactor user model",
		JoinedAt: demoBase.Add(-12 * time.Minute),
		LandedAt: demoBase.Add(-8 * time.Minute),
	}
	landedCarol = &pushq.EntryRecord{
		Author: recCarol.Author, Message: recCarol.Message,
		JoinedAt: recCarol.JoinedAt,
		LandedAt: demoBase.Add(2 * time.Minute),
	}
	landedBob = &pushq.EntryRecord{
		Author: recBob.Author, Message: recBob.Message,
		JoinedAt: recBob.JoinedAt,
		LandedAt: demoBase.Add(4 * time.Minute),
	}
	landedYou = &pushq.EntryRecord{
		Author: recYou.Author, Message: recYou.Message,
		JoinedAt: recYou.JoinedAt,
		LandedAt: demoBase.Add(6 * time.Minute),
	}
)

func withStatus(r pushq.EntryRecord, status string) pushq.EntryRecord {
	r.Status = status
	return r
}

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
		// joining: carol and bob are already in the queue ahead of you
		{pushq.PhaseChanged{Phase: pushq.PhaseJoining}, 800 * time.Millisecond},
		{pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			withStatus(recCarol, "testing"),
			withStatus(recBob, "waiting"),
			withStatus(recYou, "waiting"),
		}, Landed: landedBefore}, 600 * time.Millisecond},

		// testing: your tests start running concurrently with carol's
		{pushq.PhaseChanged{Phase: pushq.PhaseTesting}, 0},
		{pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			withStatus(recCarol, "testing"),
			withStatus(recBob, "waiting"),
			withStatus(recYou, "testing"),
		}, Landed: landedBefore}, 0},
		{pushq.LogLine{Text: "  > go test ./..."}, 400 * time.Millisecond},
		{pushq.LogLine{Text: ""}, 0},
		{pushq.LogLine{Text: "  ok   github.com/acme/app/api    1.204s"}, 700 * time.Millisecond},
		{pushq.LogLine{Text: "  ok   github.com/acme/app/auth   0.812s"}, 700 * time.Millisecond},
		{pushq.LogLine{Text: "  ok   github.com/acme/app/db     2.001s"}, 0},
		{pushq.LogLine{Text: ""}, 0},
		{pushq.LogLine{Text: "  All tests passed."}, 900 * time.Millisecond},

		// waiting: carol landed, bob is next
		{pushq.PhaseChanged{Phase: pushq.PhaseWaiting}, 0},
		{pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			withStatus(recBob, "testing"),
			withStatus(recYou, "testing"),
		}, Landed: landedCarol}, 2200 * time.Millisecond},

		// landing: bob landed, you are next
		{pushq.PhaseChanged{Phase: pushq.PhaseLanding}, 0},
		{pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			withStatus(recYou, "testing"),
		}, Landed: landedBob}, 1200 * time.Millisecond},

		// you landed — queue is empty, your commit is now the landed entry
		{pushq.QueueStateChanged{Entries: []pushq.EntryRecord{}, Landed: landedYou}, 300 * time.Millisecond},

		// done
		{pushq.Done{}, 0},
	},
}

var allScenarios = map[string]*Scenario{
	"happy-path": &happyPath,
}
