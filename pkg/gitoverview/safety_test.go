// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestPullNeverOverwritesIgnoredFile: when the incoming commits start
// tracking a path that is a gitignored local file (a .env), Pull refuses
// instead of replacing the user's file.
func TestPullNeverOverwritesIgnoredFile(t *testing.T) {
	w := newWorld(t)
	mine := w.clone(t, "mine")
	writeFile(t, filepath.Join(mine, ".git", "info", "exclude"), ".env\n")
	writeFile(t, filepath.Join(mine, ".env"), "SECRET=mine\n")
	other := w.clone(t, "other")
	commit(t, other, ".env", "SECRET=placeholder\n")
	git(t, other, "push", "-q", "origin", "main")

	h := startEngine(t, w.root)
	h.waitState(t, "mine", StateBehind)
	h.e.Sync(mine, ActionPull)
	v := h.waitState(t, "mine", StateBehind)
	if !strings.Contains(v.Err, "pull:") {
		t.Errorf("expected the pull to be refused, row error is %q", v.Err)
	}
	if data, _ := os.ReadFile(filepath.Join(mine, ".env")); string(data) != "SECRET=mine\n" {
		t.Fatalf("the ignored .env was overwritten: %q", data)
	}
}

// TestPushIgnoresConfiguredForceRefspec: a forced, all-branches push
// refspec in the repo's config must not turn Push into a force push of
// other branches; only the measured branch goes to its upstream.
func TestPushIgnoresConfiguredForceRefspec(t *testing.T) {
	w := newWorld(t)
	mine := w.clone(t, "mine")
	other := w.clone(t, "other")
	git(t, other, "checkout", "-q", "-b", "side")
	commit(t, other, "side.txt", "theirs\n")
	git(t, other, "push", "-q", "origin", "side")
	sideTip := git(t, other, "rev-parse", "HEAD")

	// mine has a stale local "side" that would rewind the remote if pushed.
	git(t, mine, "branch", "side", "origin/main")
	git(t, mine, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	git(t, mine, "config", "push.followTags", "true")
	git(t, mine, "tag", "local-tag")
	commit(t, mine, "mine.txt", "mine\n")

	h := startEngine(t, w.root)
	h.waitState(t, "mine", StateAhead)
	h.e.Sync(mine, ActionPush)
	if v := h.waitState(t, "mine", StateInSync); v.Err != "" {
		t.Fatalf("push reported %q", v.Err)
	}
	if got := strings.Fields(git(t, other, "ls-remote", "origin", "refs/heads/side"))[0]; got != sideTip {
		t.Errorf("remote side moved to %s; the configured +refspec was used", got)
	}
	if out := git(t, other, "ls-remote", "--tags", "origin"); out != "" {
		t.Errorf("tags were pushed: %s", out)
	}
}

// TestSyncActsOnlyOnTheClickedAction: a row that showed Pull never pushes,
// whatever the fresh status says.
func TestSyncActsOnlyOnTheClickedAction(t *testing.T) {
	w := newWorld(t)
	mine := w.clone(t, "mine")
	commit(t, mine, "mine.txt", "mine\n")

	h := startEngine(t, w.root)
	h.waitState(t, "mine", StateAhead)
	h.e.Sync(mine, ActionPull) // as if the row had said Pull a moment ago
	v := h.waitState(t, "mine", StateAhead)
	if !strings.Contains(v.Err, "changed since") {
		t.Errorf("row error %q, want the state-changed message", v.Err)
	}
	if out := git(t, mine, "rev-list", "--count", "origin/main..HEAD"); out != "1" {
		t.Errorf("the commit was pushed (%s left ahead)", out)
	}
	h.e.Sync(mine, ActionNone) // not an action: ignored
	if h.view("mine").Busy != "" {
		t.Error("Sync with no action started work")
	}
}

// TestNoRediscoverOnNonRepoChurn: changes in dot dirs, plain directories and
// ignored repos under a root never rescan the tree; a new clone does.
func TestNoRediscoverOnNonRepoChurn(t *testing.T) {
	w := newWorld(t)
	w.clone(t, "repo")
	w.clone(t, "hidden-by-config")
	fastLocal := fastTimings
	h := &harness{}
	h.e = newEngine(func(Overview) { h.publishes.Add(1) }, fastLocal)
	t.Cleanup(h.e.Close)
	h.e.Configure(Config{Roots: []string{w.root}, MaxDepth: 4, FetchInterval: time.Hour, Ignore: ignoreMatcher{"hidden-by-config"}})
	h.waitState(t, "repo", StateInSync)
	h.settle(t)

	count := func() int { h.e.mu.Lock(); defer h.e.mu.Unlock(); return h.e.rediscovers }
	before := count()
	for i := range 4 {
		writeFile(t, filepath.Join(w.root, ".claude", "projects", "s.jsonl"), strings.Repeat("x", i+1))
		writeFile(t, filepath.Join(w.root, "notes", "todo.txt"), strings.Repeat("y", i+1))
		writeFile(t, filepath.Join(w.root, "hidden-by-config", "f.txt"), strings.Repeat("z", i+1))
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	if n := count() - before; n != 0 {
		t.Errorf("non-repo churn caused %d rediscovers", n)
	}
	w.clone(t, "fresh")
	h.waitState(t, "fresh", StateInSync)
}

// TestSymlinkedRoot: a root reached through a symlink is watched by its real
// path, so changes still arrive.
func TestSymlinkedRoot(t *testing.T) {
	w := newWorld(t)
	repo := w.clone(t, "repo")
	link := filepath.Join(t.TempDir(), "dev-link") // not symlink-resolved on purpose
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	h := startEngine(t, link)
	h.waitState(t, "repo", StateInSync)
	h.settle(t)
	writeFile(t, filepath.Join(repo, "new.txt"), "x\n")
	h.waitState(t, "repo", StateDirty)
}

// TestStatusNeverStartsFsmonitor: a repo with core.fsmonitor=true must not
// get a daemon from the panel's status calls.
func TestStatusNeverStartsFsmonitor(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("git's builtin fsmonitor exists on macOS and Windows only")
	}
	w := newWorld(t)
	repo := w.clone(t, "repo")
	git(t, repo, "config", "core.fsmonitor", "true")
	if _, err := gitStatus(context.Background(), repo, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "fsmonitor--daemon")); err == nil {
		git(t, repo, "fsmonitor--daemon", "stop")
		t.Fatal("status started an fsmonitor daemon")
	}
}

// TestGitEnvIgnoresInheritedRepoVariables: GIT_DIR and friends from the
// environment Wave was started in must not redirect our calls.
func TestGitEnvIgnoresInheritedRepoVariables(t *testing.T) {
	w := newWorld(t)
	repo := w.clone(t, "repo")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "nowhere"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	st, err := gitStatus(context.Background(), repo, false)
	if err != nil || st.Branch != "main" {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

// TestSyncDuringRediscover runs syncs while fetch cycles rediscover, for the
// race detector.
func TestSyncDuringRediscover(t *testing.T) {
	w := newWorld(t)
	repos := []string{w.clone(t, "a"), w.clone(t, "b")}
	for _, r := range repos {
		commit(t, r, "x.txt", r)
	}
	h := startEngine(t, w.root)
	h.waitState(t, "a", StateAhead)
	h.waitState(t, "b", StateAhead)
	for range 3 {
		h.e.Refresh()
		h.e.SyncAll([]SyncTarget{{Root: repos[0], Action: ActionPush}, {Root: repos[1], Action: ActionPush}})
	}
	// Both commit to main of the same remote, so exactly one push wins; the
	// loser stays "ahead" with a push error until the next fetch shows it
	// diverged. What matters here: no row is left busy, and -race is quiet.
	eventually(t, 20*time.Second, "both syncs to finish", func() bool {
		a, b := h.view("a"), h.view("b")
		return a.Busy == "" && b.Busy == "" && (a.State == StateInSync) != (b.State == StateInSync)
	})
}
