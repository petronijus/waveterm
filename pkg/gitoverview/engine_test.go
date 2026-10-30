// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fastTimings are short enough for tests; the poll interval is an hour so
// only the watcher can notice changes.
var fastTimings = timings{
	debounce:        100 * time.Millisecond,
	minStatusGap:    200 * time.Millisecond,
	pollInterval:    time.Hour,
	rediscoverDelay: 300 * time.Millisecond,
	firstFetchDelay: 200 * time.Millisecond,
	publishDelay:    20 * time.Millisecond,
}

type harness struct {
	e         *Engine
	publishes atomic.Int64
}

func startEngine(t *testing.T, root string) *harness {
	t.Helper()
	h := &harness{}
	h.e = newEngine(func(Overview) { h.publishes.Add(1) }, fastTimings)
	t.Cleanup(h.e.Close)
	h.e.Configure(Config{Roots: []string{root}, MaxDepth: 4, FetchInterval: time.Hour})
	return h
}

func (h *harness) view(rel string) RepoView {
	for _, v := range h.e.Snapshot().Repos {
		if v.Rel == rel {
			return v
		}
	}
	return RepoView{}
}

// settle waits for the first fetch and then until no status run has happened
// for longer than the watcher's delivery latency plus the debounce.
func (h *harness) settle(t *testing.T) {
	t.Helper()
	eventually(t, 10*time.Second, "the first fetch", func() bool { return !h.e.Snapshot().LastFetch.IsZero() })
	quiet := 2 * time.Second
	last, since := fmt.Sprint(h.e.statusRuns()), time.Now()
	eventually(t, 20*time.Second, "the engine to go quiet", func() bool {
		if now := fmt.Sprint(h.e.statusRuns()); now != last {
			last, since = now, time.Now()
		}
		return time.Since(since) > quiet
	})
}

func (h *harness) waitState(t *testing.T, rel string, want State) RepoView {
	t.Helper()
	eventually(t, 10*time.Second, rel+" to become "+string(want), func() bool {
		v := h.view(rel)
		return v.State == want && v.Busy == ""
	})
	return h.view(rel)
}

// TestEngineClassifiesAndSyncs walks every state through a real remote:
// the first fetch reveals the commits pushed elsewhere, Sync fast-forwards and
// pushes exactly where that is safe and leaves dirty and diverged repos alone.
func TestEngineClassifiesAndSyncs(t *testing.T) {
	w := newWorld(t)
	behind := w.clone(t, "behind")
	ahead := w.clone(t, "ahead")
	dirty := w.clone(t, "dirty")
	diverged := w.clone(t, "diverged")
	w.clone(t, "clean")
	other := filepath.Join(filepath.Dir(w.root), "other")
	git(t, filepath.Dir(w.root), "clone", "-q", w.remote, other)

	commit(t, ahead, "local.txt", "mine\n")
	commit(t, diverged, "mine.txt", "mine\n")
	writeFile(t, filepath.Join(dirty, "README"), "edited\n")
	commit(t, other, "theirs.txt", "theirs\n")
	git(t, other, "push", "-q", "origin", "main")

	h := startEngine(t, w.root)
	// "ahead" is pushed by Sync below, which moves the remote: wait for the
	// first fetch to see the remote as it is now.
	h.waitState(t, "behind", StateBehind)
	h.waitState(t, "diverged", StateDiverged)
	if v := h.waitState(t, "ahead", StateDiverged); v.Ahead != 1 || v.Behind != 1 {
		t.Fatalf("ahead after the fetch: %+v", v) // the other clone pushed too
	}
	h.waitState(t, "dirty", StateDirty)
	h.waitState(t, "clean", StateBehind)

	// Make "ahead" strictly ahead: take the remote's commit first.
	git(t, ahead, "pull", "-q", "--rebase")
	h.waitState(t, "ahead", StateAhead)

	h.e.Sync(behind, ActionPull)
	v := h.waitState(t, "behind", StateInSync)
	if v.Err != "" {
		t.Fatalf("pull reported %q", v.Err)
	}
	if got := git(t, behind, "log", "-1", "--format=%s"); got != "change theirs.txt" {
		t.Errorf("behind HEAD is %q after the pull", got)
	}

	h.e.Sync(ahead, ActionPush)
	if v := h.waitState(t, "ahead", StateInSync); v.Err != "" {
		t.Fatalf("push reported %q", v.Err)
	}
	if got := git(t, other, "ls-remote", "origin", "main"); got[:40] != git(t, ahead, "rev-parse", "HEAD") {
		t.Errorf("remote main is not the pushed commit: %s", got)
	}

	headBefore := git(t, diverged, "rev-parse", "HEAD")
	h.e.Sync(diverged, ActionPull)
	h.e.Sync(dirty, ActionPull)
	h.waitState(t, "diverged", StateDiverged)
	h.waitState(t, "dirty", StateDirty)
	if git(t, diverged, "rev-parse", "HEAD") != headBefore {
		t.Error("Sync moved a diverged branch")
	}
	if data, _ := os.ReadFile(filepath.Join(dirty, "README")); string(data) != "edited\n" {
		t.Error("Sync touched uncommitted work")
	}
}

// TestEngineFollowsTheWatcher proves changes arrive without polling (the poll
// interval is an hour here), and that an idle engine publishes nothing.
func TestEngineFollowsTheWatcher(t *testing.T) {
	w := newWorld(t)
	repo := w.clone(t, "repo")
	h := startEngine(t, w.root)
	h.waitState(t, "repo", StateInSync)
	// Let the first fetch cycle and its status runs settle.
	h.settle(t)

	idle := h.publishes.Load()
	time.Sleep(1500 * time.Millisecond)
	if n := h.publishes.Load() - idle; n != 0 {
		t.Errorf("idle engine published %d times", n)
	}

	writeFile(t, filepath.Join(repo, "new.txt"), "x\n")
	h.waitState(t, "repo", StateDirty)

	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "c.txt", "c\n")
	h.waitState(t, "repo", StateAhead)

	// A repo cloned while the panel is open shows up by itself.
	w.clone(t, "fresh")
	h.waitState(t, "fresh", StateInSync)
}

func TestEngineConfigureIsIdempotent(t *testing.T) {
	w := newWorld(t)
	w.clone(t, "repo")
	h := startEngine(t, w.root)
	h.waitState(t, "repo", StateInSync)
	key := h.e.watchKey
	watch := h.e.watch
	h.e.Configure(Config{Roots: []string{w.root}, MaxDepth: 4, FetchInterval: time.Hour})
	if h.e.watch != watch || h.e.watchKey != key {
		t.Error("an unchanged config restarted the watcher")
	}
}

// TestEngineSkipsIgnoredDirectories: churn in a gitignored directory costs one
// check-ignore and then nothing, while a change elsewhere still lands.
func TestEngineSkipsIgnoredDirectories(t *testing.T) {
	w := newWorld(t)
	repo := w.clone(t, "repo")
	commit(t, repo, ".gitignore", "cache/\n")
	git(t, repo, "push", "-q", "origin", "main")
	writeFile(t, filepath.Join(repo, "cache", "seed"), "0\n")

	h := startEngine(t, w.root)
	h.waitState(t, "repo", StateInSync)
	h.settle(t)

	before := h.e.statusRuns()["repo"]
	for i := range 5 {
		writeFile(t, filepath.Join(repo, "cache", "deep", "f"), string(rune('a'+i)))
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	if n := h.e.statusRuns()["repo"] - before; n != 0 {
		t.Errorf("writes to an ignored directory caused %d status runs", n)
	}

	writeFile(t, filepath.Join(repo, "src", "main.go"), "package main\n")
	h.waitState(t, "repo", StateDirty)
}

func TestIgnoredLocked(t *testing.T) {
	now := time.Now()
	r := &repo{ignores: map[string]ignoreVerdict{
		"build":     {ignored: true, at: now},
		"src":       {ignored: false, at: now},
		"old":       {ignored: true, at: now.Add(-2 * ignoreTTL)},
		".claude/p": {ignored: true, at: now},
	}}
	for rel, want := range map[string][2]bool{
		"build":       {true, true},
		"build/x/y":   {true, true}, // below an ignored dir
		"src":         {false, true},
		"src/pkg":     {false, false}, // a tracked parent says nothing about a child
		"old":         {false, false}, // expired
		".claude/p/x": {true, true},
		".claude":     {false, false},
		"buildx":      {false, false}, // prefix of a name is not an ancestor
	} {
		ignored, ok := r.ignoredLocked(rel, now)
		if ignored != want[0] || ok != want[1] {
			t.Errorf("ignoredLocked(%q) = %v, %v; want %v, %v", rel, ignored, ok, want[0], want[1])
		}
	}
}
