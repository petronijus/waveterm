// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"slices"
	"testing"
)

func TestPathIndexLookup(t *testing.T) {
	ix := newPathIndex([]repoLoc{
		{Root: "/dev/app", GitDir: "/dev/app/.git", CommonDir: "/dev/app/.git"},
		{Root: "/dev/app/private", GitDir: "/dev/app/private/.git", CommonDir: "/dev/app/private/.git"},
		{Root: "/dev/app-wt", GitDir: "/dev/app/.git/worktrees/app-wt", CommonDir: "/dev/app/.git"},
	})
	tests := []struct {
		dir   string
		want  []string
		known bool
	}{
		{"/dev/app", []string{"/dev/app"}, true},
		{"/dev/app/src/deep", []string{"/dev/app"}, true},
		{"/dev/app/private/src", []string{"/dev/app/private"}, true},                     // innermost repo only
		{"/dev/app/.git", []string{"/dev/app", "/dev/app-wt"}, true},                     // shared index/HEAD dir
		{"/dev/app/.git/refs/remotes/origin", []string{"/dev/app", "/dev/app-wt"}, true}, // fetch moves refs for all
		{"/dev/app/.git/worktrees/app-wt", []string{"/dev/app-wt"}, true},                // worktree-private HEAD/index
		{"/dev/app/.git/objects/ab", nil, true},                                          // object writes are noise
		{"/dev/app/.git/logs/refs/heads", nil, true},                                     // reflog moves with the ref
		{"/dev/app/web/node_modules/x", []string{"/dev/app"}, true},                      // git decides (check-ignore)
		{"/dev/app/vendor/x", []string{"/dev/app"}, true},                                // may be tracked
		{"/dev/app/.git/modules/sub/objects", nil, true},                                 // a submodule's git dir
		{"/dev/app/.git/fsmonitor--daemon/cookies", nil, true},                           // fsmonitor cookies
		{"/dev/app/deep/other/.git/refs", nil, true},                                     // an undiscovered repo
		{"/dev/app-wt/src", []string{"/dev/app-wt"}, true},
		{"/dev/new-clone", nil, false},
		{"/elsewhere", nil, false},
	}
	for _, tc := range tests {
		got, _, known := ix.lookup(tc.dir)
		slices.Sort(got)
		if !slices.Equal(got, tc.want) || known != tc.known {
			t.Errorf("lookup(%q) = %v, %v; want %v, %v", tc.dir, got, known, tc.want, tc.known)
		}
	}
}

func TestPathIndexLookupRel(t *testing.T) {
	ix := newPathIndex([]repoLoc{{Root: "/dev/app", GitDir: "/dev/app/.git", CommonDir: "/dev/app/.git"}})
	for dir, want := range map[string]string{
		"/dev/app":                    ".",
		"/dev/app/.claude/projects/x": ".claude/projects/x",
		"/dev/app/.git/refs/heads":    "", // git dir: no ignore check
	} {
		if _, rel, _ := ix.lookup(dir); rel != want {
			t.Errorf("lookup(%q) rel = %q, want %q", dir, rel, want)
		}
	}
}

func TestUnderAny(t *testing.T) {
	roots := []string{"/dev", "/work/x"}
	for p, want := range map[string]bool{"/dev": true, "/dev/a": true, "/devx": false, "/work/x/y": true, "/work": false} {
		if underAny(p, roots) != want {
			t.Errorf("underAny(%q) != %v", p, want)
		}
	}
}
