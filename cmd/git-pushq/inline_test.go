package main

import (
	"strings"
	"testing"

	"github.com/ezcdlabs/pushq/pkg/pushq"
)

func TestRenderQueueState_MarksOwnEntry(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Status: "waiting"},
	}
	out := renderQueueState(entries, "alice", pushq.PhaseTesting)

	if !strings.Contains(out, ">") {
		t.Error("expected own entry to be marked with '>'")
	}
	// bob is not ours — should not be marked
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if strings.Contains(l, "bob-90-fix-bug") && strings.Contains(l, ">") {
			t.Error("expected bob's entry to NOT be marked with '>'")
		}
	}
}

func TestRenderQueueState_Icons(t *testing.T) {
	cases := []struct {
		status   string
		wantIcon string
	}{
		{"waiting", "·"},
		{"testing", "⠴"},
		{"done", "✔"},
	}

	for _, tc := range cases {
		entries := []pushq.EntryRecord{
			{ID: "alice-100-x", Status: tc.status},
		}
		out := renderQueueState(entries, "other", pushq.PhaseTesting)
		if !strings.Contains(out, tc.wantIcon) {
			t.Errorf("status %q: expected icon %q in output:\n%s", tc.status, tc.wantIcon, out)
		}
	}
}

func TestRenderQueueState_ShowsPhase(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Status: "testing"},
	}
	out := renderQueueState(entries, "alice", pushq.PhaseTesting)
	if !strings.Contains(out, "testing") {
		t.Errorf("expected phase 'testing' in output:\n%s", out)
	}
}

func TestRenderQueueState_ShowsAllEntryIDs(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Status: "waiting"},
		{ID: "carol-80-update-deps", Status: "waiting"},
	}
	out := renderQueueState(entries, "alice", pushq.PhaseTesting)
	for _, id := range []string{"alice-100-add-feature", "bob-90-fix-bug", "carol-80-update-deps"} {
		if !strings.Contains(out, id) {
			t.Errorf("expected entry ID %q in output:\n%s", id, out)
		}
	}
}

func TestRunInline_PrintsQueueOnStateChange(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Status: "testing"},
			{ID: "bob-90-fix-bug", Status: "waiting"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	runInline(session, &buf, "alice", false)

	out := buf.String()
	if !strings.Contains(out, "alice-100-add-feature") {
		t.Errorf("expected alice's entry in output:\n%s", out)
	}
	if !strings.Contains(out, "bob-90-fix-bug") {
		t.Errorf("expected bob's entry in output:\n%s", out)
	}
}

func TestRunInline_SuppressesLogLinesByDefault(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.LogLine{Text: "secret-test-output"},
		pushq.Done{},
	}}

	var buf strings.Builder
	runInline(session, &buf, "alice", false)

	if strings.Contains(buf.String(), "secret-test-output") {
		t.Errorf("expected log lines to be suppressed by default, but found in output:\n%s", buf.String())
	}
}

func TestRunInline_PrintsLogLinesWhenVerbose(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.LogLine{Text: "test-output-line"},
		pushq.Done{},
	}}

	var buf strings.Builder
	runInline(session, &buf, "alice", true)

	if !strings.Contains(buf.String(), "test-output-line") {
		t.Errorf("expected log lines in verbose output:\n%s", buf.String())
	}
}
