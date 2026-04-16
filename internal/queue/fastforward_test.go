package queue

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
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

// retryScenarios lists each retryable error message and a descriptive name.
var retryScenarios = []struct {
	name string
	msg  string
}{
	{"non-fast-forward", "non-fast-forward update"},
	{"failed to update ref", "failed to update ref"},
	{"reference already exists", "reference already exists"},
	{"incorrect old value provided", "incorrect old value provided"},
}

// TestUpdateStateBranch_RetriesOnKnownRejections verifies that the optimistic
// lock loop retries and succeeds when the push returns each known retryable
// error on the first attempt. Injected deterministically — no goroutines.
func TestUpdateStateBranch_RetriesOnKnownRejections(t *testing.T) {
	for _, sc := range retryScenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			remote := gittest.NewRemote(t)
			clone := remote.NewClone(t)

			var attempts atomic.Int32
			sm := &stateManager{
				pushFn: func(repoPath, r string) error {
					if attempts.Add(1) == 1 {
						return fmt.Errorf("%s", sc.msg)
					}
					return pushStateBranch(repoPath, r)
				},
			}

			err := sm.join(clone.Path, "origin", "alice-1", "refs/pushq/alice-1")
			if err != nil {
				t.Fatalf("expected retry to succeed, got: %v", err)
			}
			if attempts.Load() < 2 {
				t.Fatalf("expected at least 2 push attempts, got %d", attempts.Load())
			}
		})
	}
}

// TestUpdateStateBranch_DoesNotRetryOnRealError verifies that non-retryable
// errors are returned immediately without retrying.
func TestUpdateStateBranch_DoesNotRetryOnRealError(t *testing.T) {
	t.Parallel()
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	var attempts atomic.Int32
	sm := &stateManager{
		pushFn: func(repoPath, r string) error {
			attempts.Add(1)
			return fmt.Errorf("authentication required")
		},
	}

	err := sm.join(clone.Path, "origin", "alice-1", "refs/pushq/alice-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected exactly 1 push attempt, got %d", attempts.Load())
	}
}

// joinRaceScenarios covers each distinct push-rejection interleaving that can
// occur when two developers join concurrently.
var joinRaceScenarios = []struct {
	name        string
	rejectedMsg string
}{
	{"non-fast-forward", "non-fast-forward update"},
	{"failed to update ref", "failed to update ref"},
	{"reference already exists", "reference already exists"},
	{"incorrect old value provided", "incorrect old value provided"},
}

// TestJoin_Race_BothEntriesLand verifies that when Bob's first push is rejected
// with any known retryable error, he retries and both he and Alice end up in the
// queue. Each interleaving is a separate sub-test — no goroutines.
func TestJoin_Race_BothEntriesLand(t *testing.T) {
	for _, sc := range joinRaceScenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			remote := gittest.NewRemote(t)
			alice := remote.NewClone(t)
			bob := remote.NewClone(t)

			// Alice joins for real, creating the state branch.
			if err := Join(alice.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
				t.Fatalf("alice join failed: %v", err)
			}

			// Bob's first push is rejected; subsequent pushes use the real impl.
			var attempts atomic.Int32
			sm := &stateManager{
				pushFn: func(repoPath, r string) error {
					if attempts.Add(1) == 1 {
						return fmt.Errorf("%s", sc.rejectedMsg)
					}
					return pushStateBranch(repoPath, r)
				},
			}

			if err := sm.join(bob.Path, "origin", "bob-1", "refs/pushq/bob-1"); err != nil {
				t.Fatalf("bob join failed after retry: %v", err)
			}

			entries, err := ListEntries(bob.Path, "origin")
			if err != nil {
				t.Fatalf("ListEntries failed: %v", err)
			}
			if len(entries) != 2 {
				t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
			}
		})
	}
}

// TestJoin_Race_MultipleRetries_BothLand verifies that the retry loop handles
// being rejected more than once — Bob is rejected three times before succeeding.
func TestJoin_Race_MultipleRetries_BothLand(t *testing.T) {
	t.Parallel()
	remote := gittest.NewRemote(t)
	alice := remote.NewClone(t)
	bob := remote.NewClone(t)

	if err := Join(alice.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
		t.Fatalf("alice join failed: %v", err)
	}

	var attempts atomic.Int32
	sm := &stateManager{
		pushFn: func(repoPath, r string) error {
			if attempts.Add(1) <= 3 {
				return fmt.Errorf("non-fast-forward update")
			}
			return pushStateBranch(repoPath, r)
		},
	}

	if err := sm.join(bob.Path, "origin", "bob-1", "refs/pushq/bob-1"); err != nil {
		t.Fatalf("bob join failed after multiple retries: %v", err)
	}
	if attempts.Load() != 4 {
		t.Fatalf("expected 4 push attempts (3 failures + 1 success), got %d", attempts.Load())
	}

	entries, err := ListEntries(bob.Path, "origin")
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
	}
}
