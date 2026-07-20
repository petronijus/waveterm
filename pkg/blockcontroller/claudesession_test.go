// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSession(t *testing.T, dir string, id string, modTime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, id+claudeSessionExt)
	if err := os.WriteFile(p, []byte("{}\n"), 0600); err != nil {
		t.Fatalf("writing %s: %v", p, err)
	}
	if err := os.Chtimes(p, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", p, err)
	}
	return p
}

func TestClaudeProjectDirForCwd(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	got := claudeProjectDirForCwd("/Users/pj/Documents/Dev/waveterm")
	want := filepath.Join("/cfg", "projects", "-Users-pj-Documents-Dev-waveterm")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if claudeProjectDirForCwd("") != "" {
		t.Errorf("empty cwd should yield no project dir")
	}
}

// A directory name containing a dash encodes the same way a separator does. The mapping
// is only ever used in this direction, so the collision is harmless — but pin it down so
// nobody later assumes it round-trips.
func TestClaudeProjectDirForCwdDashCollision(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	withDash := claudeProjectDirForCwd("/a/b-c")
	withSep := claudeProjectDirForCwd("/a/b/c")
	if withDash != withSep {
		t.Errorf("expected the known collision, got %q vs %q", withDash, withSep)
	}
}

func TestFindNewClaudeSessionDetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	writeSession(t, dir, "11111111-1111-1111-1111-111111111111", old)
	before := snapshotClaudeSessions(dir)

	writeSession(t, dir, "22222222-2222-2222-2222-222222222222", time.Now())
	if got := findNewClaudeSession(dir, before); got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("got %q, want the newly created session", got)
	}
}

// `claude --continue` reuses an existing transcript, so growth counts as ours too.
func TestFindNewClaudeSessionDetectsAdvancedMtime(t *testing.T) {
	dir := t.TempDir()
	id := "33333333-3333-3333-3333-333333333333"
	writeSession(t, dir, id, time.Now().Add(-time.Hour))
	before := snapshotClaudeSessions(dir)

	writeSession(t, dir, id, time.Now())
	if got := findNewClaudeSession(dir, before); got != id {
		t.Errorf("got %q, want %q", got, id)
	}
}

func TestFindNewClaudeSessionNoChange(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "44444444-4444-4444-4444-444444444444", time.Now().Add(-time.Hour))
	before := snapshotClaudeSessions(dir)
	if got := findNewClaudeSession(dir, before); got != "" {
		t.Errorf("got %q, want no match", got)
	}
}

// The case that broke in practice: another claude is already running in the same repo and
// keeps appending to its own transcript. A freshly created transcript is unambiguous even
// though a second file also changed, so the new session must still win.
func TestFindNewClaudeSessionPrefersCreatedOverModified(t *testing.T) {
	dir := t.TempDir()
	other := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	writeSession(t, dir, other, time.Now().Add(-time.Hour))
	before := snapshotClaudeSessions(dir)

	// The unrelated session keeps writing...
	writeSession(t, dir, other, time.Now())
	// ...while ours is created.
	mine := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	writeSession(t, dir, mine, time.Now())

	if got := findNewClaudeSession(dir, before); got != mine {
		t.Errorf("got %q, want the newly created session %q", got, mine)
	}
}

// The documented failure mode: two terminals starting claude in one directory at the same
// moment must bind nothing rather than bind the wrong session.
func TestFindNewClaudeSessionAmbiguous(t *testing.T) {
	dir := t.TempDir()
	before := snapshotClaudeSessions(dir)
	now := time.Now()
	writeSession(t, dir, "55555555-5555-5555-5555-555555555555", now)
	writeSession(t, dir, "66666666-6666-6666-6666-666666666666", now)
	if got := findNewClaudeSession(dir, before); got != "" {
		t.Errorf("got %q, want no match when two sessions changed", got)
	}
}

func TestSnapshotIgnoresNonSessionFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-uuid.jsonl"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "77777777-7777-7777-7777-777777777777.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := len(snapshotClaudeSessions(dir)); got != 0 {
		t.Errorf("snapshot picked up %d entries, want 0", got)
	}
}

func TestSnapshotMissingDir(t *testing.T) {
	if got := len(snapshotClaudeSessions(filepath.Join(t.TempDir(), "nope"))); got != 0 {
		t.Errorf("missing dir should snapshot empty, got %d", got)
	}
}

func TestClaudeResumeArgRegex(t *testing.T) {
	id := "88888888-8888-8888-8888-888888888888"
	matches := []string{
		"claude --resume " + id,
		"claude -r " + id,
		"claude --resume=" + id,
		"claude -r " + id + " --model opus",
	}
	for _, cmd := range matches {
		m := claudeResumeArgRegex.FindStringSubmatch(cmd)
		if m == nil || m[1] != id {
			t.Errorf("%q: expected to extract %q, got %v", cmd, id, m)
		}
	}
	nonMatches := []string{
		"claude",
		"claude --continue",
		"claude --resume",
		"grep -r " + id + " .",
	}
	for _, cmd := range nonMatches[:3] {
		if m := claudeResumeArgRegex.FindStringSubmatch(cmd); m != nil {
			t.Errorf("%q: expected no match, got %v", cmd, m)
		}
	}
}
