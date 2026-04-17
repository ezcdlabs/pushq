//go:build integration

// Package integration contains smoke tests that run pushq against a live SSH
// git server (the "server" Docker Compose service) to catch failures that only
// manifest with a real network remote, such as SSH auth errors and GitHub's
// receive.fsckObjects rejections.
//
// Run with:
//
//	docker compose -f test/integration/docker-compose.yml run --rm client
//
// Or for a manual shell inside the client container:
//
//	docker compose -f test/integration/docker-compose.yml run --rm client bash
package integration_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ezcdlabs/pushq/pkg/pushq"
)

// serverHost returns the hostname of the SSH git server from the environment.
// The test is skipped if the variable is not set (i.e. running outside Docker).
func serverHost(t *testing.T) string {
	t.Helper()
	h := os.Getenv("PUSHQ_SSH_SERVER")
	if h == "" {
		t.Skip("PUSHQ_SSH_SERVER not set — run inside the Docker Compose client container")
	}
	return h
}

// cloneRepo clones git@host:/home/git/repo.git into a temp directory.
func cloneRepo(t *testing.T, host string) string {
	t.Helper()
	dir := t.TempDir()
	remoteURL := fmt.Sprintf("git@%s:/home/git/repo.git", host)
	run(t, "", "git", "clone", remoteURL, dir)
	run(t, dir, "git", "config", "user.email", "test@pushq")
	run(t, dir, "git", "config", "user.name", "Test")
	return dir
}

// writePushqConfig writes .pushq.json into repoPath.
func writePushqConfig(t *testing.T, repoPath, testCommand string) {
	t.Helper()
	cfg, _ := json.Marshal(map[string]string{
		"test_command": testCommand,
		"main_branch":  "main",
	})
	if err := os.WriteFile(filepath.Join(repoPath, ".pushq.json"), cfg, 0644); err != nil {
		t.Fatalf("write .pushq.json: %v", err)
	}
}

// TestSSHPush_LandsOnMain is the end-to-end smoke test:
//   - real SSH remote with receive.fsckObjects=true (matching GitHub's config)
//   - one commit, passing tests, verify it lands on main
//   - verify local main is advanced (no divergence)
//
// We call pushq.Push() directly rather than the binary to avoid the TUI's
// /dev/tty requirement in headless containers. The SSH calls go through the
// same git CLI subprocess path that the binary uses.
func TestSSHPush_LandsOnMain(t *testing.T) {
	host := serverHost(t)
	repoPath := cloneRepo(t, host)

	// Use a unique filename so each run adds a new commit even if the server
	// already has files from a previous run (the server container persists).
	uniqueFile := fmt.Sprintf("feature-%d.txt", time.Now().UnixNano())
	writePushqConfig(t, repoPath, "true")
	writeFile(t, repoPath, uniqueFile, "hello from integration test")
	run(t, repoPath, "git", "add", ".")
	run(t, repoPath, "git", "commit", "-m", "integration test feature")

	ch := pushq.Push(context.Background(), pushq.PushOptions{
		RepoPath:      repoPath,
		Remote:        "origin",
		MainBranch:    "main",
		TestCommand:   "true",
		CommitMessage: "integration test feature",
		Username:      "test",
	})

	for ev := range ch {
		t.Logf("event: %T %+v", ev, ev)
		if d, ok := ev.(pushq.Done); ok && d.Err != nil {
			t.Fatalf("Push failed: %v", d.Err)
		}
	}

	// The squashed commit must be on origin/main.
	log := runOutput(t, repoPath, "git", "log", "origin/main", "--oneline", "-3")
	if !strings.Contains(log, "integration test feature") {
		t.Fatalf("expected 'integration test feature' on origin/main, got:\n%s", log)
	}

	// Local main must not diverge from origin/main.
	localSHA := strings.TrimSpace(runOutput(t, repoPath, "git", "rev-parse", "main"))
	originSHA := strings.TrimSpace(runOutput(t, repoPath, "git", "rev-parse", "origin/main"))
	if localSHA != originSHA {
		t.Fatalf("local main (%s) diverged from origin/main (%s)", localSHA[:8], originSHA[:8])
	}
}

// TestSSHPush_ReceiveFsckObjects_RejectsInvalidTree verifies that the server has
// receive.fsckObjects enabled — this is the setting that caused the production
// failure on GitHub. We push a tree with a slash in an entry name and expect
// rejection with a "fullPathname" fsck error.
func TestSSHPush_ReceiveFsckObjects_RejectsInvalidTree(t *testing.T) {
	host := serverHost(t)
	repoPath := cloneRepo(t, host)

	// Create a blob, then build a raw tree object with a slash in the entry
	// name — exactly what the old pushq code produced ("entries/alice.json"
	// as a flat name). git mktree validates entry names and rejects slashes,
	// so we write the raw tree binary directly via hash-object --literally.
	blobHash := strings.TrimSpace(runOutputStdin(t, repoPath, "test data\n",
		"git", "hash-object", "-w", "--stdin"))

	blobHashBytes, err := hex.DecodeString(blobHash)
	if err != nil {
		t.Fatalf("decode blob hash: %v", err)
	}
	// Raw git tree entry: "<mode> <name>\0<20-byte-binary-sha>"
	var rawTree []byte
	rawTree = append(rawTree, []byte("100644 entries/bad.json\x00")...)
	rawTree = append(rawTree, blobHashBytes...)
	treeHash := strings.TrimSpace(runOutputStdin(t, repoPath, string(rawTree),
		"git", "hash-object", "--literally", "-t", "tree", "-w", "--stdin"))

	// Build a commit on top of origin/main using the broken tree.
	parentHash := strings.TrimSpace(runOutput(t, repoPath, "git", "rev-parse", "origin/main"))
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-p", parentHash, "-m", "bad tree")
	commitCmd.Dir = repoPath
	commitCmd.Env = env
	commitOut, err := commitCmd.Output()
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	badCommit := strings.TrimSpace(string(commitOut))

	// Push to a probe ref — must be rejected by receive.fsckObjects.
	pushCmd := exec.Command("git", "push", "origin", badCommit+":refs/heads/probe-fsck")
	pushCmd.Dir = repoPath
	out, err := pushCmd.CombinedOutput()
	t.Logf("push output:\n%s", out)
	if err == nil {
		t.Fatal("expected push to be rejected by receive.fsckObjects, but it succeeded")
	}
	if !strings.Contains(string(out), "fullPathname") && !strings.Contains(string(out), "fsck") {
		t.Fatalf("push failed but not with an fsck error; got:\n%s", out)
	}
}

// --- helpers -----------------------------------------------------------------

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func runOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return string(out)
}

func runOutputStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return string(out)
}

func writeFile(t *testing.T, base, relPath, content string) {
	t.Helper()
	full := filepath.Join(base, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}
