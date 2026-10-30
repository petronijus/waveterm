// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"os"
	"path/filepath"
	"testing"
)

func rels(locs []repoLoc) []string {
	out := make([]string, len(locs))
	for i, l := range locs {
		out[i] = l.Rel
	}
	return out
}

func TestDiscover(t *testing.T) {
	w := newWorld(t)
	main := w.clone(t, "app")
	w.clone(t, "app/private")          // nested repo
	w.clone(t, "group/client/site")    // depth 3
	w.clone(t, "a/b/c/d/too-deep")     // depth 5: beyond maxDepth 4
	w.clone(t, "web/node_modules/pkg") // skipped dir
	w.clone(t, ".hidden/repo")         // hidden dir
	w.clone(t, "big/output/aports")    // ignored by pattern
	git(t, main, "worktree", "add", "-q", filepath.Join(w.root, "app-feature"), "-b", "feature")
	if err := os.Symlink(main, filepath.Join(w.root, "link")); err != nil {
		t.Fatal(err)
	}

	locs, err := discover([]string{w.root}, 4, ignoreMatcher{"big/output/**"})
	if err != nil {
		t.Fatal(err)
	}
	got := rels(locs)
	want := []string{"app", "app-feature", "app/private", "group/client/site"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	byRel := map[string]repoLoc{}
	for _, l := range locs {
		byRel[l.Rel] = l
	}
	if l := byRel["app"]; l.GitDir != filepath.Join(main, ".git") || l.CommonDir != l.GitDir {
		t.Errorf("main repo dirs: %+v", l)
	}
	wt := byRel["app-feature"]
	if wt.CommonDir != filepath.Join(main, ".git") || wt.GitDir != filepath.Join(main, ".git", "worktrees", "app-feature") {
		t.Errorf("worktree dirs: %+v", wt)
	}
}

func TestDiscoverMissingRootReportsError(t *testing.T) {
	if _, err := discover([]string{filepath.Join(t.TempDir(), "nope")}, 4, nil); err == nil {
		t.Error("expected an error for a missing root")
	}
}

func TestIgnoreMatcher(t *testing.T) {
	m := ignoreMatcher(parseList(" big/output/** , OH-SO/*,\nexact "))
	for rel, want := range map[string]bool{
		"big/output":        true,
		"big/output/aports": true,
		"big/outputs":       false,
		"OH-SO/site":        true,
		"OH-SO/client/site": false,
		"exact":             true,
		"exact/nested":      false,
		"something/else":    false,
	} {
		if got := m.match(rel); got != want {
			t.Errorf("match(%q) = %v, want %v", rel, got, want)
		}
	}
}
