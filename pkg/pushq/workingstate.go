package pushq

import (
	"fmt"
	"os/exec"
	"strings"
)

// HasUncommittedChanges returns true if the working tree has any uncommitted
// changes — including unstaged modifications, staged changes, and untracked
// files. Untracked files are included because Stash uses -u.
func HasUncommittedChanges(repoPath string) (bool, error) {
	// --porcelain=v1 -u prints one line per changed/untracked file.
	cmd := exec.Command("git", "status", "--porcelain=v1", "-u")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// Stash saves all uncommitted changes (including untracked files) to the git
// stash, leaving the working tree clean.
func Stash(repoPath string) error {
	if err := git(repoPath, "stash", "push", "-u", "--message", "pushq: stashed before push"); err != nil {
		return fmt.Errorf("stash: %w", err)
	}
	return nil
}

// StashPop restores the most recently stashed changes. Returns an error if
// there is nothing to pop.
func StashPop(repoPath string) error {
	if err := git(repoPath, "stash", "pop"); err != nil {
		return fmt.Errorf("stash pop: %w", err)
	}
	return nil
}
