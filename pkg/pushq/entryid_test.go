package pushq

import (
	"strings"
	"testing"
	"time"
)

func TestSlugifyCommitMessage(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"add auth endpoint", "add-auth-endpoint"},
		{"Add Auth Endpoint", "add-auth-endpoint"},
		{"fix: token expiry bug", "fix-token-expiry-bug"},
		{"feat(auth): add login", "featauth-add-login"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"multiple   spaces", "multiple-spaces"},
		{"already-hyphenated", "already-hyphenated"},
		{"special!@#chars", "specialchars"},
		{"", ""},
		{"!!!", ""},
	}

	for _, tc := range cases {
		got := slugifyCommitMessage(tc.input)
		if got != tc.want {
			t.Errorf("slugifyCommitMessage(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestEntryID_IncludesCommitSlug(t *testing.T) {
	ts := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	id := EntryID("alice", ts, "add auth endpoint")

	if !strings.HasPrefix(id, "alice-") {
		t.Errorf("expected id to start with 'alice-', got %q", id)
	}
	if !strings.HasSuffix(id, "-add-auth-endpoint") {
		t.Errorf("expected id to end with '-add-auth-endpoint', got %q", id)
	}
}

func TestEntryID_EmptySlugOmitsSuffix(t *testing.T) {
	ts := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	id := EntryID("alice", ts, "!!!")

	// slug is empty — id should still be valid, just without a slug suffix
	if !strings.HasPrefix(id, "alice-") {
		t.Errorf("expected id to start with 'alice-', got %q", id)
	}
	if strings.HasSuffix(id, "-") {
		t.Errorf("id should not end with a trailing hyphen, got %q", id)
	}
}
