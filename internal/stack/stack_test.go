package stack_test

import (
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/internal/stack"
)

// TestBuild_NoEntriesAhead_JustOwnCommit verifies that when there are no
// entries ahead in the queue, the test branch contains only the developer's
// own commit on top of main.
func TestBuild_NoEntriesAhead_JustOwnCommit(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	// Push own commit to a queue ref.
	clone.WriteFile("feature.txt", "my feature")
	clone.CommitAll("my feature")
	clone.PushRef("HEAD", "refs/pushq/alice-100")

	result, err := stack.Build(stack.Options{
		RepoPath:     clone.Path,
		Remote:       "origin",
		MainBranch:   "main",
		OwnRef:       "refs/pushq/alice-100",
		EntriesAhead: nil,
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer result.Cleanup()

	commits := clone.LogBranch(result.BranchName)
	if len(commits) < 2 {
		t.Fatalf("expected at least 2 commits on test branch, got %d", len(commits))
	}
	if commits[0].Message != "my feature" {
		t.Fatalf("expected own commit on top, got %q", commits[0].Message)
	}
}

// TestBuild_WithEntriesAhead_StacksInOrder verifies that entries ahead are
// applied in queue order before the developer's own commit.
func TestBuild_WithEntriesAhead_StacksInOrder(t *testing.T) {
	remote := gittest.NewRemote(t)
	alice := remote.NewClone(t)
	bob := remote.NewClone(t)

	// Alice pushes her commit first.
	alice.WriteFile("alice.txt", "alice's work")
	alice.CommitAll("alice's feature")
	alice.PushRef("HEAD", "refs/pushq/alice-100")

	// Bob fetches and builds his stack with alice ahead.
	bob.Fetch()
	bob.WriteFile("bob.txt", "bob's work")
	bob.CommitAll("bob's feature")
	bob.PushRef("HEAD", "refs/pushq/bob-200")

	result, err := stack.Build(stack.Options{
		RepoPath:     bob.Path,
		Remote:       "origin",
		MainBranch:   "main",
		OwnRef:       "refs/pushq/bob-200",
		EntriesAhead: []string{"refs/pushq/alice-100"},
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer result.Cleanup()

	commits := bob.LogBranch(result.BranchName)
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	// bob's commit is on top, alice's is below it.
	if commits[0].Message != "bob's feature" {
		t.Fatalf("expected bob's commit on top, got %q", commits[0].Message)
	}
	found := false
	for _, m := range messages {
		if m == "alice's feature" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected alice's commit in stack, got: %v", messages)
	}
}

// TestBuild_ConflictingEntry_IsSkipped verifies that an entry ahead whose
// cherry-pick conflicts with an earlier entry is skipped, and the build
// continues with the remaining entries.
//
// Setup: alice and charlie are both ahead of carol. Charlie's commit conflicts
// with alice's (both edit shared.txt). Carol's commit is independent. The
// expected stack is main → alice → carol, with charlie skipped.
func TestBuild_ConflictingEntry_IsSkipped(t *testing.T) {
	remote := gittest.NewRemote(t)
	alice := remote.NewClone(t)
	charlie := remote.NewClone(t)
	carol := remote.NewClone(t)

	// Alice adds shared.txt.
	alice.WriteFile("shared.txt", "alice's version")
	alice.CommitAll("alice edits shared.txt")
	alice.PushRef("HEAD", "refs/pushq/alice-100")

	// Charlie also adds shared.txt with different content — conflicts with alice.
	charlie.WriteFile("shared.txt", "charlie's version")
	charlie.CommitAll("charlie edits shared.txt")
	charlie.PushRef("HEAD", "refs/pushq/charlie-200")

	// Carol edits a completely different file — no conflict with either.
	carol.WriteFile("carol.txt", "carol's work")
	carol.CommitAll("carol adds carol.txt")
	carol.PushRef("HEAD", "refs/pushq/carol-300")

	carol.Fetch()

	result, err := stack.Build(stack.Options{
		RepoPath:     carol.Path,
		Remote:       "origin",
		MainBranch:   "main",
		OwnRef:       "refs/pushq/carol-300",
		EntriesAhead: []string{"refs/pushq/alice-100", "refs/pushq/charlie-200"},
	})
	if err != nil {
		t.Fatalf("Build should not fail when a conflicting entry ahead is skipped, got: %v", err)
	}
	defer result.Cleanup()

	commits := carol.LogBranch(result.BranchName)
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	// Charlie's conflicting commit must be absent.
	for _, m := range messages {
		if m == "charlie edits shared.txt" {
			t.Fatalf("charlie's conflicting entry should have been skipped, stack: %v", messages)
		}
	}
	// Alice's and carol's commits must be present.
	if commits[0].Message != "carol adds carol.txt" {
		t.Fatalf("expected carol's commit on top, got %q", commits[0].Message)
	}
	found := false
	for _, m := range messages {
		if m == "alice edits shared.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected alice's commit in stack, got: %v", messages)
	}
}

// TestBuild_Cleanup_DeletesBranch verifies that Cleanup removes the temporary
// test branch from the local repo.
func TestBuild_Cleanup_DeletesBranch(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "feature")
	clone.CommitAll("my feature")
	clone.PushRef("HEAD", "refs/pushq/alice-100")

	result, err := stack.Build(stack.Options{
		RepoPath:     clone.Path,
		Remote:       "origin",
		MainBranch:   "main",
		OwnRef:       "refs/pushq/alice-100",
		EntriesAhead: nil,
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	branchName := result.BranchName
	result.Cleanup()

	// The branch should no longer exist.
	commits := clone.LogBranch(branchName)
	if len(commits) > 0 {
		t.Fatalf("expected test branch to be deleted after Cleanup")
	}
}
