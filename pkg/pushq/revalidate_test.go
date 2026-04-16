package pushq

import "testing"

// NeedsRetest(testedWith, landed, activeAheadRefs)
//
// testedWith      — ordered refs we cherry-picked, oldest-first
// landed          — most recent landed record from state branch (nil if none)
// activeAheadRefs — refs currently ahead of us and still in the queue

func TestNeedsRetest_NothingTestedNothingChanged_False(t *testing.T) {
	if NeedsRetest(nil, nil, nil) {
		t.Fatal("expected false: nothing was tested, nothing to invalidate")
	}
}

func TestNeedsRetest_AllTestedEntriesStillActive_False(t *testing.T) {
	tested := []string{"alice-ref", "bob-ref"}
	active := []string{"alice-ref", "bob-ref"}

	if NeedsRetest(tested, nil, active) {
		t.Fatal("expected false: every tested entry is still active")
	}
}

func TestNeedsRetest_EntryEjected_NoLandedRecord_True(t *testing.T) {
	tested := []string{"alice-ref", "bob-ref"}
	active := []string{"alice-ref"} // bob disappeared without landing

	if !NeedsRetest(tested, nil, active) {
		t.Fatal("expected true: bob was ejected")
	}
}

func TestNeedsRetest_LandedEntryWasOnlyTestedEntry_False(t *testing.T) {
	tested := []string{"alice-ref"}
	landed := &LandedRecord{Ref: "alice-ref", MainSHA: "abc"}
	active := []string{} // alice landed, nobody else ahead

	if NeedsRetest(tested, landed, active) {
		t.Fatal("expected false: alice landed and was the only tested entry")
	}
}

func TestNeedsRetest_LandedAtFront_EntryAfterStillActive_False(t *testing.T) {
	// alice landed; bob (between alice and us) is still testing
	tested := []string{"alice-ref", "bob-ref"}
	landed := &LandedRecord{Ref: "alice-ref", MainSHA: "abc"}
	active := []string{"bob-ref"}

	if NeedsRetest(tested, landed, active) {
		t.Fatal("expected false: alice landed (covers entries before her), bob still active")
	}
}

func TestNeedsRetest_LandedAtFront_EntryAfterEjected_True(t *testing.T) {
	// alice landed; bob (between alice and us) was ejected
	tested := []string{"alice-ref", "bob-ref"}
	landed := &LandedRecord{Ref: "alice-ref", MainSHA: "abc"}
	active := []string{} // bob gone

	if !NeedsRetest(tested, landed, active) {
		t.Fatal("expected true: bob was ejected after alice landed")
	}
}

func TestNeedsRetest_LandedAtBack_NothingAfter_False(t *testing.T) {
	// bob was the last entry above us and has now landed
	tested := []string{"alice-ref", "bob-ref"}
	landed := &LandedRecord{Ref: "bob-ref", MainSHA: "abc"}
	active := []string{} // alice also gone — covered by bob's landing chain

	if NeedsRetest(tested, landed, active) {
		t.Fatal("expected false: bob landed, alice is covered by bob's landing chain")
	}
}

func TestNeedsRetest_LandedInMiddle_EntryAfterActive_False(t *testing.T) {
	tested := []string{"alice-ref", "bob-ref", "carol-ref"}
	landed := &LandedRecord{Ref: "bob-ref", MainSHA: "abc"}
	active := []string{"carol-ref"}

	if NeedsRetest(tested, landed, active) {
		t.Fatal("expected false: bob landed (alice covered), carol still active")
	}
}

func TestNeedsRetest_LandedInMiddle_EntryAfterEjected_True(t *testing.T) {
	tested := []string{"alice-ref", "bob-ref", "carol-ref"}
	landed := &LandedRecord{Ref: "bob-ref", MainSHA: "abc"}
	active := []string{} // carol gone after bob landed

	if !NeedsRetest(tested, landed, active) {
		t.Fatal("expected true: carol was ejected after bob landed")
	}
}

func TestNeedsRetest_LandedNotInTestedWith_AllTestedStillActive_False(t *testing.T) {
	// dave landed (he was behind us in the queue, or landed before our test ran)
	// irrelevant to our chain — just check our own tested entries
	tested := []string{"alice-ref", "bob-ref"}
	landed := &LandedRecord{Ref: "dave-ref", MainSHA: "abc"}
	active := []string{"alice-ref", "bob-ref"}

	if NeedsRetest(tested, landed, active) {
		t.Fatal("expected false: landed entry is irrelevant, tested entries still active")
	}
}

func TestNeedsRetest_LandedNotInTestedWith_TestedEntryEjected_True(t *testing.T) {
	tested := []string{"alice-ref", "bob-ref"}
	landed := &LandedRecord{Ref: "dave-ref", MainSHA: "abc"}
	active := []string{"alice-ref"} // bob ejected

	if !NeedsRetest(tested, landed, active) {
		t.Fatal("expected true: bob ejected, landed entry is unrelated")
	}
}

func TestNeedsRetest_EmptyTestedWith_SomethingLanded_False(t *testing.T) {
	// We were first in the queue — nobody to test against.
	// Something else landed (must have been from before we joined).
	landed := &LandedRecord{Ref: "alice-ref", MainSHA: "abc"}

	if NeedsRetest(nil, landed, nil) {
		t.Fatal("expected false: nothing was in testedWith so nothing can be ejected")
	}
}

func TestNeedsRetest_FirstEntryEjected_True(t *testing.T) {
	// alice (first ahead of us) ejected without landing
	tested := []string{"alice-ref", "bob-ref", "carol-ref"}
	active := []string{"bob-ref", "carol-ref"} // alice gone, no landed record

	if !NeedsRetest(tested, nil, active) {
		t.Fatal("expected true: alice was ejected from the front")
	}
}
