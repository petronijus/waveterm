// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"path/filepath"
	"slices"
	"strings"
)

// watcher reports file-system changes as directory paths. Each OS has its own
// implementation (watch_*.go); repos it cannot cover are polled instead.
type watcher interface {
	Mode() string               // shown in the panel footer
	Uncovered() map[string]bool // worktree roots that must be polled
	Close()
}

// watchSink receives what a watcher saw. changed gets directories in which
// something changed; rescan means changes below a directory were coalesced
// and every repo there must be rechecked; overflow means events were dropped
// and everything must be rechecked.
type watchSink struct {
	changed  func(dirs []string)
	rescan   func(dir string)
	overflow func()
}

// pathIndex maps a changed directory to the worktrees whose status it can
// affect. Worktree roots match the innermost repo, so a nested repo does not
// wake its parent. A change in a linked worktree's private git dir affects
// that worktree only; one in the shared common dir (refs, packed-refs after a
// fetch) affects every worktree that shares it.
type pathIndex struct {
	roots   map[string]string
	gitDirs map[string]string
	common  map[string][]string
}

func newPathIndex(repos []repoLoc) *pathIndex {
	ix := &pathIndex{roots: map[string]string{}, gitDirs: map[string]string{}, common: map[string][]string{}}
	for _, r := range repos {
		ix.roots[r.Root] = r.Root
		ix.gitDirs[r.GitDir] = r.Root
		ix.common[r.CommonDir] = append(ix.common[r.CommonDir], r.Root)
	}
	return ix
}

// lookup returns the affected worktree roots, or nil when the change is noise
// (git object writes, a nested .git) or outside every repo. Whether git
// ignores a worktree directory is the caller's question (it asks git).
// For a change inside a worktree (not its git dir) rel is the directory
// relative to that worktree, "." for its root, so the caller can ask git
// whether it is ignored. known reports whether dir lies inside a known repo.
func (ix *pathIndex) lookup(dir string) (affected []string, rel string, known bool) {
	dir = filepath.Clean(dir)
	var below []string // path segments between the matched ancestor and dir
	for p := dir; ; p = filepath.Dir(p) {
		if root, ok := ix.gitDirs[p]; ok {
			if gitNoise(below) {
				return nil, "", true
			}
			if roots, ok := ix.common[p]; ok {
				return roots, "", true
			}
			return []string{root}, "", true
		}
		if roots, ok := ix.common[p]; ok {
			if gitNoise(below) {
				return nil, "", true
			}
			return roots, "", true
		}
		if root, ok := ix.roots[p]; ok {
			if worktreeNoise(below) {
				return nil, "", true
			}
			rel := "."
			if len(below) > 0 {
				rel = strings.Join(below, "/")
			}
			return []string{root}, rel, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil, "", false
		}
		below = append([]string{filepath.Base(p)}, below...)
	}
}

// gitNoise: object and pack writes come before the ref update that matters,
// the reflog moves together with the ref, submodule git dirs belong to the
// submodule, and an fsmonitor daemon's cookie files say nothing.
func gitNoise(below []string) bool {
	if len(below) == 0 {
		return false
	}
	switch below[0] {
	case "objects", "logs", "lfs", "modules", "fsmonitor--daemon":
		return true
	}
	return false
}

// worktreeNoise: a ".git" below a worktree root that is not a known git dir
// belongs to a repo discovery skipped (too deep, ignored, inside a skipped
// directory), not to this worktree.
func worktreeNoise(below []string) bool {
	return slices.Contains(below, ".git")
}

// underAny reports whether p is one of roots or below one.
func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
