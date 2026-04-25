package pushq_test

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ezcdlabs/pushq/internal/clock"
	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/internal/queue"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

func gitRevParseTest(t *testing.T, repoPath, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

// testClock creates a Fake clock and starts a background driver goroutine
// that calls Advance(5s) whenever a timer is registered. This means any
// wait-poll loop in the system under test unblocks immediately rather than
// sleeping for real time. The driver stops when the test ends.
func testClock(t *testing.T) *clock.Fake {
	t.Helper()
	fake := clock.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-fake.TimerAdded():
				fake.Advance(5 * time.Second)
			}
		}
	}()
	return fake
}

// withClock returns a copy of opts with Clock set to clk.
func withClock(opts pushq.PushOptions, clk clock.Clock) pushq.PushOptions {
	opts.Clock = clk
	return opts
}

// pushAndWait runs Push and blocks until Done, returning the error from Done.
// Used by tests that only care about the outcome, not the event stream.
func pushAndWait(ctx context.Context, opts pushq.PushOptions) error {
	for ev := range pushq.Push(ctx, opts) {
		if d, ok := ev.(pushq.Done); ok {
			return d.Err
		}
	}
	return nil
}

func alicePushOpts(repoPath string) pushq.PushOptions {
	return pushq.PushOptions{
		RepoPath:      repoPath,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "alice's feature",
		Username:      "alice",
	}
}

func bobPushOpts(repoPath string) pushq.PushOptions {
	return pushq.PushOptions{
		RepoPath:      repoPath,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "bob's feature",
		Username:      "bob",
	}
}

// TestPush_SingleDeveloper_TestsPass_LandsOnMain is the simplest meaningful
// acceptance test: one developer, empty queue, tests pass, commit lands on main
// and all queue refs are cleaned up.
func TestPush_SingleDeveloper_TestsPass_LandsOnMain(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "new feature")
	clone.CommitAll("add feature")

	err := pushAndWait(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",

	})
	if err != nil {
		t.Fatalf("Push() returned unexpected error: %v", err)
	}

	// The squashed commit must be the new tip of main on the remote.
	commits := remote.LogBranch("main")
	if len(commits) == 0 {
		t.Fatal("expected commits on main after push")
	}
	if commits[0].Message != "add feature" {
		t.Fatalf("expected top commit %q on main, got %q", "add feature", commits[0].Message)
	}

	// No entry ref (refs/pushq/<entry-id>) should remain — only the
	// state branch is allowed to persist.
	for _, ref := range remote.ListRefs() {
		if strings.HasPrefix(ref, "refs/pushq/") && ref != "refs/pushq/state" {
			t.Fatalf("expected no entry refs after completion, found: %s", ref)
		}
	}

	// The entry file must be absent from the state branch tree — done entries
	// are deleted from the active tree (the commit history is the log).
	refs := remote.ListRefs()
	stateExists := false
	for _, r := range refs {
		if r == "refs/pushq/state" {
			stateExists = true
			break
		}
	}
	if stateExists {
		// If the state branch exists there must be no active entries — alice's
		// entry file should have been removed on landing. Use the queue API rather
		// than ReadFileAtRef, because the nested-tree layout means "entries" now
		// resolves as a directory and would always appear to exist.
		entries, _, err := queue.ReadState(clone.Path, "origin")
		if err != nil {
			t.Fatalf("ReadState failed: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no active entries in state branch after completion, got: %v", entries)
		}
	}

	// Local main must be advanced to origin/main after landing — the squash
	// commit replaces the individual commits, so the local branch must not
	// diverge from the remote.
	localMain := gitRevParseTest(t, clone.Path, "main")
	originMain := gitRevParseTest(t, clone.Path, "origin/main")
	if localMain != originMain {
		t.Fatalf("local main (%s) diverged from origin/main (%s) after push — local branch not advanced", localMain[:8], originMain[:8])
	}
}

// TestPush_SingleDeveloper_TestsFail_EjectsFromQueue verifies that when tests
// fail the developer's entry is removed from the queue and the error is
// surfaced to the caller.
func TestPush_SingleDeveloper_TestsFail_EjectsFromQueue(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "new feature")
	clone.CommitAll("add feature")

	err := pushAndWait(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.FailingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",

	})
	if err == nil {
		t.Fatal("Push() should return an error when tests fail")
	}

	// The commit must NOT have landed on main.
	commits := remote.LogBranch("main")
	if commits[0].Message == "add feature" {
		t.Fatal("commit should not land on main when tests fail")
	}

	// All queue refs must be cleaned up even on failure.
	for _, ref := range remote.ListRefs() {
		if strings.HasPrefix(ref, "refs/pushq/") && ref != "refs/pushq/state" {
			t.Fatalf("expected no entry refs after ejection, found: %s", ref)
		}
	}
}

// TestPush_TwoDevelopers_Sequential_BothLand verifies that two developers
// pushing one after the other both land on main in order.
func TestPush_TwoDevelopers_Sequential_BothLand(t *testing.T) {
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	// Alice pushes and lands first.
	if err := pushAndWait(context.Background(), alicePushOpts(aliceClone.Path)); err != nil {
		t.Fatalf("alice's push failed: %v", err)
	}

	// Bob commits independently and pushes after alice has already landed.
	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	if err := pushAndWait(context.Background(), bobPushOpts(bobClone.Path)); err != nil {
		t.Fatalf("bob's push failed: %v", err)
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	hasAlice, hasBob := false, false
	for _, m := range messages {
		if m == "alice's feature" {
			hasAlice = true
		}
		if m == "bob's feature" {
			hasBob = true
		}
	}
	if !hasAlice || !hasBob {
		t.Fatalf("expected both alice's and bob's commits on main, got: %v", messages)
	}
}

// TestPush_TwoDevelopers_Concurrent_BothLand verifies that two developers
// pushing at the same time both eventually land on main.
func TestPush_TwoDevelopers_Concurrent_BothLand(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	type result struct{ err error }
	ch := make(chan result, 2)

	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(alicePushOpts(aliceClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(bobPushOpts(bobClone.Path), clk))}
	}()

	r1, r2 := <-ch, <-ch
	if r1.err != nil {
		t.Fatalf("first concurrent push failed: %v", r1.err)
	}
	if r2.err != nil {
		t.Fatalf("second concurrent push failed: %v", r2.err)
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	hasAlice, hasBob := false, false
	for _, m := range messages {
		if m == "alice's feature" {
			hasAlice = true
		}
		if m == "bob's feature" {
			hasBob = true
		}
	}
	if !hasAlice || !hasBob {
		t.Fatalf("expected both commits on main after concurrent push, got: %v", messages)
	}
}

// TestPush_TwoDevelopers_FirstEjected_SecondLands verifies that when the first
// developer's tests fail and they eject, the second developer rebuilds their
// stack without the ejected entry and still lands on main.
func TestPush_TwoDevelopers_FirstEjected_SecondLands(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	// Alice and bob both push concurrently; alice's tests are configured to fail.
	type result struct{ err error }
	ch := make(chan result, 2)

	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(pushq.PushOptions{
			RepoPath:      aliceClone.Path,
			Remote:        "origin",
			MainBranch:    "main",
			TestCommand:   gittest.FailingTestCommand(),
			CommitMessage: "alice's feature",
			Username:      "alice",
		}, clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(bobPushOpts(bobClone.Path), clk))}
	}()

	var aliceErr, bobErr error
	for range 2 {
		r := <-ch
		if r.err != nil {
			aliceErr = r.err
		} else {
			bobErr = r.err
		}
	}

	if aliceErr == nil {
		t.Fatal("alice's push should have failed (tests configured to fail)")
	}
	if bobErr != nil {
		t.Fatalf("bob's push should succeed after alice ejects, got: %v", bobErr)
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	for _, m := range messages {
		if m == "alice's feature" {
			t.Fatal("alice's commit should not be on main after ejection")
		}
	}
	hasBob := false
	for _, m := range messages {
		if m == "bob's feature" {
			hasBob = true
			break
		}
	}
	if !hasBob {
		t.Fatalf("expected bob's commit on main, got: %v", messages)
	}
}

// TestPush_ThreeDevelopers_Concurrent_AllLand verifies that three developers
// pushing simultaneously all land on main. Each developer may test against
// different combinations of entries ahead, and may need to retest as earlier
// entries resolve — but all three must eventually land.
func TestPush_ThreeDevelopers_Concurrent_AllLand(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)
	carolClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	carolClone.WriteFile("carol.txt", "carol's work")
	carolClone.CommitAll("carol's feature")

	type result struct{ err error }
	ch := make(chan result, 3)

	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(alicePushOpts(aliceClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(bobPushOpts(bobClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(pushq.PushOptions{
			RepoPath:      carolClone.Path,
			Remote:        "origin",
			MainBranch:    "main",
			TestCommand:   gittest.PassingTestCommand(),
			CommitMessage: "carol's feature",
			Username:      "carol",
		}, clk))}
	}()

	for range 3 {
		if r := <-ch; r.err != nil {
			t.Fatalf("a concurrent push failed: %v", r.err)
		}
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	hasAlice, hasBob, hasCarol := false, false, false
	for _, m := range messages {
		switch m {
		case "alice's feature":
			hasAlice = true
		case "bob's feature":
			hasBob = true
		case "carol's feature":
			hasCarol = true
		}
	}
	if !hasAlice || !hasBob || !hasCarol {
		t.Fatalf("expected all three commits on main, got: %v", messages)
	}
}

// TestPush_ConflictingChanges_OneLandsOneEjects verifies that when two
// developers modify the same file, the one who joins first lands and the
// other — whose cherry-pick onto the first's changes would conflict — ejects.
func TestPush_ConflictingChanges_OneLandsOneEjects(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)

	// Both edit the same file — a guaranteed cherry-pick conflict for whoever
	// ends up second in the queue.
	aliceClone.WriteFile("shared.txt", "alice's version")
	aliceClone.CommitAll("alice edits shared.txt")

	bobClone.WriteFile("shared.txt", "bob's version")
	bobClone.CommitAll("bob edits shared.txt")

	type result struct{ err error }
	ch := make(chan result, 2)

	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(alicePushOpts(aliceClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(bobPushOpts(bobClone.Path), clk))}
	}()

	r1, r2 := <-ch, <-ch

	// Exactly one should succeed and one should fail.
	errs := 0
	if r1.err != nil {
		errs++
	}
	if r2.err != nil {
		errs++
	}
	if errs != 1 {
		t.Fatalf("expected exactly one failure from conflicting pushes, got %d errors: %v / %v", errs, r1.err, r2.err)
	}

	// Exactly one commit (either alice's or bob's) should be on main.
	commits := remote.LogBranch("main")
	landed := 0
	for _, c := range commits {
		if c.Message == "alice's feature" || c.Message == "bob's feature" {
			landed++
		}
	}
	if landed != 1 {
		t.Fatalf("expected exactly one of alice/bob on main, found %d in: %v", landed, commits)
	}
}

// TestPush_EntryAboveLands_NoRetest verifies that when the developer ahead of
// you lands on main, your passing test result is NOT invalidated — you should
// land without running your tests a second time.
//
// This is the key property of the NeedsRetest algorithm: landings are covered
// by the landing chain and do not require a retest. Only ejections do.
//
// We detect an unwanted retest by giving carol a "run-once" test command: it
// succeeds on the first invocation but fails on any subsequent one. If the
// implementation incorrectly retests after alice lands, carol's second test run
// will fail and the push will return an error.
func TestPush_EntryAboveLands_NoRetest(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	carolClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	carolClone.WriteFile("carol.txt", "carol's work")
	carolClone.CommitAll("carol's feature")

	// carol's test command succeeds once; a second invocation fails.
	carolFlag := filepath.Join(t.TempDir(), "carol-test-ran")

	type result struct{ err error }
	ch := make(chan result, 2)

	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(alicePushOpts(aliceClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(pushq.PushOptions{
			RepoPath:      carolClone.Path,
			Remote:        "origin",
			MainBranch:    "main",
			TestCommand:   gittest.RunOnceTestCommand(carolFlag),
			CommitMessage: "carol's feature",
			Username:      "carol",
		}, clk))}
	}()

	r1, r2 := <-ch, <-ch
	if r1.err != nil {
		t.Fatalf("first push failed: %v", r1.err)
	}
	if r2.err != nil {
		t.Fatalf("second push failed (carol may have retested after alice landed): %v", r2.err)
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	hasAlice, hasCarol := false, false
	for _, m := range messages {
		switch m {
		case "alice's feature":
			hasAlice = true
		case "carol's feature":
			hasCarol = true
		}
	}
	if !hasAlice || !hasCarol {
		t.Fatalf("expected both alice and carol on main, got: %v", messages)
	}
}

// TestPush_MiddleDeveloperEjects_OthersLand verifies that when a developer in
// the middle of the queue ejects (tests fail), the developers behind them
// rebuild their stacks, retest, and still land on main.
func TestPush_MiddleDeveloperEjects_OthersLand(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)
	carolClone := remote.NewClone(t)

	aliceClone.WriteFile("alice.txt", "alice's work")
	aliceClone.CommitAll("alice's feature")

	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	carolClone.WriteFile("carol.txt", "carol's work")
	carolClone.CommitAll("carol's feature")

	type result struct{ err error }
	ch := make(chan result, 3)

	// Alice and carol have passing tests; bob's tests fail.
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(alicePushOpts(aliceClone.Path), clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(pushq.PushOptions{
			RepoPath:      bobClone.Path,
			Remote:        "origin",
			MainBranch:    "main",
			TestCommand:   gittest.FailingTestCommand(),
			CommitMessage: "bob's feature",
			Username:      "bob",
		}, clk))}
	}()
	go func() {
		ch <- result{pushAndWait(context.Background(), withClock(pushq.PushOptions{
			RepoPath:      carolClone.Path,
			Remote:        "origin",
			MainBranch:    "main",
			TestCommand:   gittest.PassingTestCommand(),
			CommitMessage: "carol's feature",
			Username:      "carol",
		}, clk))}
	}()

	var bobErr error
	var otherErrs []error
	for range 3 {
		r := <-ch
		if r.err != nil {
			// We expect exactly one error (bob's). Accumulate to distinguish.
			if bobErr == nil {
				bobErr = r.err
			} else {
				otherErrs = append(otherErrs, r.err)
			}
		}
	}

	if bobErr == nil {
		t.Fatal("expected bob's push to fail (tests configured to fail)")
	}
	if len(otherErrs) > 0 {
		t.Fatalf("expected only bob to fail, got additional errors: %v", otherErrs)
	}

	commits := remote.LogBranch("main")
	messages := make([]string, len(commits))
	for i, c := range commits {
		messages[i] = c.Message
	}

	for _, m := range messages {
		if m == "bob's feature" {
			t.Fatal("bob's commit should not be on main after ejection")
		}
	}

	hasAlice, hasCarol := false, false
	for _, m := range messages {
		switch m {
		case "alice's feature":
			hasAlice = true
		case "carol's feature":
			hasCarol = true
		}
	}
	if !hasAlice || !hasCarol {
		t.Fatalf("expected alice's and carol's commits on main, got: %v", messages)
	}
}

// TestPush_GITDIRInEnvironment_IsIgnored verifies that a poisoned GIT_DIR in
// the process environment does not break the push. When git invokes an external
// command (e.g. "git pushq"), it sets GIT_DIR to its own .git directory. Every
// subprocess git call we make would inherit that and operate on the wrong repo
// unless we explicitly strip it.
func TestPush_GITDIRInEnvironment_IsIgnored(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)
	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	// Set GIT_DIR after repo setup so that gittest helpers are not affected.
	// This simulates git setting GIT_DIR in the environment before invoking
	// "git pushq" as an external subcommand.
	t.Setenv("GIT_DIR", "/this-path-does-not-exist")

	err := pushAndWait(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",

	})
	if err != nil {
		t.Fatalf("push failed with poisoned GIT_DIR in environment: %v", err)
	}
}

// TestPush_ContextCancelledWhileWaiting_EjectsEntry verifies that cancelling
// the context while a developer is waiting for entries ahead causes the entry
// to be ejected from the queue and context.Canceled to be returned.
func TestPush_ContextCancelledWhileWaiting_EjectsEntry(t *testing.T) {
	clk := testClock(t)
	remote := gittest.NewRemote(t)
	aliceClone := remote.NewClone(t)
	bobClone := remote.NewClone(t)

	// Add alice to the queue state manually so bob will be behind her.
	// Her entryRef does not need to be a real ref on the remote — stack.Build
	// silently skips refs that cannot be fetched, so bob still tests and passes,
	// then enters PhaseWaiting because alice is ahead of him in the state branch.
	if err := queue.Join(aliceClone.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
		t.Fatalf("queue.Join alice: %v", err)
	}

	bobClone.WriteFile("bob.txt", "bob's work")
	bobClone.CommitAll("bob's feature")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var finalErr error
	cancelledOnce := false
	for ev := range pushq.Push(ctx, withClock(bobPushOpts(bobClone.Path), clk)) {
		if ph, ok := ev.(pushq.PhaseChanged); ok && ph.Phase == pushq.PhaseWaiting && !cancelledOnce {
			cancel()
			cancelledOnce = true
		}
		if d, ok := ev.(pushq.Done); ok {
			finalErr = d.Err
		}
	}

	if !cancelledOnce {
		t.Fatal("bob never reached PhaseWaiting — test setup may be wrong")
	}
	if !errors.Is(finalErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", finalErr)
	}

	// Bob's entry must have been ejected from the queue.
	entries, err := queue.ListEntries(bobClone.Path, "origin")
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.ID, "bob-") {
			t.Fatalf("bob's entry should have been ejected after cancellation, but found: %v", e)
		}
	}
}

func TestPush_QueueStateChanged_LandedPopulatedFromRemoteHead(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	clone.WriteFile("feature.txt", "hello")
	clone.CommitAll("add feature")

	var firstLanded string
	for ev := range pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      clone.Path,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   gittest.PassingTestCommand(),
		CommitMessage: "add feature",
		Username:      "alice",
	}) {
		if qsc, ok := ev.(pushq.QueueStateChanged); ok && firstLanded == "" {
			firstLanded = qsc.Landed
		}
	}

	if firstLanded == "" {
		t.Fatal("expected QueueStateChanged.Landed to be non-empty")
	}
	if !strings.Contains(firstLanded, "initial commit") {
		t.Errorf("expected Landed to contain remote HEAD subject %q, got: %q", "initial commit", firstLanded)
	}
}
