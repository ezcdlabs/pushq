package queue_test

import (
	"testing"

	"github.com/ezcdlabs/pushq/internal/gittest"
	"github.com/ezcdlabs/pushq/internal/protocol"
	"github.com/ezcdlabs/pushq/internal/queue"
)

// TestJoin_CreatesEntryOnStateBranch verifies that joining adds an entry file
// to refs/pushq/state on the remote.
func TestJoin_CreatesEntryOnStateBranch(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	content, ok := remote.ReadFileAtRef("refs/pushq/state", "entries/"+entryID+".json")
	if !ok {
		t.Fatal("expected entry file on state branch after Join")
	}
	if content == "" {
		t.Fatal("entry file should not be empty")
	}
}

// TestJoin_EntryStatusIsWaiting verifies the initial status is "waiting".
func TestJoin_EntryStatusIsWaiting(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	entry, err := queue.ReadEntry(clone.Path, "origin", entryID)
	if err != nil {
		t.Fatalf("ReadEntry failed: %v", err)
	}
	if entry.Status != protocol.StatusWaiting {
		t.Fatalf("expected status %q, got %q", protocol.StatusWaiting, entry.Status)
	}
}

// TestSetStatus_UpdatesEntryOnStateBranch verifies status transitions are
// persisted to the remote state branch.
func TestSetStatus_UpdatesEntryOnStateBranch(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if err := queue.SetStatus(clone.Path, "origin", entryID, protocol.StatusTesting); err != nil {
		t.Fatalf("SetStatus failed: %v", err)
	}

	entry, err := queue.ReadEntry(clone.Path, "origin", entryID)
	if err != nil {
		t.Fatalf("ReadEntry failed: %v", err)
	}
	if entry.Status != protocol.StatusTesting {
		t.Fatalf("expected status %q, got %q", protocol.StatusTesting, entry.Status)
	}
}

// TestRemoveEntry_DeletesEntryFromStateBranch verifies that removing an entry
// deletes its file from the state branch tree.
func TestRemoveEntry_DeletesEntryFromStateBranch(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if err := queue.RemoveEntry(clone.Path, "origin", entryID, "eject"); err != nil {
		t.Fatalf("RemoveEntry failed: %v", err)
	}

	_, ok := remote.ReadFileAtRef("refs/pushq/state", "entries/"+entryID+".json")
	if ok {
		t.Fatal("expected entry file to be gone after RemoveEntry")
	}
}

// TestLandEntry_RemovesActiveEntry verifies that landing an entry deletes its
// active file from the state branch.
func TestLandEntry_RemovesActiveEntry(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if err := queue.LandEntry(clone.Path, "origin", entryID, "abc123"); err != nil {
		t.Fatalf("LandEntry failed: %v", err)
	}

	_, ok := remote.ReadFileAtRef("refs/pushq/state", "entries/"+entryID+".json")
	if ok {
		t.Fatal("expected active entry file to be gone after LandEntry")
	}
}

// TestLandEntry_WritesLandedRecord verifies that landing writes entries/_landed.json
// with the entry ref and main SHA.
func TestLandEntry_WritesLandedRecord(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	entryID := "alice-1744124000"
	entryRef := "refs/pushq/" + entryID

	if err := queue.Join(clone.Path, "origin", entryID, entryRef); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if err := queue.LandEntry(clone.Path, "origin", entryID, "abc123sha"); err != nil {
		t.Fatalf("LandEntry failed: %v", err)
	}

	entries, landed, err := queue.ReadState(clone.Path, "origin")
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no active entries after landing, got %d", len(entries))
	}
	if landed == nil {
		t.Fatal("expected a landed record after LandEntry")
	}
	if landed.Ref != entryRef {
		t.Fatalf("expected landed ref %q, got %q", entryRef, landed.Ref)
	}
	if landed.MainSHA != "abc123sha" {
		t.Fatalf("expected landed main_sha %q, got %q", "abc123sha", landed.MainSHA)
	}
}

// TestLandEntry_ReplacesExistingLandedRecord verifies that a second landing
// replaces the previous _landed.json rather than accumulating entries.
func TestLandEntry_ReplacesExistingLandedRecord(t *testing.T) {
	remote := gittest.NewRemote(t)
	alice := remote.NewClone(t)
	bob := remote.NewClone(t)

	if err := queue.Join(alice.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
		t.Fatalf("alice Join failed: %v", err)
	}
	bob.Fetch()
	if err := queue.Join(bob.Path, "origin", "bob-2", "refs/pushq/bob-2"); err != nil {
		t.Fatalf("bob Join failed: %v", err)
	}

	if err := queue.LandEntry(alice.Path, "origin", "alice-1", "sha-alice"); err != nil {
		t.Fatalf("alice LandEntry failed: %v", err)
	}
	bob.Fetch()
	if err := queue.LandEntry(bob.Path, "origin", "bob-2", "sha-bob"); err != nil {
		t.Fatalf("bob LandEntry failed: %v", err)
	}

	_, landed, err := queue.ReadState(bob.Path, "origin")
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if landed == nil {
		t.Fatal("expected a landed record")
	}
	if landed.Ref != "refs/pushq/bob-2" {
		t.Fatalf("expected bob's ref to be the landed record, got %q", landed.Ref)
	}
}

// TestReadState_NoLandedRecord_ReturnsNil verifies that ReadState returns nil
// for the landed record when nothing has landed yet.
func TestReadState_NoLandedRecord_ReturnsNil(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	if err := queue.Join(clone.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	_, landed, err := queue.ReadState(clone.Path, "origin")
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}
	if landed != nil {
		t.Fatalf("expected nil landed record, got %+v", landed)
	}
}

// TestListEntries_DoesNotIncludeLandedRecord verifies that ListEntries only
// returns active entries, not the _landed.json sentinel file.
func TestListEntries_DoesNotIncludeLandedRecord(t *testing.T) {
	remote := gittest.NewRemote(t)
	clone := remote.NewClone(t)

	if err := queue.Join(clone.Path, "origin", "alice-1", "refs/pushq/alice-1"); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if err := queue.LandEntry(clone.Path, "origin", "alice-1", "sha1"); err != nil {
		t.Fatalf("LandEntry failed: %v", err)
	}
	if err := queue.Join(clone.Path, "origin", "bob-2", "refs/pushq/bob-2"); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	entries, err := queue.ListEntries(clone.Path, "origin")
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 active entry, got %d: %v", len(entries), entries)
	}
	if entries[0].ID != "bob-2" {
		t.Fatalf("expected bob-2, got %q", entries[0].ID)
	}
}

// TestListEntries_ReturnsEntriesInQueueOrder verifies that ListEntries returns
// entries in commit order on the state branch (queue position order).
func TestListEntries_ReturnsEntriesInQueueOrder(t *testing.T) {
	remote := gittest.NewRemote(t)
	alice := remote.NewClone(t)
	bob := remote.NewClone(t)

	if err := queue.Join(alice.Path, "origin", "alice-100", "refs/pushq/alice-100"); err != nil {
		t.Fatalf("alice Join failed: %v", err)
	}

	bob.Fetch()
	if err := queue.Join(bob.Path, "origin", "bob-200", "refs/pushq/bob-200"); err != nil {
		t.Fatalf("bob Join failed: %v", err)
	}

	entries, err := queue.ListEntries(alice.Path, "origin")
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "alice-100" || entries[1].ID != "bob-200" {
		t.Fatalf("unexpected order: %v", entries)
	}
}

// TestJoin_ConcurrentJoins_BothSucceed verifies that two clones joining
// simultaneously both end up in the queue (optimistic lock retry works).
