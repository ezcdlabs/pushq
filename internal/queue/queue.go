package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ezcdlabs/pushq/internal/gitenv"
	"github.com/ezcdlabs/pushq/internal/protocol"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const stateBranch = "refs/pushq/state"
const landedFile = "entries/_landed.json"

// EntryRecord is a queue entry as returned by ListEntries, enriched with its ID.
type EntryRecord struct {
	ID     string
	Ref    string
	Status protocol.Status
}

// LandedEntry is the most recently landed queue entry, stored as
// entries/_landed.json in the state branch. At most one exists at a time.
type LandedEntry struct {
	Ref     string `json:"ref"`
	MainSHA string `json:"main_sha"`
}

// Join adds an entry to the queue with status "waiting" and pushes the state
// branch. Retries automatically on fast-forward push failures.
func Join(repoPath, remote, entryID, entryRef string) error {
	return defaultManager.join(repoPath, remote, entryID, entryRef)
}

// SetStatus updates the status of an existing entry and pushes the state branch.
func SetStatus(repoPath, remote, entryID string, status protocol.Status) error {
	return defaultManager.setStatus(repoPath, remote, entryID, status)
}

// RemoveEntry deletes an entry from the state branch tree (used for ejections).
func RemoveEntry(repoPath, remote, entryID, reason string) error {
	return defaultManager.removeEntry(repoPath, remote, entryID, reason)
}

// LandEntry removes the active entry file and writes entries/_landed.json,
// replacing any previous landed record. mainSHA is the commit SHA on the main
// branch that this entry landed as.
func LandEntry(repoPath, remote, entryID, mainSHA string) error {
	return defaultManager.landEntry(repoPath, remote, entryID, mainSHA)
}

// ReadState fetches the state branch and returns active entries in queue order
// plus the most recent landed record (nil if nothing has landed yet).
func ReadState(repoPath, remote string) ([]EntryRecord, *LandedEntry, error) {
	if err := fetchStateBranch(repoPath, remote); err != nil {
		return nil, nil, err
	}
	entries, err := readEntriesFromLocal(repoPath)
	if err != nil {
		return nil, nil, err
	}
	landed, err := readLandedFromLocal(repoPath)
	if err != nil {
		return nil, nil, err
	}
	return entries, landed, nil
}

// ReadEntry fetches the current state branch and returns the entry for entryID.
func ReadEntry(repoPath, remote, entryID string) (*EntryRecord, error) {
	entries, err := ListEntries(repoPath, remote)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.ID == entryID {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("entry %q not found", entryID)
}

// ListEntries fetches the state branch and returns all active entries in queue
// order (commit order on the state branch).
func ListEntries(repoPath, remote string) ([]EntryRecord, error) {
	if err := fetchStateBranch(repoPath, remote); err != nil {
		return nil, err
	}
	return readEntriesFromLocal(repoPath)
}

// --- internal ----------------------------------------------------------------

// stateManager holds the push function used by the optimistic lock loop.
// Tests construct their own stateManager with a scripted pushFn to exercise
// specific retry scenarios without mutating any global state.
type stateManager struct {
	pushFn func(repoPath, remote string) error
}

// defaultManager is used by all public functions in production.
var defaultManager = &stateManager{pushFn: pushStateBranch}

func (sm *stateManager) join(repoPath, remote, entryID, entryRef string) error {
	entry := protocol.QueueEntry{
		Ref:    entryRef,
		Status: protocol.StatusWaiting,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return sm.updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		files["entries/"+entryID+".json"] = data
	}, "join: "+entryID)
}

func (sm *stateManager) setStatus(repoPath, remote, entryID string, status protocol.Status) error {
	return sm.updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		existing := files["entries/"+entryID+".json"]
		var entry protocol.QueueEntry
		if err := json.Unmarshal(existing, &entry); err == nil {
			entry.Status = status
			if data, err := json.Marshal(entry); err == nil {
				files["entries/"+entryID+".json"] = data
			}
		}
	}, "status: "+entryID+" "+string(status))
}

func (sm *stateManager) removeEntry(repoPath, remote, entryID, reason string) error {
	return sm.updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		delete(files, "entries/"+entryID+".json")
	}, reason+": "+entryID)
}

func (sm *stateManager) landEntry(repoPath, remote, entryID, mainSHA string) error {
	entryFile := "entries/" + entryID + ".json"
	return sm.updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		var ref string
		if data, ok := files[entryFile]; ok {
			var entry protocol.QueueEntry
			if err := json.Unmarshal(data, &entry); err == nil {
				ref = entry.Ref
			}
		}
		delete(files, entryFile)
		if landed, err := json.Marshal(LandedEntry{Ref: ref, MainSHA: mainSHA}); err == nil {
			files[landedFile] = landed
		}
	}, "land: "+entryID)
}

// updateStateBranch is the optimistic lock loop: fetch, mutate the file tree,
// commit, push. Retries on fast-forward rejection.
func (sm *stateManager) updateStateBranch(repoPath, remote string, mutate func(map[string][]byte), message string) error {
	for {
		// Fetch (or init) the state branch.
		_ = fetchStateBranch(repoPath, remote) // ignore error — branch may not exist yet

		repo, err := gogit.PlainOpen(repoPath)
		if err != nil {
			return fmt.Errorf("open repo: %w", err)
		}

		// Read current tree into a mutable map.
		files, parentHash, err := readStateBranchFiles(repo)
		if err != nil {
			return fmt.Errorf("read state branch: %w", err)
		}

		// Apply the mutation.
		mutate(files)

		// Build new tree.
		treeHash, err := buildTree(repo, files)
		if err != nil {
			return fmt.Errorf("build tree: %w", err)
		}

		// Commit.
		sig := &object.Signature{Name: "pushq", Email: "pushq@local", When: time.Now()}
		commit := &object.Commit{
			Author:    *sig,
			Committer: *sig,
			Message:   message,
			TreeHash:  treeHash,
		}
		if parentHash != plumbing.ZeroHash {
			commit.ParentHashes = []plumbing.Hash{parentHash}
		}

		enc := repo.Storer.NewEncodedObject()
		if err := commit.Encode(enc); err != nil {
			return fmt.Errorf("encode commit: %w", err)
		}
		commitHash, err := repo.Storer.SetEncodedObject(enc)
		if err != nil {
			return fmt.Errorf("store commit: %w", err)
		}

		// Update local ref.
		ref := plumbing.NewHashReference(plumbing.ReferenceName(stateBranch), commitHash)
		if err := repo.Storer.SetReference(ref); err != nil {
			return fmt.Errorf("set local ref: %w", err)
		}

		// Push.
		pushErr := sm.pushFn(repoPath, remote)
		if pushErr == nil {
			return nil
		}
		if isFastForwardRejected(pushErr) {
			continue // lost the race, retry
		}
		if isBrokenObjectError(pushErr) {
			// The local state branch contains invalid git objects (created by an
			// older version of pushq). Delete the local ref so the next iteration
			// fetches a clean copy from the remote — or starts an orphan commit if
			// the remote has no state branch yet.
			_ = deleteLocalStateRef(repoPath)
			continue
		}
		return fmt.Errorf("push state branch: %w", pushErr)
	}
}

func readStateBranchFiles(repo *gogit.Repository) (map[string][]byte, plumbing.Hash, error) {
	files := make(map[string][]byte)

	ref, err := repo.Reference(plumbing.ReferenceName(stateBranch), true)
	if err != nil {
		// State branch doesn't exist yet — start with empty tree.
		return files, plumbing.ZeroHash, nil
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}

	err = tree.Files().ForEach(func(f *object.File) error {
		content, err := f.Contents()
		if err != nil {
			return err
		}
		files[f.Name] = []byte(content)
		return nil
	})
	if err != nil {
		return nil, plumbing.ZeroHash, err
	}

	return files, ref.Hash(), nil
}

// buildTree constructs a git tree from a flat map of slash-separated paths to
// content. It creates proper nested sub-trees rather than flat entries with
// slashes in the name, so the resulting objects pass GitHub's fsck checks
// (fullPathname check via receive.fsckObjects).
func buildTree(repo *gogit.Repository, files map[string][]byte) (plumbing.Hash, error) {
	blobs := make(map[string]plumbing.Hash, len(files))
	for path, content := range files {
		enc := repo.Storer.NewEncodedObject()
		enc.SetType(plumbing.BlobObject)
		w, err := enc.Writer()
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if _, err := w.Write(content); err != nil {
			return plumbing.ZeroHash, err
		}
		w.Close()
		h, err := repo.Storer.SetEncodedObject(enc)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		blobs[path] = h
	}
	return buildNestedTree(repo, blobs, "")
}

// buildNestedTree recursively creates a git tree for all blob paths under prefix.
func buildNestedTree(repo *gogit.Repository, blobs map[string]plumbing.Hash, prefix string) (plumbing.Hash, error) {
	dirs := make(map[string]struct{})
	var entries []object.TreeEntry

	for path, h := range blobs {
		rel := path
		if prefix != "" {
			if !strings.HasPrefix(path, prefix+"/") {
				continue
			}
			rel = path[len(prefix)+1:]
		}
		if idx := strings.IndexByte(rel, '/'); idx >= 0 {
			dirs[rel[:idx]] = struct{}{}
		} else {
			entries = append(entries, object.TreeEntry{
				Name: rel,
				Mode: 0100644,
				Hash: h,
			})
		}
	}

	for dir := range dirs {
		sub := dir
		if prefix != "" {
			sub = prefix + "/" + dir
		}
		h, err := buildNestedTree(repo, blobs, sub)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{
			Name: dir,
			Mode: 0040000,
			Hash: h,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	tree := &object.Tree{Entries: entries}
	enc := repo.Storer.NewEncodedObject()
	if err := tree.Encode(enc); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(enc)
}

func readEntriesFromLocal(repoPath string) ([]EntryRecord, error) {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	ref, err := repo.Reference(plumbing.ReferenceName(stateBranch), true)
	if err != nil {
		return nil, nil // no state branch yet
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var records []EntryRecord
	err = tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasPrefix(f.Name, "entries/") || !strings.HasSuffix(f.Name, ".json") {
			return nil
		}
		if f.Name == landedFile {
			return nil // handled separately by readLandedFromLocal
		}
		id := strings.TrimSuffix(strings.TrimPrefix(f.Name, "entries/"), ".json")
		content, err := f.Contents()
		if err != nil {
			return err
		}
		var entry protocol.QueueEntry
		if err := json.Unmarshal([]byte(content), &entry); err != nil {
			return err
		}
		records = append(records, EntryRecord{ID: id, Ref: entry.Ref, Status: entry.Status})
		return nil
	})
	return records, err
}

func readLandedFromLocal(repoPath string) (*LandedEntry, error) {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}
	ref, err := repo.Reference(plumbing.ReferenceName(stateBranch), true)
	if err != nil {
		return nil, nil // no state branch yet
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var landed *LandedEntry
	err = tree.Files().ForEach(func(f *object.File) error {
		if f.Name != landedFile {
			return nil
		}
		content, err := f.Contents()
		if err != nil {
			return err
		}
		var l LandedEntry
		if err := json.Unmarshal([]byte(content), &l); err != nil {
			return err
		}
		landed = &l
		return nil
	})
	if err != nil {
		return nil, err
	}
	return landed, nil
}

func fetchStateBranch(repoPath, remote string) error {
	cmd := exec.Command("git", "fetch", remote, "+refs/pushq/state:refs/pushq/state")
	cmd.Dir = repoPath
	cmd.Env = gitenv.Clean()
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		// State branch doesn't exist on the remote yet — treat as empty, not an error.
		if strings.Contains(msg, "couldn't find remote ref") {
			return nil
		}
		return fmt.Errorf("fetch state branch: %w\n%s", err, msg)
	}
	return nil
}

func pushStateBranch(repoPath, remote string) error {
	cmd := exec.Command("git", "push", remote, "refs/pushq/state:refs/pushq/state")
	cmd.Dir = repoPath
	cmd.Env = gitenv.Clean()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// isBrokenObjectError reports whether err indicates the remote rejected the
// push because it received git objects that fail fsck (e.g. GitHub's
// receive.fsckObjects check). This typically means the local state branch was
// created by an older version of pushq that stored flat-tree entries with
// slashes in their names. The recovery is to delete the local ref and retry
// from scratch so the next commit has a clean ancestry.
func deleteLocalStateRef(repoPath string) error {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return err
	}
	return repo.Storer.RemoveReference(plumbing.ReferenceName(stateBranch))
}

func isBrokenObjectError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "fullPathname") ||
		strings.Contains(msg, "fsck error")
}

func isFastForwardRejected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gogit.ErrNonFastForwardUpdate) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "failed to update ref") ||
		strings.Contains(msg, "reference already exists") ||
		strings.Contains(msg, "incorrect old value provided") ||
		strings.Contains(msg, "[rejected]")
}
