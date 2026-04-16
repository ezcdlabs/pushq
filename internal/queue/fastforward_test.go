package queue

import (
	"fmt"
	"testing"
)

// TestIsFastForwardRejected documents all known retryable push rejection
// messages from go-git. When a new message is discovered at runtime, add
// a failing case here first, then extend isFastForwardRejected.
func TestIsFastForwardRejected(t *testing.T) {
	cases := []struct {
		msg      string
		expected bool
	}{
		// known retryable messages
		{"non-fast-forward update", true},
		{"failed to update ref", true},
		{"reference already exists", true},
		{"incorrect old value provided", true},
		// wrapping should still match
		{"push state branch: command error on refs/pushq/state: reference already exists", true},
		{"push state branch: command error on refs/pushq/state: incorrect old value provided", true},
		// non-retryable errors must not be swallowed
		{"authentication required", false},
		{"connection refused", false},
		{"repository not found", false},
	}

	for _, c := range cases {
		got := isFastForwardRejected(fmt.Errorf("%s", c.msg))
		if got != c.expected {
			t.Errorf("isFastForwardRejected(%q) = %v, want %v", c.msg, got, c.expected)
		}
	}
}
