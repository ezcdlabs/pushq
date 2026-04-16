package protocol

// Status represents the lifecycle state of a queue entry.
type Status string

const (
	StatusWaiting Status = "waiting"
	StatusTesting Status = "testing"
)

// QueueEntry is the structure stored as entries/<entry-id>.json on the state branch.
type QueueEntry struct {
	Ref    string `json:"ref"`
	Status Status `json:"status"`
}
