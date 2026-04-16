package pushq

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ezcdlabs/pushq/internal/clock"
	"github.com/ezcdlabs/pushq/internal/gitenv"
	"github.com/ezcdlabs/pushq/internal/queue"
	"github.com/ezcdlabs/pushq/internal/refs"
	"github.com/ezcdlabs/pushq/internal/runner"
	"github.com/ezcdlabs/pushq/internal/stack"
)

type PushOptions struct {
	RepoPath      string
	Remote        string
	MainBranch    string
	TestCommand   string
	CommitMessage string
	Username      string
	// Clock controls the timer used for queue-state polls when entries are
	// ahead. Defaults to the real clock when nil. Inject a fake clock in
	// tests to avoid waiting on real time.
	Clock clock.Clock
}

// Push runs the full pushq lifecycle and streams events on the returned
// channel. The channel is closed after a Done event is sent.
func Push(ctx context.Context, opts PushOptions) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		err := push(ctx, opts, ch)
		ch <- Done{Err: err}
	}()
	return ch
}

func push(ctx context.Context, opts PushOptions, events chan<- Event) error {
	if opts.Remote == "" {
		opts.Remote = "origin"
	}
	if opts.MainBranch == "" {
		opts.MainBranch = "main"
	}
	if opts.Clock == nil {
		opts.Clock = clock.Real()
	}

	events <- PhaseChanged{Phase: PhaseJoining}

	// 1. Squash local commits since main into one commit and push to queue ref.
	entryID := opts.Username + "-" + fmt.Sprintf("%d", time.Now().UnixMilli())
	entryRef := "refs/pushq/" + entryID

	if err := squashAndPushEntryRef(opts.RepoPath, opts.Remote, opts.MainBranch, opts.CommitMessage, entryRef); err != nil {
		return fmt.Errorf("prepare entry ref: %w", err)
	}

	// 2. Join the queue.
	if err := queue.Join(opts.RepoPath, opts.Remote, entryID, entryRef); err != nil {
		_ = refs.DeleteRef(opts.RepoPath, opts.Remote, entryRef)
		return fmt.Errorf("join queue: %w", err)
	}

	// 3. Mark as testing.
	if err := queue.SetStatus(opts.RepoPath, opts.Remote, entryID, "testing"); err != nil {
		_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
		return fmt.Errorf("set status testing: %w", err)
	}

	// 4–7. Test-and-land loop.
	var testedWith []string // entry refs we included in the last passing test run
	tested := false

	for {
		// 4. Read current queue state (entries + landed record).
		entries, landed, err := queue.ReadState(opts.RepoPath, opts.Remote)
		if err != nil {
			_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
			return fmt.Errorf("read state: %w", err)
		}

		// Emit queue state so the TUI left panel stays current.
		events <- QueueStateChanged{Entries: toEntryRecords(entries)}

		var aheadRefs []string
		for _, e := range entries {
			if e.ID == entryID {
				break
			}
			aheadRefs = append(aheadRefs, e.Ref)
		}

		var landedRecord *LandedRecord
		if landed != nil {
			landedRecord = &LandedRecord{Ref: landed.Ref, MainSHA: landed.MainSHA}
		}

		if tested && NeedsRetest(testedWith, landedRecord, aheadRefs) {
			tested = false
		}

		// 5–6. Build and test if we don't have a valid passing result yet.
		if !tested {
			events <- PhaseChanged{Phase: PhaseBuildingStack}

			testStack, err := stack.Build(stack.Options{
				RepoPath:     opts.RepoPath,
				Remote:       opts.Remote,
				MainBranch:   opts.MainBranch,
				OwnRef:       entryRef,
				EntriesAhead: aheadRefs,
			})
			if err != nil {
				_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
				return fmt.Errorf("build test stack: %w", err)
			}

			events <- PhaseChanged{Phase: PhaseTesting}

			lines := make(chan string, 64)
			var linesWg sync.WaitGroup
			linesWg.Add(1)
			go func() {
				defer linesWg.Done()
				for line := range lines {
					events <- LogLine{Text: line}
				}
			}()

			result, runErr := runner.Run(ctx, opts.TestCommand, opts.RepoPath, lines)
			close(lines)
			linesWg.Wait() // ensure all lines are forwarded before Done can be sent
			testStack.Cleanup()

			if runErr != nil {
				_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
				return fmt.Errorf("run tests: %w", runErr)
			}
			if !result.Passed {
				_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
				return fmt.Errorf("tests failed:\n%s", result.Output)
			}

			tested = true
			testedWith = make([]string, len(aheadRefs))
			copy(testedWith, aheadRefs)
		}

		// 7. Tests are passing. Only push to main once no entries are ahead.
		if len(aheadRefs) > 0 {
			events <- PhaseChanged{Phase: PhaseWaiting}
			select {
			case <-ctx.Done():
				_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
				return ctx.Err()
			case <-opts.Clock.After(5 * time.Second):
			}
			continue
		}

		events <- PhaseChanged{Phase: PhaseLanding}

		pushStack, err := stack.Build(stack.Options{
			RepoPath:     opts.RepoPath,
			Remote:       opts.Remote,
			MainBranch:   opts.MainBranch,
			OwnRef:       entryRef,
			EntriesAhead: nil,
		})
		if err != nil {
			_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
			return fmt.Errorf("build push stack: %w", err)
		}

		mainSHA, shaErr := gitRevParse(opts.RepoPath, pushStack.BranchName)
		originSHA, _ := gitRevParse(opts.RepoPath, opts.Remote+"/"+opts.MainBranch)
		if shaErr == nil && mainSHA == originSHA {
			pushStack.Cleanup()
			_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
			return fmt.Errorf("landing stack is identical to %s/%s — entry ref may be empty or already landed", opts.Remote, opts.MainBranch)
		}
		pushErr := refs.PushRef(opts.RepoPath, opts.Remote, pushStack.BranchName, opts.MainBranch)
		pushStack.Cleanup()

		if pushErr == nil {
			if shaErr == nil {
				_ = queue.LandEntry(opts.RepoPath, opts.Remote, entryID, mainSHA)
			} else {
				_ = queue.RemoveEntry(opts.RepoPath, opts.Remote, entryID, "done")
			}
			_ = refs.DeleteRef(opts.RepoPath, opts.Remote, entryRef)
			return nil
		}

		if !isFastForwardRejected(pushErr) {
			_ = eject(opts.RepoPath, opts.Remote, entryID, entryRef)
			return fmt.Errorf("push to main: %w", pushErr)
		}

		// Push rejected — someone else landed between our queue check and push.
		// Loop back to re-read the queue and try again.
	}
}

// toEntryRecords converts internal queue records to the public EntryRecord type.
func toEntryRecords(entries []queue.EntryRecord) []EntryRecord {
	out := make([]EntryRecord, len(entries))
	for i, e := range entries {
		out[i] = EntryRecord{ID: e.ID, Ref: e.Ref, Status: string(e.Status)}
	}
	return out
}

// eject removes the entry from the state branch and deletes the entry ref.
func eject(repoPath, remote, entryID, entryRef string) error {
	_ = queue.RemoveEntry(repoPath, remote, entryID, "eject")
	return refs.DeleteRef(repoPath, remote, entryRef)
}

// squashAndPushEntryRef squashes all commits since main into a single commit
// rebased on main, then pushes it to entryRef on the remote.
func squashAndPushEntryRef(repoPath, remote, mainBranch, message, entryRef string) error {
	if err := git(repoPath, "fetch", remote, mainBranch); err != nil {
		return fmt.Errorf("fetch main: %w", err)
	}
	remoteMain := remote + "/" + mainBranch

	tmpBranch := "pushq-squash-tmp"
	_ = git(repoPath, "branch", "-D", tmpBranch)

	if err := git(repoPath, "checkout", "-b", tmpBranch, remoteMain); err != nil {
		return fmt.Errorf("create squash branch: %w", err)
	}

	if err := git(repoPath, "merge", "--squash", "HEAD@{1}"); err != nil {
		_ = git(repoPath, "checkout", "-")
		_ = git(repoPath, "branch", "-D", tmpBranch)
		return fmt.Errorf("squash merge: %w", err)
	}

	if err := gitCommit(repoPath, message); err != nil {
		_ = git(repoPath, "checkout", "-")
		_ = git(repoPath, "branch", "-D", tmpBranch)
		return fmt.Errorf("commit squash: %w", err)
	}

	if err := refs.PushRef(repoPath, remote, "HEAD", entryRef); err != nil {
		_ = git(repoPath, "checkout", "-")
		_ = git(repoPath, "branch", "-D", tmpBranch)
		return fmt.Errorf("push entry ref: %w", err)
	}

	if err := git(repoPath, "update-ref", entryRef, "HEAD"); err != nil {
		_ = git(repoPath, "checkout", "-")
		_ = git(repoPath, "branch", "-D", tmpBranch)
		return fmt.Errorf("set local entry ref: %w", err)
	}

	_ = git(repoPath, "checkout", "-")
	_ = git(repoPath, "branch", "-D", tmpBranch)
	return nil
}

func gitRevParse(repoPath, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = repoPath
	cmd.Env = gitenv.Clean()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isFastForwardRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "failed to update ref") ||
		strings.Contains(msg, "rejected")
}

func git(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = gitenv.Clean()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func gitCommit(repoPath, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	cmd.Env = append(gitenv.Clean(),
		"GIT_AUTHOR_NAME=pushq",
		"GIT_AUTHOR_EMAIL=pushq@local",
		"GIT_COMMITTER_NAME=pushq",
		"GIT_COMMITTER_EMAIL=pushq@local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}
	return nil
}
