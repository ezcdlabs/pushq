package pushq

import (
	"fmt"
	"os/exec"
	"strings"
)

// PendingCommit is a commit that exists locally but has not yet landed on main.
type PendingCommit struct {
	Hash    string // full 40-char SHA
	Subject string // first line of commit message
	Author  string // author name
}

// ListPendingCommits returns the commits on the local branch that are ahead of
// remote/mainBranch, in oldest-first order. Returns an empty slice (no error)
// if there is nothing to push.
func ListPendingCommits(repoPath, remote, mainBranch string) ([]PendingCommit, error) {
	if err := git(repoPath, "fetch", remote, mainBranch); err != nil {
		return nil, fmt.Errorf("fetch %s/%s: %w", remote, mainBranch, err)
	}

	base := remote + "/" + mainBranch
	// %x00 is a NUL byte — safe delimiter since it can't appear in git output fields.
	cmd := exec.Command("git", "log", base+"..HEAD", "--format=%H%x00%an%x00%s", "--reverse")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	var commits []PendingCommit
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		commits = append(commits, PendingCommit{
			Hash:    parts[0],
			Author:  parts[1],
			Subject: parts[2],
		})
	}
	return commits, nil
}
