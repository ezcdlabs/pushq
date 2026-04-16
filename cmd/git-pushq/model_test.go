package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// fakeSession is a PushSession whose events are scripted at construction time.
// Tests that only need to exercise Update/View can use an empty fakeSession and
// inject messages directly via send().
type fakeSession struct {
	events []pushq.Event
}

func (f *fakeSession) Start() <-chan pushq.Event {
	ch := make(chan pushq.Event, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch
}

func (f *fakeSession) Cancel() {}

// newTestModel creates a model with a fixed terminal size suitable for tests.
func newTestModel(session ...PushSession) model {
	var s PushSession = &fakeSession{}
	if len(session) > 0 {
		s = session[0]
	}
	m := initialModel(s)
	m.width = 120
	m.height = 30
	return m
}

// send dispatches a single message to the model and returns the updated model.
func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

// TestTUI_RendersEntryInLeftPanel verifies that a QueueStateChanged event
// causes the entry ID to appear in the view.
func TestTUI_RendersEntryInLeftPanel(t *testing.T) {
	m := send(newTestModel(), pushq.QueueStateChanged{
		Entries: []pushq.EntryRecord{
			{ID: "alice-100", Ref: "refs/pushq/alice-100", Status: "testing"},
		},
	})
	if !strings.Contains(m.View(), "alice-100") {
		t.Fatalf("expected alice-100 in view:\n%s", m.View())
	}
}

// TestTUI_ShowsPhaseInView verifies that a PhaseChanged event causes the phase
// name to appear somewhere in the view.
func TestTUI_ShowsPhaseInView(t *testing.T) {
	m := send(newTestModel(), pushq.PhaseChanged{Phase: pushq.PhaseTesting})
	if !strings.Contains(m.View(), "testing") {
		t.Fatalf("expected 'testing' in view:\n%s", m.View())
	}
}

// TestTUI_ShowsLogLineInRightPanel verifies that a LogLine event causes the
// text to appear in the view.
func TestTUI_ShowsLogLineInRightPanel(t *testing.T) {
	m := send(newTestModel(), pushq.LogLine{Text: "all-tests-passed"})
	if !strings.Contains(m.View(), "all-tests-passed") {
		t.Fatalf("expected log line in view:\n%s", m.View())
	}
}

// TestTUI_ShowsErrorOnDone verifies that a Done event with a non-nil error
// causes the error message to appear in the view.
func TestTUI_ShowsErrorOnDone(t *testing.T) {
	m := send(newTestModel(), pushq.Done{Err: fmt.Errorf("tests-failed-exit-1")})
	if !strings.Contains(m.View(), "tests-failed-exit-1") {
		t.Fatalf("expected error message in view:\n%s", m.View())
	}
}

// TestTUI_MultipleLogLinesAccumulate verifies that successive LogLine events
// all appear in the view, not just the most recent one.
func TestTUI_MultipleLogLinesAccumulate(t *testing.T) {
	m := newTestModel()
	m = send(m, pushq.LogLine{Text: "line-one"})
	m = send(m, pushq.LogLine{Text: "line-two"})
	m = send(m, pushq.LogLine{Text: "line-three"})

	view := m.View()
	for _, want := range []string{"line-one", "line-two", "line-three"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view:\n%s", want, view)
		}
	}
}

// TestTUI_QueueUpdatesReplaceOldState verifies that a second QueueStateChanged
// replaces the previous entries rather than accumulating them.
func TestTUI_QueueUpdatesReplaceOldState(t *testing.T) {
	m := newTestModel()
	m = send(m, pushq.QueueStateChanged{
		Entries: []pushq.EntryRecord{{ID: "alice-100", Status: "testing"}},
	})
	m = send(m, pushq.QueueStateChanged{
		Entries: []pushq.EntryRecord{{ID: "bob-200", Status: "waiting"}},
	})

	view := m.View()
	if strings.Contains(view, "alice-100") {
		t.Fatal("expected alice-100 to be gone after second QueueStateChanged")
	}
	if !strings.Contains(view, "bob-200") {
		t.Fatalf("expected bob-200 in view:\n%s", view)
	}
}
