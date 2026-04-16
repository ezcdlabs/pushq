package stack

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ezcdlabs/pushq/internal/gitenv"
)

// Options configures a stack build.
type Options struct {
	RepoPath     string
	Remote       string
	MainBranch   string
	OwnRef       string
	EntriesAhead []string
}

// Result is the outcome of a successful Build. Call Cleanup when done testing.
type Result struct {
	BranchName string
	repoPath   string
	remote     string
	mainBranch string
}

// Cleanup deletes the temporary test branch from the local repo.
func (r *Result) Cleanup() {
	// Detach HEAD to origin/main so we can delete the test branch regardless
	// of local branch state.
	_ = git(r.repoPath, "checkout", "--detach", r.remote+"/"+r.mainBranch)
	_ = git(r.repoPath, "branch", "-D", r.BranchName)
}

// Build creates a local pushq-test-branch by cherry-picking entries ahead (in
// order, skipping conflicts) then the developer's own ref, all on top of main.
func Build(opts Options) (*Result, error) {
	branchName := "pushq-test-branch"

	// Ensure the remote tracking branch is current.
	_ = git(opts.RepoPath, "fetch", opts.Remote, opts.MainBranch)

	// Delete any stale test branch, then create it from the remote tracking
	// branch — not local main, which may have unpushed commits.
	_ = git(opts.RepoPath, "branch", "-D", branchName)
	remoteMain := opts.Remote + "/" + opts.MainBranch
	if err := git(opts.RepoPath, "checkout", "-b", branchName, remoteMain); err != nil {
		return nil, fmt.Errorf("create test branch from %s: %w", remoteMain, err)
	}

	// Cherry-pick each entry ahead, skipping conflicts.
	for _, ref := range opts.EntriesAhead {
		localRef := "refs/pushq-stack-fetch/" + sanitiseRef(ref)
		_ = git(opts.RepoPath, "fetch", opts.Remote, ref+":"+localRef)
		if err := cherryPick(opts.RepoPath, localRef); err != nil {
			// Abort the failed cherry-pick and continue without this entry.
			_ = git(opts.RepoPath, "cherry-pick", "--abort")
		}
	}

	// Fetch and cherry-pick own ref — this must succeed.
	ownLocalRef := "refs/pushq-stack-fetch/" + sanitiseRef(opts.OwnRef)
	_ = git(opts.RepoPath, "fetch", opts.Remote, opts.OwnRef+":"+ownLocalRef)
	if err := cherryPick(opts.RepoPath, ownLocalRef); err != nil {
		_ = git(opts.RepoPath, "cherry-pick", "--abort")
		_ = git(opts.RepoPath, "checkout", opts.MainBranch)
		_ = git(opts.RepoPath, "branch", "-D", branchName)
		return nil, fmt.Errorf("cherry-pick own ref %s: %w", opts.OwnRef, err)
	}

	return &Result{BranchName: branchName, repoPath: opts.RepoPath, remote: opts.Remote, mainBranch: opts.MainBranch}, nil
}

func cherryPick(repoPath, ref string) error {
	return git(repoPath, "cherry-pick", ref)
}

// sanitiseRef converts a ref name into a safe path component for use as a
// local fetch target (e.g. "refs/pushq/alice-100" → "alice-100").
func sanitiseRef(ref string) string {
	return strings.ReplaceAll(ref, "/", "-")
}

func git(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = gitenv.Clean()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}
