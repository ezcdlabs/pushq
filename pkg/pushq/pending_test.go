package pushq_test

import (
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

func TestListPendingCommits_NoPendingCommits_ReturnsEmpty(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	commits, err := pushq.ListPendingCommits(clone.Path, "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected no pending commits, got: %v", commits)
	}
}

func TestListPendingCommits_SingleCommit_ReturnsThat(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "content")
	clone.CommitAll("add feature")

	commits, err := pushq.ListPendingCommits(clone.Path, "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 pending commit, got %d: %v", len(commits), commits)
	}
	if commits[0].Subject != "add feature" {
		t.Fatalf("expected subject %q, got %q", "add feature", commits[0].Subject)
	}
}

func TestListPendingCommits_MultipleCommits_ReturnsOldestFirst(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("a.txt", "a")
	clone.CommitAll("first commit")
	clone.WriteFile("b.txt", "b")
	clone.CommitAll("second commit")
	clone.WriteFile("c.txt", "c")
	clone.CommitAll("third commit")

	commits, err := pushq.ListPendingCommits(clone.Path, "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("expected 3 pending commits, got %d", len(commits))
	}
	if commits[0].Subject != "first commit" || commits[1].Subject != "second commit" || commits[2].Subject != "third commit" {
		t.Fatalf("expected oldest-first order, got: %v", commits)
	}
}

func TestListPendingCommits_CommitAlreadyOnMain_NotIncluded(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("landed.txt", "already on main")
	clone.CommitAll("landed commit")
	clone.Push("main")

	clone.WriteFile("pending.txt", "not yet pushed")
	clone.CommitAll("pending commit")

	commits, err := pushq.ListPendingCommits(clone.Path, "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 pending commit, got %d: %v", len(commits), commits)
	}
	if commits[0].Subject != "pending commit" {
		t.Fatalf("expected %q, got %q", "pending commit", commits[0].Subject)
	}
}

func TestListPendingCommits_IncludesAuthorAndHash(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("file.txt", "content")
	clone.CommitAll("my commit")

	commits, err := pushq.ListPendingCommits(clone.Path, "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if len(commits[0].Hash) != 40 {
		t.Fatalf("expected 40-char hash, got %q", commits[0].Hash)
	}
	if commits[0].Author == "" {
		t.Fatal("expected non-empty author")
	}
}
