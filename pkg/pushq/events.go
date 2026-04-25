package pushq

import (
	"fmt"
	"strings"
	"time"
)

// EntryID returns the queue entry identifier for a given username, join time,
// and squash commit message.
func EntryID(username string, t time.Time, commitMessage string) string {
	id := username + "-" + fmt.Sprintf("%d", t.UnixMilli())
	if slug := slugifyCommitMessage(commitMessage); slug != "" {
		id += "-" + slug
	}
	return id
}

// slugifyCommitMessage converts a commit message into a short lowercase
// hyphenated string safe for use in a git ref or queue entry ID.
func slugifyCommitMessage(msg string) string {
	msg = strings.ToLower(msg)
	var b strings.Builder
	prevHyphen := true // suppress leading hyphens
	for _, r := range msg {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == ' ' || r == '-':
			if !prevHyphen {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// Event is the sealed interface for all events emitted by Push.
type Event interface {
	sealedEvent()
}

// EntryRecord is a queue entry as seen by the caller.
type EntryRecord struct {
	ID       string
	Ref      string
	Status   string
	Author   string
	Message  string
	JoinedAt time.Time
	LandedAt time.Time // zero if not yet landed
}

// QueueStateChanged is emitted after joining and on each state poll during
// the wait loop. Entries contains active (non-landed) entries in queue order.
// Landed is the most recently landed entry; nil if unavailable.
type QueueStateChanged struct {
	Entries []EntryRecord
	Landed  *EntryRecord
}

func (QueueStateChanged) sealedEvent() {}

// Phase is a named stage of the Push lifecycle.
type Phase string

const (
	PhaseJoining       Phase = "joining"
	PhaseBuildingStack Phase = "building-stack"
	PhaseTesting       Phase = "testing"
	PhaseWaiting       Phase = "waiting"
	PhaseLanding       Phase = "landing"
)

// PhaseChanged is emitted when Push moves to a new phase of work.
type PhaseChanged struct {
	Phase Phase
}

func (PhaseChanged) sealedEvent() {}

// LogLine is a single line of stdout/stderr from the test command.
type LogLine struct {
	Text string
}

func (LogLine) sealedEvent() {}

// Note is an informational message emitted by Push for display to the user.
// Unlike LogLine, Note is always shown regardless of verbose mode.
type Note struct {
	Text string
}

func (Note) sealedEvent() {}

// Done is the terminal event — always the last event emitted on the channel.
// Err is nil on success, non-nil on failure or ejection.
type Done struct {
	Err error
}

func (Done) sealedEvent() {}
