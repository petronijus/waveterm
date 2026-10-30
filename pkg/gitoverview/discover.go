// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// repoLoc is where a worktree and its git metadata live. For a linked worktree
// GitDir is <main>/.git/worktrees/<name> and CommonDir is <main>/.git.
type repoLoc struct {
	Root      string // absolute worktree root
	Rel       string // relative to the scan root it was found under
	GitDir    string
	CommonDir string
}

// skipDirs are never descended into: dependency trees and build output hold
// thousands of directories and never a repo worth listing.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "Pods": true, "build": true,
	"dist": true, "target": true, "DerivedData": true, "__pycache__": true,
}

func skippedName(name string) bool {
	return skipDirs[name] || strings.HasPrefix(name, ".")
}

// ignoreMatcher matches repo paths (relative to their scan root) against the
// user's ignore list: "a/b/**" ignores everything under a/b, anything else is
// a filepath.Match pattern on the whole relative path.
type ignoreMatcher []string

func parseList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m ignoreMatcher) match(rel string) bool {
	for _, p := range m {
		if prefix, ok := strings.CutSuffix(p, "/**"); ok {
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

// discover walks each root to maxDepth and returns every worktree found,
// nested repos included. It reads directory entries only: no stat per file,
// symlinks are not followed.
func discover(roots []string, maxDepth int, ignore ignoreMatcher) ([]repoLoc, error) {
	var repos []repoLoc
	seen := map[string]bool{}
	var firstErr error
	for _, root := range roots {
		var walk func(dir string, depth int)
		walk = func(dir string, depth int) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				if depth == 0 && firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, e := range entries {
				if e.Name() == ".git" {
					if loc, ok := resolveRepo(root, dir); ok && !seen[loc.Root] && !ignore.match(loc.Rel) {
						seen[loc.Root] = true
						repos = append(repos, loc)
					}
					break
				}
			}
			if depth >= maxDepth {
				return
			}
			for _, e := range entries {
				if e.IsDir() && !skippedName(e.Name()) {
					walk(filepath.Join(dir, e.Name()), depth+1)
				}
			}
		}
		walk(root, 0)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Root < repos[j].Root })
	return repos, firstErr
}

// resolveRepo locates the git dirs of the worktree at dir. A ".git" file is a
// linked worktree or a submodule: "gitdir: <path>", with the shared metadata
// named by <gitdir>/commondir when it exists.
func resolveRepo(scanRoot, dir string) (repoLoc, bool) {
	rel, err := filepath.Rel(scanRoot, dir)
	if err != nil {
		return repoLoc{}, false
	}
	if rel == "." {
		rel = filepath.Base(dir)
	}
	loc := repoLoc{Root: dir, Rel: filepath.ToSlash(rel)}
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return repoLoc{}, false
	}
	if info.IsDir() {
		loc.GitDir, loc.CommonDir = dotGit, dotGit
		return loc, true
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return repoLoc{}, false
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return repoLoc{}, false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	loc.GitDir, loc.CommonDir = realPath(gitDir), realPath(gitDir)
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		loc.CommonDir = realPath(common)
	}
	return loc, true
}

// realPath cleans p and resolves symlinks when it can: the watchers report
// real paths, and a gitdir: line may name a path through a symlink.
func realPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// expandHome resolves a leading "~/" against the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
