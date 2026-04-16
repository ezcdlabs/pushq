package pushq_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// --- HasUncommittedChanges ---------------------------------------------------

func TestHasUncommittedChanges_CleanTree_ReturnsFalse(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	has, err := pushq.HasUncommittedChanges(clone.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected false for clean working tree")
	}
}

func TestHasUncommittedChanges_UnstagedModification_ReturnsTrue(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("file.txt", "original")
	clone.CommitAll("add file")

	// Modify without staging.
	if err := os.WriteFile(filepath.Join(clone.Path, "file.txt"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	has, err := pushq.HasUncommittedChanges(clone.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected true for unstaged modification")
	}
}

func TestHasUncommittedChanges_StagedModification_ReturnsTrue(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("staged.txt", "content")
	// WriteFile + stage but don't commit.
	clone.WriteFile("staged.txt", "changed")
	// Stage the file.
	if err := runGit(clone.Path, "add", "staged.txt"); err != nil {
		t.Fatal(err)
	}

	has, err := pushq.HasUncommittedChanges(clone.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected true for staged modification")
	}
}

func TestHasUncommittedChanges_UntrackedFile_ReturnsTrue(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	// Write a file but don't stage it.
	if err := os.WriteFile(filepath.Join(clone.Path, "untracked.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	has, err := pushq.HasUncommittedChanges(clone.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected true for untracked file")
	}
}

// --- Stash / StashPop --------------------------------------------------------

func TestStash_ClearsWorkingTree(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "work in progress")
	// Leave it unstaged.
	if err := os.WriteFile(filepath.Join(clone.Path, "feature.txt"), []byte("work in progress"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := pushq.Stash(clone.Path); err != nil {
		t.Fatalf("Stash failed: %v", err)
	}

	has, err := pushq.HasUncommittedChanges(clone.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected clean working tree after Stash")
	}
}

func TestStash_IncludesUntrackedFiles(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	// Create an untracked file (never staged).
	if err := os.WriteFile(filepath.Join(clone.Path, "untracked.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := pushq.Stash(clone.Path); err != nil {
		t.Fatalf("Stash failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(clone.Path, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatal("expected untracked file to be stashed (not present in working tree)")
	}
}

func TestStashPop_RestoresChanges(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	if err := os.WriteFile(filepath.Join(clone.Path, "wip.txt"), []byte("wip"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := pushq.Stash(clone.Path); err != nil {
		t.Fatalf("Stash failed: %v", err)
	}
	if err := pushq.StashPop(clone.Path); err != nil {
		t.Fatalf("StashPop failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(clone.Path, "wip.txt"))
	if err != nil {
		t.Fatalf("expected wip.txt to be restored: %v", err)
	}
	if string(data) != "wip" {
		t.Fatalf("expected restored content %q, got %q", "wip", string(data))
	}
}

func TestStashPop_NoStash_ReturnsError(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	if err := pushq.StashPop(clone.Path); err == nil {
		t.Fatal("expected error when popping with no stash")
	}
}

// --- helpers -----------------------------------------------------------------

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}
