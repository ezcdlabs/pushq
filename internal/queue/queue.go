package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ezcdlabs/pushq/internal/protocol"
	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
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
	entry := protocol.QueueEntry{
		Ref:    entryRef,
		Status: protocol.StatusWaiting,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		files["entries/"+entryID+".json"] = data
	}, "join: "+entryID)
}

// SetStatus updates the status of an existing entry and pushes the state branch.
func SetStatus(repoPath, remote, entryID string, status protocol.Status) error {
	return updateStateBranch(repoPath, remote, func(files map[string][]byte) {
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

// RemoveEntry deletes an entry from the state branch tree (used for ejections).
func RemoveEntry(repoPath, remote, entryID, reason string) error {
	return updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		delete(files, "entries/"+entryID+".json")
	}, reason+": "+entryID)
}

// LandEntry removes the active entry file and writes entries/_landed.json,
// replacing any previous landed record. mainSHA is the commit SHA on the main
// branch that this entry landed as.
func LandEntry(repoPath, remote, entryID, mainSHA string) error {
	entryFile := "entries/" + entryID + ".json"
	return updateStateBranch(repoPath, remote, func(files map[string][]byte) {
		// Read the entry's ref before deleting it.
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

// updateStateBranch is the optimistic lock loop: fetch, mutate the file tree,
// commit, push. Retries on fast-forward rejection.
func updateStateBranch(repoPath, remote string, mutate func(map[string][]byte), message string) error {
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
		pushErr := pushStateBranch(repoPath, remote)
		if pushErr == nil {
			return nil
		}
		if isFastForwardRejected(pushErr) {
			continue // lost the race, retry
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

func buildTree(repo *gogit.Repository, files map[string][]byte) (plumbing.Hash, error) {
	var entries []object.TreeEntry

	for path, content := range files {
		blob := &object.Blob{}
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
		blobHash, err := repo.Storer.SetEncodedObject(enc)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		_ = blob

		// Handle subdirectories — go-git tree objects are flat per-level, so
		// for simple "entries/<id>.json" paths we build a nested tree.
		entries = append(entries, object.TreeEntry{
			Name: path,
			Mode: 0100644,
			Hash: blobHash,
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
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return err
	}
	err = repo.Fetch(&gogit.FetchOptions{
		RemoteName: remote,
		RefSpecs:   []gogitconfig.RefSpec{"+refs/pushq/state:refs/pushq/state"},
	})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

func pushStateBranch(repoPath, remote string) error {
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return err
	}
	err = repo.Push(&gogit.PushOptions{
		RemoteName: remote,
		RefSpecs:   []gogitconfig.RefSpec{"refs/pushq/state:refs/pushq/state"},
	})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
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
		strings.Contains(msg, "incorrect old value provided")
}
