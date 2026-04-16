package runner

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/ezcdlabs/pushq/internal/gitenv"
)

// Result holds the outcome of a test command run.
type Result struct {
	Passed bool
	Output string
}

// Run executes command in workDir and returns whether it exited 0.
// Combined stdout+stderr is captured in Result.Output.
// If lines is non-nil, each output line is sent to it as it is produced.
// Returns a non-nil error only for unexpected failures (e.g. context cancelled,
// command not found) — a non-zero exit code is not an error, it sets Passed=false.
func Run(ctx context.Context, command string, workDir string, lines chan<- string) (Result, error) {
	cmd := buildCommand(ctx, command)
	cmd.Dir = workDir

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	var sb strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			sb.WriteString(line + "\n")
			if lines != nil {
				lines <- line
			}
		}
	}()

	err := cmd.Run()
	pw.Close()
	wg.Wait()

	output := sb.String()

	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if isExitError(err) {
			return Result{Passed: false, Output: output}, nil
		}
		return Result{}, err
	}

	return Result{Passed: true, Output: output}, nil
}

func buildCommand(ctx context.Context, command string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		parts := strings.Fields(command)
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Env = gitenv.Clean()
	return cmd
}

func isExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}
