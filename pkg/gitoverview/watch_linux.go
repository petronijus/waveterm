// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// maxDirsPerRepo caps the inotify watches one worktree may hold, over its
// whole life (directories created later count too). inotify is per
// directory, so a repo with a huge untracked tree is polled instead.
const maxDirsPerRepo = 4000

// watchBudgetShare is the fraction of the user's inotify watch limit the
// panel may take in total, leaving the rest to editors, IDEs and the desktop.
const watchBudgetShare = 4

// inotifyWatcher watches, per repo, every worktree directory git does not
// ignore (minus nested repos, which have their own entry), the git dir and
// common dir themselves and the refs tree. Scan roots are watched
// non-recursively so a fresh top-level clone shows up.
type inotifyWatcher struct {
	w      *fsnotify.Watcher
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex
	limit     int
	total     int
	repos     map[string]*repoWatch // by worktree root
	dirRepo   map[string]*repoWatch // watched worktree dir -> its repo
	refRepo   map[string]*repoWatch // watched refs dir -> its repo
	uncovered map[string]bool
}

type repoWatch struct {
	loc      repoLoc
	dirs     []string // every path this repo added, for removal
	released bool     // gave up: polled from now on, never watched again
}

var errBudget = errors.New("inotify watch budget exhausted")

func newWatcher(roots []string, repos []repoLoc, sink watchSink) (watcher, error) {
	fw, err := fsnotify.NewBufferedWatcher(1024)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	iw := &inotifyWatcher{
		w: fw, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		limit: watchLimit() / watchBudgetShare,
		repos: map[string]*repoWatch{}, dirRepo: map[string]*repoWatch{}, refRepo: map[string]*repoWatch{},
		uncovered: map[string]bool{},
	}
	for _, r := range roots {
		_ = iw.add(nil, r)
	}
	for _, loc := range repos {
		iw.watchRepo(loc)
	}
	go iw.loop(sink)
	return iw, nil
}

// watchLimit reads the kernel's per-user inotify watch limit.
func watchLimit() int {
	data, err := os.ReadFile("/proc/sys/fs/inotify/max_user_watches")
	if err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n > 0 {
			return n
		}
	}
	return 8192 // the historical default
}

// add watches path for rw (nil for a scan root), within both budgets.
func (iw *inotifyWatcher) add(rw *repoWatch, path string) error {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	if rw != nil && rw.released {
		return errBudget
	}
	if iw.total >= iw.limit || (rw != nil && len(rw.dirs) >= maxDirsPerRepo) {
		return errBudget
	}
	// Adding under the lock keeps release from racing a concurrent grow; an
	// inotify_add_watch is a single syscall.
	if err := iw.w.Add(path); err != nil {
		return err
	}
	iw.total++
	if rw != nil {
		rw.dirs = append(rw.dirs, path)
	}
	return nil
}

// watchRepo adds every watch a repo needs, or none: a repo that does not fit
// releases what it took and is polled.
func (iw *inotifyWatcher) watchRepo(loc repoLoc) {
	rw := &repoWatch{loc: loc}
	iw.mu.Lock()
	iw.repos[loc.Root] = rw
	iw.mu.Unlock()
	err := iw.add(rw, loc.GitDir)
	if err == nil && loc.CommonDir != loc.GitDir {
		err = iw.add(rw, loc.CommonDir)
	}
	if err == nil {
		err = iw.addRefs(rw, filepath.Join(loc.CommonDir, "refs"))
	}
	if err == nil {
		err = iw.addTree(rw, loc.Root, ignoredDirs(iw.ctx, loc.Root))
	}
	if err != nil {
		iw.release(rw)
	}
}

func (iw *inotifyWatcher) addRefs(rw *repoWatch, dir string) error {
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if err := iw.add(rw, p); err != nil {
			return err
		}
		iw.mu.Lock()
		iw.refRepo[p] = rw
		iw.mu.Unlock()
		return nil
	})
}

// addTree watches dir and every subdirectory that is not ignored, not .git
// and not a nested repo. ignored holds worktree-relative paths with a
// trailing slash, as `git ls-files --directory` prints them.
func (iw *inotifyWatcher) addTree(rw *repoWatch, dir string, ignored map[string]bool) error {
	if err := iw.add(rw, dir); err != nil {
		return err
	}
	iw.mu.Lock()
	iw.dirRepo[dir] = rw
	iw.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".git" {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		rel, _ := filepath.Rel(rw.loc.Root, sub)
		if ignored[filepath.ToSlash(rel)+"/"] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(sub, ".git")); err == nil {
			continue // nested repo: watched under its own entry
		}
		if err := iw.addTree(rw, sub, ignored); err != nil {
			return err
		}
	}
	return nil
}

// ignoredDirs lists the directories git ignores in a worktree, without
// descending into them: one call per repo instead of one per directory.
func ignoredDirs(ctx context.Context, root string) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, err := runGit(ctx, root, true, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	dirs := map[string]bool{}
	if err != nil {
		return dirs
	}
	for _, p := range strings.Split(string(out), "\x00") {
		if strings.HasSuffix(p, "/") {
			dirs[p] = true
		}
	}
	return dirs
}

// release removes every watch of a repo and marks it polled.
func (iw *inotifyWatcher) release(rw *repoWatch) {
	iw.mu.Lock()
	rw.released = true
	dirs := rw.dirs
	rw.dirs = nil
	iw.total -= len(dirs)
	for _, d := range dirs {
		delete(iw.dirRepo, d)
		delete(iw.refRepo, d)
	}
	iw.uncovered[rw.loc.Root] = true
	iw.mu.Unlock()
	for _, d := range dirs {
		_ = iw.w.Remove(d)
	}
}

func (iw *inotifyWatcher) loop(sink watchSink) {
	for {
		select {
		case <-iw.done:
			return
		case err, ok := <-iw.w.Errors:
			if !ok {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				sink.overflow()
			}
		case ev, ok := <-iw.w.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			dirs := []string{dir}
			if ev.Has(fsnotify.Create) {
				go iw.grow(dir, ev.Name)
				// A directory created in a scan root may be a fresh clone;
				// report it by name so the engine can check it for .git.
				dirs = append(dirs, ev.Name)
			}
			sink.changed(dirs)
		}
	}
}

// grow starts watching a directory created inside a watched worktree (unless
// git ignores it) or inside a watched refs tree (a branch named feature/x).
func (iw *inotifyWatcher) grow(parent, created string) {
	if info, err := os.Lstat(created); err != nil || !info.IsDir() {
		return
	}
	iw.mu.Lock()
	inTree, inRefs := iw.dirRepo[parent], iw.refRepo[parent]
	iw.mu.Unlock()
	var err error
	switch {
	case inRefs != nil:
		err = iw.addRefs(inRefs, created)
		inTree = inRefs
	case inTree != nil && filepath.Base(created) != ".git":
		if _, statErr := os.Lstat(filepath.Join(created, ".git")); statErr == nil {
			return // a nested repo; discovery picks it up
		}
		rel, _ := filepath.Rel(inTree.loc.Root, created)
		ctx, cancel := context.WithTimeout(iw.ctx, 10*time.Second)
		ignored, ignErr := gitIgnored(ctx, inTree.loc.Root, []string{filepath.ToSlash(rel)})
		cancel()
		if ignErr == nil && ignored[filepath.ToSlash(rel)] {
			return
		}
		err = iw.addTree(inTree, created, ignoredDirs(iw.ctx, inTree.loc.Root))
	default:
		return
	}
	if err != nil && iw.ctx.Err() == nil {
		iw.release(inTree)
	}
}

func (iw *inotifyWatcher) Mode() string { return "inotify" }

func (iw *inotifyWatcher) Uncovered() map[string]bool {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	out := make(map[string]bool, len(iw.uncovered))
	for k := range iw.uncovered {
		out[k] = true
	}
	return out
}

func (iw *inotifyWatcher) Close() {
	iw.cancel()
	close(iw.done)
	_ = iw.w.Close()
}
