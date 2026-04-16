package gitenv

import (
	"os"
	"strings"
)

// gitInheritedVars are environment variables that git sets when it invokes
// external commands (e.g. "git pushq"). Subprocesses that in turn invoke git
// must not inherit these, otherwise git operates on the wrong directory.
var gitInheritedVars = map[string]bool{
	"GIT_DIR":       true,
	"GIT_WORK_TREE": true,
	"GIT_PREFIX":    true,
}

// Clean returns a copy of os.Environ with git-context variables stripped.
// Use this as cmd.Env for any exec.Cmd that calls git, to avoid inheriting
// GIT_DIR and friends when pushq is itself invoked as a git subcommand.
func Clean() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		key := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			key = e[:i]
		}
		if !gitInheritedVars[key] {
			out = append(out, e)
		}
	}
	return out
}
