package display

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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

// fixed reference time used across rendering tests
var testNow = time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)

func TestRenderQueueState_OwnEjectedEntryUsesRedMarker(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "ejected"},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)

	// The own ejected line must contain both ">" and "✗"
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, ">") && strings.Contains(line, "✗") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected own ejected entry to have both '>' marker and '✗' icon:\n%s", out)
	}
}

func TestRenderQueueState_MarksOwnEntry(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Author: "alice", Message: "add feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Author: "bob", Message: "fix bug", Status: "waiting"},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)

	if !strings.Contains(out, ">") {
		t.Error("expected own entry to be marked with '>'")
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "bob") && strings.Contains(l, ">") {
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
		{"ejected", "✗"},
	}

	for _, tc := range cases {
		entries := []pushq.EntryRecord{
			{ID: "alice-100-x", Author: "alice", Message: "x", Status: tc.status},
		}
		out := RenderQueueState(entries, "other", nil, 0, testNow, 0)
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

func TestRenderQueueState_ShowsAuthorAndMessage(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add auth endpoint", Status: "testing"},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)
	if !strings.Contains(out, "alice") {
		t.Errorf("expected author in output:\n%s", out)
	}
	if !strings.Contains(out, "add auth endpoint") {
		t.Errorf("expected message in output:\n%s", out)
	}
}

func TestRenderQueueState_ShowsElapsedForActiveEntry(t *testing.T) {
	joinedAt := testNow.Add(-90 * time.Second)
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "testing", JoinedAt: joinedAt},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)
	if !strings.Contains(out, "1m 30s") {
		t.Errorf("expected elapsed '1m 30s' in output:\n%s", out)
	}
}

func TestRenderQueueState_ShowsAllEntryIDs(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-add-feature", Author: "alice", Message: "add feature", Status: "testing"},
		{ID: "bob-90-fix-bug", Author: "bob", Message: "fix bug", Status: "waiting"},
		{ID: "carol-80-update-deps", Author: "carol", Message: "update deps", Status: "waiting"},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)
	for _, name := range []string{"alice", "bob", "carol"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %q in output:\n%s", name, out)
		}
	}
}

func TestRenderQueueState_ReversesOrder(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "a", Status: "testing"},
		{ID: "bob-90-x", Author: "bob", Message: "b", Status: "waiting"},
		{ID: "carol-80-x", Author: "carol", Message: "c", Status: "waiting"},
	}
	out := RenderQueueState(entries, "other", nil, 0, testNow, 0)

	alicePos := strings.Index(out, "alice")
	bobPos := strings.Index(out, "bob")
	carolPos := strings.Index(out, "carol")

	if !(carolPos < bobPos && bobPos < alicePos) {
		t.Errorf("expected carol above bob above alice, got carol=%d bob=%d alice=%d in:\n%s",
			carolPos, bobPos, alicePos, out)
	}
}

func TestRenderQueueState_ShowsLandedAtBottom(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "testing"},
	}
	landed := &pushq.EntryRecord{Author: "bob", Message: "fix navbar"}
	out := RenderQueueState(entries, "alice", landed, 0, testNow, 0)

	if !strings.Contains(out, "fix navbar") {
		t.Errorf("expected landed message in output:\n%s", out)
	}
	alicePos := strings.Index(out, "alice")
	landedPos := strings.Index(out, "fix navbar")
	if landedPos < alicePos {
		t.Errorf("expected landed entry below alice, got landed=%d alice=%d in:\n%s",
			landedPos, alicePos, out)
	}
}

func TestRenderQueueState_LandedShowsFixedElapsed(t *testing.T) {
	joinedAt := testNow.Add(-5 * time.Minute)
	landedAt := testNow.Add(-2 * time.Minute)
	landed := &pushq.EntryRecord{
		Author: "bob", Message: "fix navbar",
		JoinedAt: joinedAt, LandedAt: landedAt,
	}
	out := RenderQueueState(nil, "alice", landed, 0, testNow, 0)
	if !strings.Contains(out, "3m 00s") {
		t.Errorf("expected elapsed '3m 00s' for landed entry:\n%s", out)
	}
}

func TestRenderQueueState_OmitsLandedRowWhenNil(t *testing.T) {
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "x", Status: "testing"},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly 1 line with no landed entry, got %d lines:\n%s", len(lines), out)
	}
}

// --- RunInline layout tests --------------------------------------------------

func TestRunInline_UsesInjectedNowFnForElapsed(t *testing.T) {
	joinedAt := testNow
	injectedNow := testNow.Add(2 * time.Minute)

	session := &fakeSession{events: []pushq.Event{
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "testing", JoinedAt: joinedAt},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, func() time.Time { return injectedNow })

	if !strings.Contains(buf.String(), "2m 00s") {
		t.Errorf("expected elapsed '2m 00s' from injected nowFn, got:\n%s", buf.String())
	}
}

func TestRunInline_JoiningPhase_ShowsJoiningStatus(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

	out := buf.String()
	if !strings.Contains(out, "joining") {
		t.Errorf("expected 'joining' in output while joining:\n%s", out)
	}
}

func TestRunInline_AfterJoining_ShowsJoinedHeader(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Author: "alice", Message: "add feature", Status: "testing"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

	out := buf.String()
	if !strings.Contains(out, "joined") {
		t.Errorf("expected 'joined' in output after receiving queue state:\n%s", out)
	}
}

func TestRunInline_AfterJoining_ShowsQueueHeader(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Author: "alice", Message: "add feature", Status: "testing"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

	out := buf.String()
	if !strings.Contains(out, "Queue") {
		t.Errorf("expected 'Queue' header in output after joining:\n%s", out)
	}
}

func TestRunInline_PrintsQueueOnStateChange(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.PhaseChanged{Phase: pushq.PhaseJoining},
		pushq.QueueStateChanged{Entries: []pushq.EntryRecord{
			{ID: "alice-100-add-feature", Author: "alice", Message: "add feature", Status: "testing"},
			{ID: "bob-90-fix-bug", Author: "bob", Message: "fix bug", Status: "waiting"},
		}},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

	out := buf.String()
	if !strings.Contains(out, "alice") {
		t.Errorf("expected alice in output:\n%s", out)
	}
	if !strings.Contains(out, "bob") {
		t.Errorf("expected bob in output:\n%s", out)
	}
}

func TestRunInline_AlwaysShowsNoteEvents(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.Note{Text: "retesting — entry ahead was ejected"},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

	if !strings.Contains(buf.String(), "retesting — entry ahead was ejected") {
		t.Errorf("expected Note text in output regardless of verbose:\n%s", buf.String())
	}
}

func TestRunInline_SuppressesLogLinesByDefault(t *testing.T) {
	session := &fakeSession{events: []pushq.Event{
		pushq.LogLine{Text: "secret-test-output"},
		pushq.Done{},
	}}

	var buf strings.Builder
	RunInline(session, &buf, "alice", false, nil)

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
	RunInline(session, &buf, "alice", true, nil)

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

	if strings.Contains(buf.String(), "\033[") {
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

	p.print("a\nb\nc\n")
	first := buf.String()

	p.print("x\ny\nz\n")
	second := buf.String()[len(first):]

	if !strings.Contains(second, "\033[3A") {
		t.Errorf("expected \\033[3A (cursor up 3) in second print, got: %q", second)
	}
}

func TestRenderQueueState_RightAlignsElapsedToWidth(t *testing.T) {
	const width = 60
	joinedAt := testNow.Add(-90 * time.Second)
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "testing", JoinedAt: joinedAt},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, width)

	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		got := lipgloss.Width(line)
		if got != width {
			t.Errorf("expected visual line width %d, got %d: %q", width, got, line)
		}
	}
}

func TestRenderQueueState_RightAlignsLandedElapsedToWidth(t *testing.T) {
	const width = 60
	joinedAt := testNow.Add(-5 * time.Minute)
	landedAt := testNow.Add(-2 * time.Minute)
	landed := &pushq.EntryRecord{
		Author: "bob", Message: "fix navbar",
		JoinedAt: joinedAt, LandedAt: landedAt,
	}
	out := RenderQueueState(nil, "alice", landed, 0, testNow, width)

	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		got := lipgloss.Width(line)
		if got != width {
			t.Errorf("expected visual line width %d, got %d: %q", width, got, line)
		}
	}
}

func TestRenderQueueState_ZeroWidthNoRightAlign(t *testing.T) {
	joinedAt := testNow.Add(-90 * time.Second)
	entries := []pushq.EntryRecord{
		{ID: "alice-100-x", Author: "alice", Message: "add feature", Status: "testing", JoinedAt: joinedAt},
	}
	out := RenderQueueState(entries, "alice", nil, 0, testNow, 0)
	if !strings.Contains(out, "1m 30s") {
		t.Errorf("expected elapsed in output without width:\n%s", out)
	}
}

func TestSnapshotPrinter_InPlace_ClearsExtraLinesWhenShrinking(t *testing.T) {
	var buf strings.Builder
	p := &snapshotPrinter{out: &buf, inPlace: true}

	p.print("a\nb\nc\n")
	first := buf.String()

	p.print("x\n")
	second := buf.String()[len(first):]

	clearCount := strings.Count(second, "\033[K")
	if clearCount < 3 {
		t.Errorf("expected at least 3 clear-to-EOL sequences when shrinking, got %d in: %q", clearCount, second)
	}
}
