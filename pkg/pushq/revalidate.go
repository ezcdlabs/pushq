package pushq

// LandedRecord is the most recently landed queue entry, stored as
// entries/_landed.json in the state branch.
type LandedRecord struct {
	Ref     string // the entry ref that landed, e.g. refs/pushq/alice-123
	MainSHA string // the main branch SHA after the landing commit
}

// NeedsRetest reports whether a passing test run has been invalidated by
// changes in the queue since the tests ran.
//
// testedWith is the ordered list of entry refs cherry-picked during the test,
// oldest (front of queue) first. landed is the most recent landed record from
// the state branch (nil if none has landed). activeAheadRefs is the current
// set of entry refs still active ahead of you in the queue.
func NeedsRetest(testedWith []string, landed *LandedRecord, activeAheadRefs []string) bool {
	active := make(map[string]bool, len(activeAheadRefs))
	for _, ref := range activeAheadRefs {
		active[ref] = true
	}

	// Find the anchor: the position of the landed ref in our testedWith list.
	// Everything before it is covered by the landing chain; only entries after
	// it (between the lander and us) need checking.
	startIdx := 0
	if landed != nil {
		for i, ref := range testedWith {
			if ref == landed.Ref {
				startIdx = i + 1
				break
			}
		}
	}

	// Any entry from startIdx onwards that is no longer active was ejected.
	for _, ref := range testedWith[startIdx:] {
		if !active[ref] {
			return true
		}
	}
	return false
}
