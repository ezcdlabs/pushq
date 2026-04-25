package display

import (
	"strings"
	"testing"

	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// fakeSession is a PushSession whose events are scripted at construction time.
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

func TestRenderQueueState_MarksOwnEntry(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Status: "waiting"},
	}
	out := RenderQueueState(entries, "alice", "", 0)

	if !strings.Contains(out, ">") {
		t.Error("expected own entry to be marked with '>'")
	}
	for _, l := range strings.Split(out, "\n") {
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
		{"testing", spinnerFrames[0]},
		{"done", "✔"},
	}

	for _, tc := range cases {
		entries := []pushq.EntryRecord{
			{ID: "alice-100-x", Status: tc.status},
		}
		out := RenderQueueState(entries, "other", "", 0)
		if !strings.Contains(out, tc.wantIcon) {
			t.Errorf("status %q: expected icon %q in output:\n%s", tc.status, tc.wantIcon, out)
		}
	}
}

func TestEntryIcon_TestingSpinnerAdvances(t *testing.T) {
	frame0 := EntryIcon("testing", 0)
	frame1 := EntryIcon("testing", 1)
	if frame0 == frame1 {
		t.Error("expected different spinner frames for index 0 and 1")
	}
}

func TestRenderQueueState_ShowsAllEntryIDs(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Status: "waiting"},
		{ID: "carol-80-update-deps", Status: "waiting"},
	}
	out := RenderQueueState(entries, "alice", "", 0)
	for _, id := range []string{"alice-100-add-feature", "bob-90-fix-bug", "carol-80-update-deps"} {
		if !strings.Contains(out, id) {
			t.Errorf("expected entry ID %q in output:\n%s", id, out)
		}
	}
}

func TestRenderQueueState_ReversesOrder(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"}, // first in queue (lands first)
		{ID: "bob-90-fix-bug", Status: "waiting"},
		{ID: "carol-80-update-deps", Status: "waiting"}, // last in queue
	}
	out := RenderQueueState(entries, "other", "", 0)

	alicePos := strings.Index(out, "alice")
	bobPos := strings.Index(out, "bob")
	carolPos := strings.Index(out, "carol")

	// display order should be carol (top) → bob → alice (bottom, closest to landing)
	if !(carolPos < bobPos && bobPos < alicePos) {
		t.Errorf("expected carol above bob above alice, got carol=%d bob=%d alice=%d in:\n%s",
			carolPos, bobPos, alicePos, out)
	}
}

func TestRenderQueueState_ShowsLandedAtBottom(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Status: "testing"},
	}
	out := RenderQueueState(entries, "alice", "abc1234 fix navbar", 0)

	if !strings.Contains(out, "fix navbar") {
		t.Errorf("expected landed commit in output:\n%s", out)
	}
	alicePos := strings.Index(out, "alice")
	landedPos := strings.Index(out, "fix navbar")
	if landedPos < alicePos {
		t.Errorf("expected landed entry below alice, got landed=%d alice=%d in:\n%s",
			landedPos, alicePos, out)
	}
}

func TestRenderQueueState_OmitsLandedRowWhenEmpty(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Status: "testing"},
	}
	out := RenderQueueState(entries, "alice", "", 0)
	// Should not have a stray checkmark from a landed row
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly 1 line with no landed entry, got %d lines:\n%s", len(lines), out)
	}
}

// --- RunInline layout tests --------------------------------------------------

func TestRunInline_JoiningPhase_ShowsJoiningStatus(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false)

	out := buf.String()
	if !strings.Contains(out, "joining") {
		t.Errorf("expected 'joining' in output while joining:\n%s", out)
	}
}

func TestRunInline_AfterJoining_ShowsJoinedHeader(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Status: "testing"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false)

	out := buf.String()
	if !strings.Contains(out, "joined") {
		t.Errorf("expected 'joined' in output after receiving queue state:\n%s", out)
	}
}

func TestRunInline_AfterJoining_ShowsQueueHeader(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Status: "testing"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false)

	out := buf.String()
	if !strings.Contains(out, "Queue") {
		t.Errorf("expected 'Queue' header in output after joining:\n%s", out)
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
	RunInline(session, &buf, "alice", false)

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
	RunInline(session, &buf, "alice", false)

	if strings.Contains(buf.String(), "secret-test-output") {
		t.Errorf("expected log lines suppressed by default:\n%s", buf.String())
	}
}

func TestRunInline_PrintsLogLinesWhenVerbose(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.LogLine{Text: "test-output-line"},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", true)

	if !strings.Contains(buf.String(), "test-output-line") {
		t.Errorf("expected log lines in verbose output:\n%s", buf.String())
	}
}

// --- snapshotPrinter unit tests ----------------------------------------------

func TestSnapshotPrinter_NonInPlace_NoAnsiEscapes(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: false}

	p.print("line1\nline2\n")
	p.print("line1\nline2\n")

	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("expected no ANSI escapes in non-in-place mode, got: %q", buf.String())
	}
}

func TestSnapshotPrinter_InPlace_FirstPrintNoEscapes(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: true}

	p.print("line1\nline2\n")

	// First print: no cursor movement yet (nothing to overwrite)
	if strings.Contains(buf.String(), "\033[") {
		// Allow \033[K (clear to EOL) but not cursor-up sequences
		for _, seq := range strings.Split(buf.String(), "\033[") {
			if len(seq) > 0 && seq[0] == '[' {
				t.Errorf("unexpected cursor-movement sequence in first print: %q", buf.String())
			}
		}
	}
}

func TestSnapshotPrinter_InPlace_SecondPrintHasCursorUp(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: true}

	p.print("line1\nline2\n")
	first := buf.String()

	p.print("line1\nline2\n")
	second := buf.String()[len(first):]

	if !strings.Contains(second, "\033[") {
		t.Errorf("expected ANSI cursor-up in second print, got: %q", second)
	}
}

func TestSnapshotPrinter_InPlace_MovesUpByLineCount(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: true}

	// First print: 3 lines
	p.print("a\nb\nc\n")
	first := buf.String()

	// Second print: cursor should move up 3 lines
	p.print("x\ny\nz\n")
	second := buf.String()[len(first):]

	if !strings.Contains(second, "\033[3A") {
		t.Errorf("expected \\033[3A (cursor up 3) in second print, got: %q", second)
	}
}

func TestSnapshotPrinter_InPlace_ClearsExtraLinesWhenShrinking(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: true}

	p.print("a\nb\nc\n") // 3 lines
	first := buf.String()

	p.print("x\n") // 1 line — shrinking
	second := buf.String()[len(first):]

	// Must clear the 2 lines that are no longer part of the snapshot
	clearCount := strings.Count(second, "\033[K")
	if clearCount < 3 { // 1 new line + 2 cleared old lines
		t.Errorf("expected at least 3 clear-to-EOL sequences when shrinking, got %d in: %q", clearCount, second)
	}
}
