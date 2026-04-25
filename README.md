# pushq (`git pushq`)

![demo](assets/happy-path.gif)

Serverless push queue for trunk-based development. Test your commits against everyone ahead of you in the queue — on your own machine, in parallel, before anything touches main. No server, no PRs, no merge queue service. Just git refs.

---

## Install

### macOS (Homebrew)

```sh
brew install ezcdlabs/tap/git-pushq
```

### Windows (Scoop)

```sh
scoop bucket add ezcdlabs https://github.com/ezcdlabs/scoop-bucket
scoop install git-pushq
```

### macOS / Linux (manual)

Download the latest release from [github.com/ezcdlabs/pushq/releases](https://github.com/ezcdlabs/pushq/releases), extract, and place `git-pushq` somewhere on your `$PATH`:

```sh
# example — adjust version and platform
curl -sSL https://github.com/ezcdlabs/pushq/releases/latest/download/git-pushq_linux_amd64.tar.gz \
  | tar -xz git-pushq && sudo mv git-pushq /usr/local/bin/
```

Git will then expose it as `git pushq`.

---

## Setup

Add a `.pushq.json` to your repo root:

```json
{
  "test_command": "make test",
  "main_branch": "main"
}
```

Commit it so everyone on the team uses the same test command.

---

## Usage

```sh
git pushq
```

That's it. `git pushq` squashes your commits, joins the queue, builds a local test stack against everyone ahead of you, runs your tests, and lands on main when it's your turn. Press `Q` to cancel and self-eject at any time.

---

## Design Document

## Problem Statement

Trunk-based development (TBD) works well at small scale but has a fundamental problem at larger team sizes: by the time your local tests pass, someone else may have pushed to main, meaning your tests didn't validate the actual integration. The standard solution — a merge queue with PRs and branches — solves this but introduces heavyweight process that encourages bad habits (long-lived branches, manual review gates, slow feedback loops).

`git pushq` is a serverless push queue that solves the scale problem while preserving the simplicity of pushing directly to main. It is explicitly a **shift-left** tool: integration testing happens on the developer's machine, speculatively and in pipeline order, before anything touches main.

---

## Core Concept

Instead of testing your local commits against main, you test them against **the full stack of in-progress queue entries ahead of you**, applied in order. This is a **pipelined** approach to parallelism rather than a forked one:

```
main ← alice's commits ← bob's commits ← carol's commits
        (alice testing)   (bob testing)    (carol testing)
```

All three are testing simultaneously. If alice passes, bob is already validated against alice's changes. If alice fails and is ejected, bob and carol rebuild their local stacks without alice and retest. The machine is occupied during testing (~1 minute), but the main branch is never broken.

---

## Architecture: Serverless via Git Refs

There is no server. The remote git repository itself acts as the coordination layer.

### Two types of refs

**`refs/pushq/state`** — a long-lived branch used purely for coordination. Never merged to main. Its commit history is the queue log. Its working tree contains one JSON file per active queue entry.

**`refs/pushq/<entry-id>`** — a ref per active queue entry holding that developer's actual commits, always rebased on main. Deleted when the entry completes or is ejected.

### The state branch structure

Each developer owns exactly one file in the state branch tree, named `entries/<entry-id>.json`:

```json
{ "ref": "refs/pushq/bob-1744124000", "status": "testing" }
```

Statuses: `waiting` → `testing` → (deleted on completion or ejection)

There is also at most **one landed record** in the tree at any time, at a fixed path `entries/_landed.json`:

```json
{ "ref": "refs/pushq/alice-1744123000", "main_sha": "a1b2c3d..." }
```

When an entry successfully lands on main, it writes this record (replacing any previous one) in the same commit that removes its own active entry file. When the next entry lands, it replaces the record again. Only the most recent landing is kept.

### Why per-file rather than one shared JSON blob

A single shared state file requires everyone to deserialise, mutate, reserialise, and win a push race. Per-file means:

- **Joins never conflict** — each join adds a new uniquely-named file
- **Status updates never conflict** — you only ever write your own file
- **Ejections are a simple file deletion commit**
- **No merge conflicts ever arise on the state branch** — only fast-forward push failures, which are trivially retried

**Optimistic locking** is provided for free by git's fast-forward push semantics. If your push is rejected, you fetch, replay your commit on top, and retry. There is no content conflict to resolve.

---

## Queue Ordering

**Commit order on the state branch is the authoritative queue order.** The timestamp in the entry filename is a tiebreaker hint, but the push order (i.e. which fast-forward won) is what actually determines position.

---

## Queue Refs Are Always Based on Main

Each developer's queue ref is always their squashed commit **rebased on main** — not on any other queue entry. This ref is immutable once pushed. It is a stable, standalone object that anyone can fetch at any time without waiting for any other queue member.

The integration stack is computed **locally at test time** by cherry-picking queue entries in order. The remote queue refs are never mutated after joining.

---

## Building the Local Test Stack

When ready to test, build the full integration stack locally:

```
git checkout -b pushq-test-branch main
for each entry ahead of you in queue (in commit order):
    git cherry-pick <entry-ref>
    if conflict: skip this entry (see below)
git cherry-pick <your own ref>
run tests
delete pushq-test-branch
```

This is a sequence of shell-outs to `git cherry-pick`. go-git does not have a reliable cherry-pick implementation; shelling out is the correct choice.

### Why this is correct

Because each queue ref is independently rebased on main, cherry-picking them in order onto main produces exactly the same result as if they had been pushed sequentially. You are testing your changes in the context of everyone ahead of you in the queue, which is the exact guarantee the push queue exists to provide.

### Cherry-pick conflicts: skip, don't wait

If `cherry-pick <entry-B>` fails during your stack build, skip B and continue building the stack without them.

This is safe because a cherry-pick failure is **deterministic** — it is a pure git operation with no race condition. If B's commits cannot be applied on top of A's, that will be true whether you check now or in 5 minutes. B's eventual ejection from the queue does not change the answer; it merely makes the queue state consistent with a fact the git object model already told you with certainty.

You are not making an optimistic bet when you skip B — you are stating a fact. Testing against `[A, C]` is a valid and complete test of your integration, because B was never going to be part of the final history anyway.

---

## Ejection Is Always Self-Sovereign

**Only the affected developer ejects themselves.** No other queue member can or should forcibly remove someone.

### How B discovers their conflict

B discovers their own cherry-pick failure the same way C does: by trying to apply their commits during their own stack build. B will discover the conflict at the same time or before C does, depending on polling lag.

### The ejection flow

1. B notices their cherry-pick fails during stack build
2. B self-ejects: commits deletion of `entries/<entry-id>.json` to state branch with message `"eject: <entry-id>"`
3. B deletes `refs/pushq/<entry-id>` from remote
4. B is informed they need to fix the conflict and rejoin

Meanwhile, anyone who skipped B during their own stack build continues testing unaffected. When B's entry disappears from the state branch, nothing changes for them — they already computed the correct stack.

### Offline / crashed members

If B's process dies after a conflict is detected but before they self-eject, B's entry will remain in the queue indefinitely. A `git pushq gc` command (see Open Questions) can clean up stale entries. This is a v2 concern.

---

## The Squash-on-Push Contract

When joining the queue, your commits are **squashed into a single commit** before being pushed to your queue ref. This is enforced by the tool, not optional.

### Rationale

Other developers are cherry-picking your work into their local test stacks. Multiple WIP commits increase conflict surface area and make cascade rebuilds more painful. A single clean commit with a real message makes cherry-picks almost always trivially clean.

This is a **social contract enforced mechanically**: you are asking others to build on your work, so you must present it cleanly.

### The join flow

1. User runs `git pushq`
2. Tool checks for any unpushed commits since diverging from main — if none, abort: "nothing to push"
3. If there are **uncommitted changes**, automatically stash them with `git stash -u` (including untracked files), then proceed. On success, stash is automatically popped. On failure or cancellation, the user is informed to run `git stash pop` manually.
4. Show the list of commits about to be squashed
5. Prompt user for a single commit message
6. Squash into one commit, rebased on main
7. Join the queue (see full flow below)

---

## Full CLI Flow

There is one command: `git pushq`. Queue visibility and cancellation are built into it interactively rather than being separate subcommands.

### `git pushq`

```
1.  Check working state (see squash flow above)
2.  Fetch refs/pushq/state
3.  Optimistic lock loop:
      a. Compute entry-id = "<username>-<unix-timestamp>"
      b. Create entries/<entry-id>.json with status "waiting"
      c. Commit to state branch with message "join: <entry-id>"
      d. git push refs/pushq/state
      e. If rejected (not fast-forward): fetch, go to (b)
4.  Push squashed commit (rebased on main) to refs/pushq/<entry-id>
5.  Update own entry status to "testing", commit + push to state branch
6.  Build local pushq-test-branch:
      a. checkout -b pushq-test-branch main
      b. cherry-pick each entry ahead of us in queue order, skipping conflicts
      c. cherry-pick own ref
7.  Run test command
8.  Delete pushq-test-branch
9.  If tests PASS:
      → Validate queue hasn't changed in a way that invalidates the result
      → If invalid: go to step 6 and retest
      → git push origin main  (fast-forward onto current main)
      → Atomically: remove own entry file + write entries/_landed.json, commit + push state branch
      → Delete refs/pushq/<entry-id> from remote
10. If tests FAIL:
      → Commit deletion of entries/<entry-id>.json, message "eject: <entry-id>"
      → Push state branch update
      → Delete refs/pushq/<entry-id> from remote
      → Notify user, exit non-zero
```

### When to rebuild the stack

After tests pass, before pushing to main, validate that the result is still good. The precise rule:

**A passing test run is still valid if and only if every entry you cherry-picked can be accounted for — either landed or still active in the queue.**

The algorithm uses one piece of local state (the ordered list of entry refs you cherry-picked, `testedWith`) and one piece of remote state (the single `_landed.json` record):

1. Read the current active entries and the latest landed record from the state branch.
2. Find the landed record's ref in your local `testedWith` list.
   - If it is **not in your list**: someone landed that you never tested with — your base is stale. Retest.
   - If it **is in your list** at position `i`: everything before position `i` is implicitly covered (the landing was itself valid, so its entire chain is baked into `main_sha`).
3. For each entry **after position `i`** in your `testedWith` list (entries that were between the last lander and you):
   - If still **active** in the queue: fine, keep waiting.
   - If **absent** (not active, not the landed record): it was ejected without landing. Retest.

This means a landing by someone ahead of you **never** triggers a retest. Only an ejection — an entry disappearing from the queue without leaving a landed record — invalidates your result.

If tests are already running when the queue changes, finish the current run first, then apply this check before attempting to push.

### Live queue display during `git pushq`

While `git pushq` is running, it continuously polls `refs/pushq/state` and renders the current queue inline — entry IDs, statuses, and positions — updating on each poll tick. There are no separate `git pushq status` or `git pushq cancel` commands.

To cancel, the user presses `Q`. This triggers a self-ejection (same flow as a test failure ejection), deletes the queue ref, and restores stashed changes if applicable.

---

## Cascade Behaviour on Ejection

When an entry is ejected, all entries behind it in the queue are unaffected in terms of their remote refs — those are all still valid rebases on main. They simply need to rebuild their local test stack without the ejected entry and retest.

There is no O(n²) cascade problem here because no remote refs need updating. The only cost is re-running local tests, which is the unavoidable minimum.

---

## Polling

There is no push notification mechanism — all CLIs poll `refs/pushq/state` on a short interval (suggested: 5 seconds). Given that tests take ~60 seconds, a 5-second polling lag is negligible.

---

## Configuration

A `.pushq.json` in the repo root:

```json
{
  "test_command": "make test",
  "main_branch": "main"
}
```

---

## Implementation Notes (Go)

### Recommended structure

```
pushq/
├── pkg/pushq/              # public library — all domain logic, no TUI or I/O
├── cmd/git-pushq/              # TUI (Bubble Tea), interface definition, wiring to pkg/pushq
├── internal/
│   ├── queue/           # state branch read/write, optimistic lock loop
│   ├── stack/           # local cherry-pick stack builder
│   ├── runner/          # runs test command, captures exit code + output
│   ├── refs/            # push/fetch/delete queue refs
│   └── protocol/        # shared types: QueueEntry, Status
└── config/              # .pushq.json loading
```

### Separation of library and CLI

`pkg/pushq` exposes the full push queue lifecycle as a Go API with no terminal I/O. Acceptance tests call it directly against real local git repos — no binary build required, no TUI to drive.

`cmd/git-pushq` is a small hexagonal layer whose core is the Bubble Tea rendering logic. It defines a `PushSession` interface based on what the TUI needs (a stream of events), not what `pkg/pushq` exposes. In production, the interface is satisfied by `pkg/pushq`. In tests, a fake with scripted events is injected. This means TUI rendering can be tested without any git operations.

```go
// defined in cmd/git-pushq — the TUI's contract with the outside world
type PushSession interface {
    Start() <-chan pushq.Event
    Cancel()
}
```

### TUI: Bubble Tea + Lipgloss

The interactive display during `git pushq` is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss). These are chosen over raw ANSI/PTY approaches for cross-platform support (including Windows) via `golang.org/x/term`. Bubble Tea's model/update/view pattern is also significantly easier to reason about than cursor manipulation by hand.

### go-git vs shelling out

Use `go-git` for: reading the state branch, listing refs, writing/committing the state tree, fetching.

Shell out to `git cherry-pick` and `git rebase` for actual history manipulation — go-git does not have working implementations of either. This is not a compromise; most real-world Go git tooling does the same for complex operations.

### Optimistic lock loop

The state branch update pattern used everywhere:

```go
for {
    err := fetchStateBranch()
    mutateState()
    err = pushStateBranch()
    if err == nil {
        break
    }
    if !isFastForwardRejected(err) {
        return err  // real error, don't retry
    }
    // lost the race, retry
}
```

### Recording demos

`cmd/demo` is an interactive design browser and scenario player for the TUI. It is completely standalone — no git server or real push queue needed.

**Interactive browser** (step through all design states with arrow keys):

```sh
go run ./cmd/demo
```

**Scenario playback** (auto-plays a scripted narrative end-to-end):

```sh
go run ./cmd/demo --play happy-path
```

**Recording a GIF** with [asciinema](https://asciinema.org) and [agg](https://github.com/asciinema/agg):

```sh
# install dependencies
pip install asciinema           # (or: sudo apt install asciinema)
curl -sL https://github.com/asciinema/agg/releases/latest/download/agg-x86_64-unknown-linux-gnu -o ~/.local/bin/agg && chmod +x ~/.local/bin/agg

./scripts/record-demo.sh
# → assets/happy-path.gif
```

---

## What This Is Not

- Not a code review tool — no PRs, no approvals
- Not a CI system — tests run locally, on the developer's machine
- Not a replacement for post-merge CI — that can still run asynchronously as a safety net, it just isn't a gate
- Not suitable for test suites longer than ~5 minutes (machine occupation becomes unacceptable)

---

## Open Questions / Future Work

Issues are grouped roughly by when they are likely to surface.

### Immediate / first real use

- **Stale test branch** — if a previous run crashed, `pushq-test-branch` may already exist. The tool should detect and delete it at startup rather than failing mid-run.
- **Duplicate entry detection** — running `git pushq` twice creates a second entry under a new timestamp. The tool should detect an existing entry owned by you and abort (or offer to cancel it) before joining again.

### Soon after first use

- **Stash restore on failure** — automatically run `git stash pop` on test failure or ejection so the developer is returned to their original working state cleanly.
- **Conflict UX on join** — when your squashed commit cannot be rebased onto main cleanly, the current error is raw git output. Needs a clear message and a clean exit state so the developer knows exactly what to fix before rejoining.

### Once multiple people are using it

- **`git pushq gc`** — clean up orphaned `refs/pushq/*` refs and state entries from crashed or offline sessions. This is the highest-priority v2 item: a crashed session leaves a permanent ghost entry that affects queue perception for everyone behind it indefinitely.
- **Partial state recovery** — if the tool pushes the queue ref but then fails to update the state branch (or vice versa), it should detect and repair its own half-written state on the next run rather than leaving the queue inconsistent.

### Edge cases

- **Main moves between join and land** — if main advances after you join but before you land, the fast-forward push to main will be rejected. The tool should handle this gracefully: re-rebase, retest if necessary, and retry rather than exiting with a raw push error.
- **Queue drains while waiting** — if all entries ahead of you land while you are in the waiting phase, the tool should proceed immediately on the next poll rather than waiting the full poll interval.

### Future / optional

- **Multi-commit option** — v1 squashes always; a future flag could preserve commits for teams with atomic-commit history policies (squash for the queue ref, restore original commits when landing on main).
