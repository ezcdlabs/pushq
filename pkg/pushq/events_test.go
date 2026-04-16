package pushq_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// collectEvents reads from a Push event channel until Done is received.
func collectEvents(t *testing.T, ch <-chan pushq.Event) []pushq.Event {
	t.Helper()
	var events []pushq.Event
	for ev := range ch {
		events = append(events, ev)
		if _, ok := ev.(pushq.Done); ok {
			return events
		}
	}
	return events
}

// TestPush_EmitsDoneWithNilErrOnSuccess verifies that a successful push ends
// with a Done event carrying a nil error.
func TestPush_EmitsDoneWithNilErrOnSuccess(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	events := collectEvents(t, pushq.Push(context.Background(), alicePushOpts(clone.Path)))

	last := events[len(events)-1]
	done, ok := last.(pushq.Done)
	if !ok {
		t.Fatalf("expected last event to be Done, got %T", last)
	}
	if done.Err != nil {
		t.Fatalf("expected Done.Err nil on success, got: %v", done.Err)
	}
}

// TestPush_EmitsDoneWithErrOnTestFailure verifies that a push where tests fail
// ends with a Done event carrying a non-nil error.
func TestPush_EmitsDoneWithErrOnTestFailure(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	events := collectEvents(t, pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.FailingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",
	}))

	last := events[len(events)-1]
	done, ok := last.(pushq.Done)
	if !ok {
		t.Fatalf("expected last event to be Done, got %T", last)
	}
	if done.Err == nil {
		t.Fatal("expected Done.Err non-nil when tests fail")
	}
}

// TestPush_EmitsLogLinesFromTestCommand verifies that stdout/stderr from the
// test command appears as LogLine events in the stream.
func TestPush_EmitsLogLinesFromTestCommand(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	events := collectEvents(t, pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   "echo hello-from-test",
		CommitMessage: "add feature",
		Username:      "alice",
	}))

	var logLines []string
	for _, ev := range events {
		if ll, ok := ev.(pushq.LogLine); ok {
			logLines = append(logLines, ll.Text)
		}
	}

	found := false
	for _, l := range logLines {
		if strings.Contains(l, "hello-from-test") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'hello-from-test' in log lines, got: %v", logLines)
	}
}

// TestPush_EmitsQueueStateWithOurEntryAfterJoining verifies that at least one
// QueueStateChanged event is emitted after joining and that it contains our entry.
func TestPush_EmitsQueueStateWithOurEntryAfterJoining(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	events := collectEvents(t, pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",
	}))

	found := false
	for _, ev := range events {
		qs, ok := ev.(pushq.QueueStateChanged)
		if !ok {
			continue
		}
		for _, e := range qs.Entries {
			if strings.HasPrefix(e.ID, "alice-") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected a QueueStateChanged event containing alice's entry after joining")
	}
}

// TestPush_EmitsPhaseChangedEvents verifies that PhaseChanged events are
// emitted and cover the expected phases for a single-developer push.
func TestPush_EmitsPhaseChangedEvents(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	events := collectEvents(t, pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",
	}))

	phases := make(map[pushq.Phase]bool)
	for _, ev := range events {
		if pc, ok := ev.(pushq.PhaseChanged); ok {
			phases[pc.Phase] = true
		}
	}

	for _, want := range []pushq.Phase{pushq.PhaseJoining, pushq.PhaseTesting, pushq.PhaseLanding} {
		if !phases[want] {
			t.Errorf("expected PhaseChanged{%q} to be emitted, but it wasn't", want)
		}
	}
}
