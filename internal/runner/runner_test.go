package runner_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/ezcdlabs/pushq/internal/runner"
)

func passingCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 0"
	}
	return "true"
}

func failingCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 1"
	}
	return "false"
}

func echoCommand(msg string) string {
	if runtime.GOOS == "windows" {
		return "cmd /c echo " + msg
	}
	return "echo " + msg
}

func TestRun_PassingCommand_ReturnsPassed(t *testing.T) {
	result, err := runner.Run(context.Background(), passingCommand(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected Passed=true for a zero-exit command")
	}
}

func TestRun_FailingCommand_ReturnsNotPassed(t *testing.T) {
	result, err := runner.Run(context.Background(), failingCommand(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected Passed=false for a non-zero-exit command")
	}
}

func TestRun_CapturesOutput(t *testing.T) {
	result, err := runner.Run(context.Background(), echoCommand("hello"), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("expected output to contain %q, got: %q", "hello", result.Output)
	}
}

func TestRun_RespectsWorkDir(t *testing.T) {
	dir := t.TempDir()
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cmd /c cd"
	} else {
		cmd = "pwd"
	}

	result, err := runner.Run(context.Background(), cmd, dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, dir) {
		t.Fatalf("expected output to contain workdir %q, got: %q", dir, result.Output)
	}
}

func TestRun_ContextCancelled_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cmd /c timeout 10"
	} else {
		cmd = "sleep 10"
	}

	_, err := runner.Run(ctx, cmd, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected an error when context is cancelled")
	}
}

func TestRun_StreamsLinesToChannel(t *testing.T) {
	lines := make(chan string, 16)
	_, err := runner.Run(context.Background(), echoCommand("streamed-line"), t.TempDir(), lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(lines)

	var got []string
	for l := range lines {
		got = append(got, l)
	}

	found := false
	for _, l := range got {
		if strings.Contains(l, "streamed-line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'streamed-line' in streamed lines, got: %v", got)
	}
}
