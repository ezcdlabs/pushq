package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ezcdlabs/pushq/config"
	"github.com/ezcdlabs/pushq/pkg/pushq"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Find repo root.
	repoPath, err := repoRoot()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	// 2. Load config.
	cfg, err := config.Load(repoPath)
	if err != nil {
		return fmt.Errorf("could not load .pushq.json: %w", err)
	}

	// 3. Check for pending commits — nothing to do if we're already up to date.
	pending, err := pushq.ListPendingCommits(repoPath, "origin", cfg.MainBranch)
	if err != nil {
		return fmt.Errorf("list pending commits: %w", err)
	}
	if len(pending) == 0 {
		return fmt.Errorf("nothing to push — no commits ahead of %s", cfg.MainBranch)
	}

	// 4. Stash uncommitted changes so the working tree is clean for the test run.
	stashed := false
	dirty, err := pushq.HasUncommittedChanges(repoPath)
	if err != nil {
		return fmt.Errorf("check working tree: %w", err)
	}
	if dirty {
		fmt.Println("Stashing uncommitted changes...")
		if err := pushq.Stash(repoPath); err != nil {
			return fmt.Errorf("stash: %w", err)
		}
		stashed = true
	}

	// 5. Show commits and prompt for a single squash message.
	fmt.Println("\nCommits to push:")
	for _, c := range pending {
		fmt.Printf("  %s  %s\n", c.Hash[:8], c.Subject)
	}
	defaultMsg := pending[len(pending)-1].Subject
	fmt.Printf("\nCommit message [%s]: ", defaultMsg)
	message, err := readLine()
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		message = defaultMsg
	}

	// 6. Derive a URL-safe username from git config.
	username, err := gitConfigValue(repoPath, "user.name")
	if err != nil || username == "" {
		username = "dev"
	}
	username = sanitizeUsername(username)

	// 7. Run the TUI.
	ctx, cancel := context.WithCancel(context.Background())
	session := &realSession{
		ctx:    ctx,
		cancel: cancel,
		opts: pushq.PushOptions{
			RepoPath:      repoPath,
			Remote:        "origin",
			MainBranch:    cfg.MainBranch,
			TestCommand:   cfg.TestCommand,
			CommitMessage: message,
			Username:      username,
		},
	}

	p := tea.NewProgram(initialModel(session), tea.WithAltScreen())
	finalRaw, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	final := finalRaw.(model)

	// 8. Post-TUI output — printed to the normal terminal after alt screen exits.
	if final.err != nil {
		if len(final.logLines) > 0 {
			fmt.Fprintln(os.Stderr, "\n--- test output ---")
			for _, line := range final.logLines {
				fmt.Fprintln(os.Stderr, line)
			}
			fmt.Fprintln(os.Stderr, "---")
		}
		fmt.Fprintf(os.Stderr, "\nfailed: %v\n", final.err)
		if stashed {
			fmt.Fprintln(os.Stderr, "Your changes are still stashed. Run 'git stash pop' to restore.")
		}
		return final.err
	}

	fmt.Println("\nlanded.")
	if stashed {
		fmt.Println("Restoring stashed changes...")
		if err := pushq.StashPop(repoPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: stash pop failed: %v\nRun 'git stash pop' manually.\n", err)
		}
	}
	return nil
}

// realSession implements PushSession using pkg/pushq.
type realSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   pushq.PushOptions
}

func (s *realSession) Start() <-chan pushq.Event {
	return pushq.Push(s.ctx, s.opts)
}

func (s *realSession) Cancel() {
	s.cancel()
}

// --- helpers -----------------------------------------------------------------

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitConfigValue(repoPath, key string) (string, error) {
	cmd := exec.Command("git", "config", key)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sanitizeUsername converts a git display name into a lowercase hyphenated
// string safe for use as a queue entry ID prefix.
func sanitizeUsername(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "dev"
	}
	return s
}

func readLine() (string, error) {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}
