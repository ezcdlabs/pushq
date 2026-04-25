package pushq

import (
	"fmt"
	"time"
)

// EntryID returns the queue entry identifier for a given username and join time.
func EntryID(username string, t time.Time) string {
	return username + "-" + fmt.Sprintf("%d", t.UnixMilli())
}

// Event is the sealed interface for all events emitted by Push.
type Event interface {
	sealedEvent()
}

// EntryRecord is a queue entry as seen by the caller.
type EntryRecord struct {
	ID     string
	Ref    string
	Status string
}

// QueueStateChanged is emitted after joining and on each state poll during the
// wait loop. Entries contains only active (non-landed) entries in queue order.
type QueueStateChanged struct {
	Entries []EntryRecord
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

// Done is the terminal event — always the last event emitted on the channel.
// Err is nil on success, non-nil on failure or ejection.
type Done struct {
	Err error
}

func (Done) sealedEvent() {}
