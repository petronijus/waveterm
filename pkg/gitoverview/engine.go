// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config is what the panel lets the user set.
type Config struct {
	Roots         []string
	MaxDepth      int
	Ignore        ignoreMatcher
	FetchInterval time.Duration
}

// Tuning. Status costs 40–80 ms of CPU per repo; everything below exists so
// that it runs only for repos that changed, at low priority, a few at a time.
const (
	statusWorkers     = 2  // background status calls in parallel
	fetchWorkers      = 4  // fetches are network-bound
	syncWorkers       = 3  // user-clicked syncs in parallel
	fetchFreshnessPct = 90 // a FETCH_HEAD younger than 90 % of the interval is fresh enough

	// ignoreTTL bounds how long a "git ignores this directory" answer is
	// trusted. Changes there (build output, caches, ~/.claude transcripts in a
	// dotfiles repo) then cost a map lookup instead of a status run.
	ignoreTTL = 10 * time.Minute
)

// timings are per engine so tests can shorten them.
type timings struct {
	debounce        time.Duration // let a burst of writes settle
	minStatusGap    time.Duration // per repo, however busy it is
	pollInterval    time.Duration // repos no watcher covers
	rediscoverDelay time.Duration
	firstFetchDelay time.Duration
	publishDelay    time.Duration
}

var defaultTimings = timings{
	debounce:        1500 * time.Millisecond,
	minStatusGap:    5 * time.Second,
	pollInterval:    2 * time.Minute,
	rediscoverDelay: 5 * time.Second,
	firstFetchDelay: 3 * time.Second,
	publishDelay:    150 * time.Millisecond,
}

type repo struct {
	loc     repoLoc
	view    RepoView
	timer   *time.Timer // pending status run
	running bool        // a status call is in flight
	again   bool        // something changed while it ran
	lastRun time.Time
	lastSig statusInfo
	runs    int // status calls so far, for TestMeasureRealTree

	ignores       map[string]ignoreVerdict // worktree-relative dir -> git's answer
	gitignores    map[string]time.Time     // .gitignore mtimes seen, by worktree-relative dir
	ignorePending map[string]bool          // dirs waiting for a check-ignore run
	ignoreT       *time.Timer
	ignoreRunning bool
}

// gitignoreChangedLocked reports whether the .gitignore in rel changed since
// it was last seen, which invalidates every cached answer for the repo. One
// stat per change in a directory git does not ignore.
func (r *repo) gitignoreChangedLocked(rel string) bool {
	var mtime time.Time
	if info, err := os.Stat(filepath.Join(r.loc.Root, filepath.FromSlash(rel), ".gitignore")); err == nil {
		mtime = info.ModTime()
	}
	seen, known := r.gitignores[rel]
	if known && seen.Equal(mtime) {
		return false
	}
	if r.gitignores == nil {
		r.gitignores = map[string]time.Time{}
	}
	r.gitignores[rel] = mtime
	return known
}

type ignoreVerdict struct {
	ignored bool
	at      time.Time
}

// ignoredLocked answers from the cache: true/false when known, ok=false when
// git has to be asked. Any fresh ignored ancestor makes rel ignored.
func (r *repo) ignoredLocked(rel string, now time.Time) (ignored, ok bool) {
	for i := 0; i <= len(rel); i++ {
		if i == len(rel) || rel[i] == '/' {
			if v, hit := r.ignores[rel[:i]]; hit && v.ignored && now.Sub(v.at) < ignoreTTL {
				return true, true
			}
		}
	}
	if v, hit := r.ignores[rel]; hit && now.Sub(v.at) < ignoreTTL {
		return v.ignored, true
	}
	return false, false
}

// Engine keeps the overview current. All state is guarded by mu; git runs
// outside the lock.
type Engine struct {
	publish func(Overview)
	tm      timings

	ctx    context.Context
	cancel context.CancelFunc

	statusSem chan struct{}
	fetchSem  chan struct{}
	syncSem   chan struct{}

	rediscoverMu sync.Mutex // one rediscover at a time; it swaps the watcher outside mu

	mu            sync.Mutex
	cfg           Config
	absRoots      []string
	configured    bool
	loopCancel    context.CancelFunc
	repos         map[string]*repo
	index         *pathIndex
	watch         watcher
	watchKey      string
	lastFetch     time.Time
	nextFetch     time.Time
	fetching      bool
	scanErr       string
	rediscoverT   *time.Timer
	candidates    map[string]bool // directories that may be new repos
	publishT      *time.Timer
	lastPublished Overview
	rediscovers   int      // completed rediscover runs, for tests
	overflows     int      // full rechecks the watcher asked for, for TestMeasureRealTree
	rescans       []string // coalesced subtrees the watcher reported, for TestMeasureRealTree
}

func NewEngine(publish func(Overview)) *Engine {
	return newEngine(publish, defaultTimings)
}

func newEngine(publish func(Overview), tm timings) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		publish:   publish,
		tm:        tm,
		ctx:       ctx,
		cancel:    cancel,
		statusSem: make(chan struct{}, statusWorkers),
		fetchSem:  make(chan struct{}, fetchWorkers),
		syncSem:   make(chan struct{}, syncWorkers),
		repos:     map[string]*repo{},
		index:     newPathIndex(nil),
	}
}

// Configure applies a config; an unchanged one is a no-op, so the UI may call
// it on every render-effect run.
func (e *Engine) Configure(cfg Config) {
	e.mu.Lock()
	if e.configured && reflect.DeepEqual(cfg, e.cfg) {
		e.mu.Unlock()
		return
	}
	if cfg.FetchInterval < time.Minute {
		cfg.FetchInterval = time.Minute
	}
	e.cfg, e.configured = cfg, true
	if e.loopCancel != nil {
		e.loopCancel()
	}
	loopCtx, cancel := context.WithCancel(e.ctx)
	e.loopCancel = cancel
	e.mu.Unlock()

	e.rediscover()
	go e.loop(loopCtx, cfg.FetchInterval)
}

func (e *Engine) Close() {
	e.cancel()
	e.mu.Lock()
	w := e.watch
	e.watch = nil
	for _, r := range e.repos {
		r.stopTimers()
	}
	e.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (r *repo) stopTimers() {
	if r.timer != nil {
		r.timer.Stop()
	}
	if r.ignoreT != nil {
		r.ignoreT.Stop()
	}
}

func (e *Engine) loop(ctx context.Context, interval time.Duration) {
	first := time.NewTimer(e.tm.firstFetchDelay)
	defer first.Stop()
	fetchTick := time.NewTicker(interval)
	defer fetchTick.Stop()
	pollTick := time.NewTicker(e.tm.pollInterval)
	defer pollTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			e.fetchCycle(false)
		case <-fetchTick.C:
			e.fetchCycle(false)
		case <-pollTick.C:
			e.pollUncovered()
		}
	}
}

// rediscover rescans the roots, keeps the state of repos still present,
// rebuilds the path index and, when the watched set changed, builds the new
// watcher outside the lock (inotify setup walks thousands of directories)
// and swaps it in.
func (e *Engine) rediscover() {
	e.rediscoverMu.Lock()
	defer e.rediscoverMu.Unlock()
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()

	roots := resolveRoots(cfg.Roots)
	locs, err := discover(roots, cfg.MaxDepth, cfg.Ignore)
	key := fmt.Sprint(roots, locs)

	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.absRoots = roots
	e.rediscovers++
	e.scanErr = ""
	if err != nil {
		e.scanErr = err.Error()
	}
	next := make(map[string]*repo, len(locs))
	var fresh []string
	for _, loc := range locs {
		if r, ok := e.repos[loc.Root]; ok {
			r.loc = loc
			r.view.Rel = loc.Rel
			next[loc.Root] = r
			continue
		}
		next[loc.Root] = &repo{loc: loc, view: RepoView{Path: loc.Root, Rel: loc.Rel, State: StateUnknown}}
		fresh = append(fresh, loc.Root)
	}
	for root, r := range e.repos {
		if _, ok := next[root]; !ok {
			r.stopTimers()
		}
	}
	e.repos = next
	e.index = newPathIndex(locs)
	for _, root := range fresh {
		e.requestStatusLocked(root, 0)
	}
	e.schedulePublishLocked()
	rewatch := key != e.watchKey || e.watch == nil
	e.mu.Unlock()

	if !rewatch {
		return
	}
	w, werr := newWatcher(roots, locs, watchSink{changed: e.changed, rescan: e.rescan, overflow: e.overflow})
	if werr != nil {
		log.Printf("watcher unavailable, polling every %s: %v", e.tm.pollInterval, werr)
		w = nil
	}
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		if w != nil {
			w.Close()
		}
		return
	}
	old := e.watch
	e.watch, e.watchKey = w, key
	e.schedulePublishLocked()
	e.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// resolveRoots expands "~", makes the roots absolute and resolves symlinks:
// FSEvents reports real paths, so a root reached through a symlink (or
// /tmp -> /private/tmp) would otherwise never match.
func resolveRoots(cfgRoots []string) []string {
	roots := make([]string, 0, len(cfgRoots))
	for _, r := range cfgRoots {
		abs, err := filepath.Abs(expandHome(r))
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		roots = append(roots, filepath.Clean(abs))
	}
	return roots
}

// considerCandidatesLocked queues directories that may have become repos.
// The check runs after rediscoverDelay: a clone creates its directory first
// and .git a moment later, and a burst of events collapses into one check.
func (e *Engine) considerCandidatesLocked(dirs []string) {
	if e.candidates == nil {
		e.candidates = map[string]bool{}
	}
	for _, d := range dirs {
		e.candidates[d] = true
	}
	if e.rediscoverT == nil {
		e.rediscoverT = time.AfterFunc(e.tm.rediscoverDelay, e.checkCandidates)
	}
}

// checkCandidates rediscovers if any queued directory now holds a .git.
func (e *Engine) checkCandidates() {
	e.mu.Lock()
	e.rediscoverT = nil
	dirs := e.candidates
	e.candidates = nil
	e.mu.Unlock()
	for d := range dirs {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			e.rediscover()
			return
		}
	}
}

// changed is the watcher callback.
func (e *Engine) changed(dirs []string) {
	e.mu.Lock()
	now := time.Now()
	var candidates []string
	for _, d := range dirs {
		affected, rel, known := e.index.lookup(d)
		if !known {
			if c, ok := e.newRepoCandidateLocked(d); ok {
				candidates = append(candidates, c)
			}
			continue
		}
		if rel != "" {
			e.worktreeChangedLocked(affected[0], rel, now)
			continue
		}
		if filepath.Base(d) == "info" {
			// .git/info/exclude may have changed: forget old ignore answers.
			for _, root := range affected {
				if r := e.repos[root]; r != nil {
					r.ignores = nil
				}
			}
		}
		for _, root := range affected {
			e.requestStatusLocked(root, e.tm.debounce)
		}
	}
	if len(candidates) > 0 {
		e.considerCandidatesLocked(candidates)
	}
	e.mu.Unlock()
}

// newRepoCandidateLocked decides whether a change outside every known repo
// can be a new repo that discovery would list: the directory holding a
// ".git" (or the changed directory itself) within maxDepth of a root, not
// below a skipped or hidden directory and not on the ignore list. Anything
// else — Finder metadata, dot directories, ignored or too-deep repos — must
// not trigger a rescan of the whole tree.
func (e *Engine) newRepoCandidateLocked(dir string) (string, bool) {
	for _, root := range e.absRoots {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if rel == "." {
			return "", false // the root itself; its children report their own events
		}
		segs := strings.Split(filepath.ToSlash(rel), "/")
		for i, seg := range segs {
			if seg == ".git" {
				segs = segs[:i]
				break
			}
		}
		if len(segs) == 0 || len(segs) > e.cfg.MaxDepth {
			return "", false
		}
		for _, seg := range segs {
			if skippedName(seg) {
				return "", false
			}
		}
		candRel := strings.Join(segs, "/")
		if e.cfg.Ignore.match(candRel) {
			return "", false
		}
		return filepath.Join(root, filepath.FromSlash(candRel)), true
	}
	return "", false
}

// rescan handles a watcher report that changes below dir were coalesced
// (FSEvents MustScanSubDirs): every repo at or below dir gets a status run.
func (e *Engine) rescan(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rescans = append(e.rescans, dir)
	affected, _, _ := e.index.lookup(dir)
	for _, root := range affected {
		e.requestStatusLocked(root, e.tm.debounce)
	}
	for root := range e.repos {
		if underAny(root, []string{dir}) {
			e.requestStatusLocked(root, e.tm.debounce)
		}
	}
}

// worktreeChangedLocked handles a change below a worktree root: nothing when
// git ignores that directory, a status run when it does not, and a batched
// check-ignore when the answer is not cached yet.
func (e *Engine) worktreeChangedLocked(root, rel string, now time.Time) {
	r := e.repos[root]
	if r == nil {
		return
	}
	if r.gitignoreChangedLocked(rel) {
		r.ignores = nil
	}
	if rel == "." {
		e.requestStatusLocked(root, e.tm.debounce)
		return
	}
	ignored, ok := r.ignoredLocked(rel, now)
	switch {
	case ok && ignored:
		return
	case ok:
		e.requestStatusLocked(root, e.tm.debounce)
		return
	}
	if r.ignorePending == nil {
		r.ignorePending = map[string]bool{}
	}
	r.ignorePending[rel] = true
	if r.ignoreT == nil && !r.ignoreRunning {
		r.ignoreT = time.AfterFunc(e.tm.debounce, func() { e.runIgnoreCheck(root) })
	}
}

func (e *Engine) runIgnoreCheck(root string) {
	e.mu.Lock()
	r := e.repos[root]
	if r == nil || e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	r.ignoreT = nil
	rels := make([]string, 0, len(r.ignorePending))
	for rel := range r.ignorePending {
		rels = append(rels, rel)
	}
	r.ignorePending = nil
	r.ignoreRunning = true
	e.mu.Unlock()

	select {
	case e.statusSem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	ignored, err := gitIgnored(e.ctx, root, rels)
	<-e.statusSem

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.repos[root] != r {
		return
	}
	r.ignoreRunning = false
	now := time.Now()
	for rel, v := range r.ignores {
		if now.Sub(v.at) >= ignoreTTL {
			delete(r.ignores, rel)
		}
	}
	if r.ignores == nil {
		r.ignores = map[string]ignoreVerdict{}
	}
	relevant := err != nil
	for _, rel := range rels {
		if err == nil {
			r.ignores[rel] = ignoreVerdict{ignored: ignored[rel], at: now}
		}
		if !ignored[rel] {
			relevant = true
		}
	}
	if relevant {
		e.requestStatusLocked(root, 0)
	}
	if len(r.ignorePending) > 0 {
		r.ignoreT = time.AfterFunc(e.tm.debounce, func() { e.runIgnoreCheck(root) })
	}
}

func (e *Engine) overflow() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.overflows++
	for root := range e.repos {
		e.requestStatusLocked(root, e.tm.debounce)
	}
}

func (e *Engine) pollUncovered() {
	e.mu.Lock()
	defer e.mu.Unlock()
	var uncovered map[string]bool
	if e.watch != nil {
		uncovered = e.watch.Uncovered()
	}
	for root := range e.repos {
		if e.watch == nil || uncovered[root] {
			e.requestStatusLocked(root, 0)
		}
	}
}

// requestStatusLocked schedules a status run for root. Requests coalesce: one
// pending timer per repo, never closer than minStatusGap to the previous run,
// and a change during a run or a sync re-runs once afterwards.
func (e *Engine) requestStatusLocked(root string, delay time.Duration) {
	r := e.repos[root]
	if r == nil {
		return
	}
	if r.running || r.view.Busy != "" {
		r.again = true
		return
	}
	if r.timer != nil {
		return
	}
	if gap := e.tm.minStatusGap - time.Since(r.lastRun); gap > delay {
		delay = gap
	}
	r.timer = time.AfterFunc(delay, func() { e.runStatus(root) })
}

func (e *Engine) runStatus(root string) {
	e.mu.Lock()
	r := e.repos[root]
	if r == nil || e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	r.timer = nil
	if r.running || r.view.Busy != "" {
		r.again = true
		e.mu.Unlock()
		return
	}
	r.running = true
	e.mu.Unlock()

	select {
	case e.statusSem <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	st, err := gitStatus(e.ctx, root, true)
	<-e.statusSem

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.repos[root] != r {
		return // dropped by a rediscover meanwhile
	}
	r.running = false
	r.lastRun = time.Now()
	r.runs++
	e.applyStatusLocked(r, st, err)
	if r.again {
		r.again = false
		e.requestStatusLocked(root, e.tm.debounce)
	}
	e.schedulePublishLocked()
}

func (e *Engine) applyStatusLocked(r *repo, st statusInfo, err error) {
	v := &r.view
	if err != nil {
		v.State, v.Action, v.Reason = StateError, ActionNone, err.Error()
		return
	}
	if st != r.lastSig {
		v.Err = "" // the repo moved on; an old sync error no longer applies
		r.lastSig = st
	}
	v.Branch, v.Upstream = st.Branch, st.Upstream
	v.Ahead, v.Behind = st.Ahead, st.Behind
	v.Changed = st.Changed + st.Untracked + st.Conflicts
	v.State, v.Action, v.Reason = classify(st)
	if info, err := os.Stat(filepath.Join(r.loc.GitDir, "logs", "HEAD")); err == nil {
		v.Activity = info.ModTime()
	}
}

// fetchCycle rediscovers, fetches every repo whose FETCH_HEAD is stale (all of
// them when forced) and re-reads every status: a periodic safety net in case
// the watcher missed something.
func (e *Engine) fetchCycle(force bool) {
	e.mu.Lock()
	if e.fetching {
		e.mu.Unlock()
		return
	}
	e.fetching = true
	interval := e.cfg.FetchInterval
	e.schedulePublishLocked()
	e.mu.Unlock()

	e.rediscover()

	e.mu.Lock()
	targets := make([]repoLoc, 0, len(e.repos))
	for _, r := range e.repos {
		if r.view.Busy == "" { // a running sync fetches by itself
			targets = append(targets, r.loc)
		}
	}
	e.mu.Unlock()

	fresh := time.Duration(int64(interval) * fetchFreshnessPct / 100)
	var wg sync.WaitGroup
	for _, loc := range targets {
		if !hasRemote(loc.CommonDir) || (!force && fetchedWithin(loc, fresh)) {
			e.mu.Lock()
			e.requestStatusLocked(loc.Root, 0)
			e.mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(loc repoLoc) {
			defer wg.Done()
			select {
			case e.fetchSem <- struct{}{}:
			case <-e.ctx.Done():
				return
			}
			err := gitFetch(e.ctx, loc.Root, true)
			<-e.fetchSem
			e.mu.Lock()
			if r := e.repos[loc.Root]; r != nil {
				r.view.FetchErr = ""
				if err != nil {
					r.view.FetchErr = err.Error()
				}
				e.requestStatusLocked(loc.Root, 0)
			}
			e.mu.Unlock()
		}(loc)
	}
	wg.Wait()

	e.mu.Lock()
	e.fetching = false
	e.lastFetch = time.Now()
	e.nextFetch = e.lastFetch.Add(interval)
	e.schedulePublishLocked()
	e.mu.Unlock()
}

// Refresh is the panel's "Fetch now": rediscover and fetch everything.
func (e *Engine) Refresh() { go e.fetchCycle(true) }

// fetchedWithin reports whether the last fetch is younger than d. FETCH_HEAD
// lives in the common dir; reopening the panel therefore does not refetch.
func fetchedWithin(loc repoLoc, d time.Duration) bool {
	info, err := os.Stat(filepath.Join(loc.CommonDir, "FETCH_HEAD"))
	return err == nil && time.Since(info.ModTime()) < d
}

func hasRemote(commonDir string) bool {
	cfg, err := os.ReadFile(filepath.Join(commonDir, "config"))
	return err == nil && bytes.Contains(cfg, []byte("[remote \""))
}

// Sync fetches root, then performs want — a fast-forward or a push — if,
// and only if, the fresh status calls for exactly that. The panel passes the
// action the user saw on the button: a row that said Pull never pushes, even
// if the upstream was rewound meanwhile. Anything else is left for a human.
func (e *Engine) Sync(root string, want Action) {
	if want != ActionPull && want != ActionPush {
		return
	}
	e.mu.Lock()
	r := e.repos[root]
	if r == nil || r.view.Busy != "" {
		e.mu.Unlock()
		return
	}
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	loc := r.loc
	r.view.Busy, r.view.Err = "fetching", ""
	e.schedulePublishLocked()
	e.mu.Unlock()

	go func() {
		select {
		case e.syncSem <- struct{}{}:
		case <-e.ctx.Done():
			return
		}
		defer func() { <-e.syncSem }()
		errMsg := e.syncSteps(r, loc, want)

		st, err := gitStatus(e.ctx, root, false)
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.repos[root] != r {
			return
		}
		r.view.Busy = ""
		r.lastRun = time.Now()
		e.applyStatusLocked(r, st, err)
		r.view.Err = errMsg
		r.lastSig = st
		if r.again {
			r.again = false
			e.requestStatusLocked(root, e.tm.debounce)
		}
		e.schedulePublishLocked()
	}()
}

func (e *Engine) syncSteps(r *repo, loc repoLoc, want Action) string {
	root := loc.Root
	if hasRemote(loc.CommonDir) {
		if err := gitFetch(e.ctx, root, false); err != nil {
			return "fetch: " + err.Error()
		}
		e.setFetchErr(r, "")
	}
	st, err := gitStatus(e.ctx, root, false)
	if err != nil {
		return "status: " + err.Error()
	}
	if _, action, _ := classify(st); action != want {
		return "not synced: the repo changed since the panel showed it — check the row and try again"
	}
	switch want {
	case ActionPull:
		e.setBusy(r, "pulling")
		if err := gitFastForward(e.ctx, root); err != nil {
			return "pull: " + err.Error()
		}
	case ActionPush:
		e.setBusy(r, "pushing")
		if err := gitPush(e.ctx, root, st.Branch); err != nil {
			return "push: " + err.Error()
		}
	}
	return ""
}

func (e *Engine) setBusy(r *repo, busy string) {
	e.mu.Lock()
	r.view.Busy = busy
	e.schedulePublishLocked()
	e.mu.Unlock()
}

func (e *Engine) setFetchErr(r *repo, msg string) {
	e.mu.Lock()
	r.view.FetchErr = msg
	e.mu.Unlock()
}

// SyncTarget is one row Sync all acts on, with the action the user saw.
type SyncTarget struct {
	Root   string
	Action Action
}

// SyncAll syncs exactly the rows the user confirmed, each only with the
// action it showed.
func (e *Engine) SyncAll(targets []SyncTarget) {
	for _, t := range targets {
		e.Sync(t.Root, t.Action)
	}
}

func (e *Engine) schedulePublishLocked() {
	if e.publishT == nil {
		e.publishT = time.AfterFunc(e.tm.publishDelay, e.doPublish)
	}
}

func (e *Engine) doPublish() {
	e.mu.Lock()
	e.publishT = nil
	ov := e.snapshotLocked()
	if reflect.DeepEqual(ov, e.lastPublished) {
		e.mu.Unlock()
		return
	}
	e.lastPublished = ov
	e.mu.Unlock()
	if e.ctx.Err() == nil {
		e.publish(ov)
	}
}

// Snapshot returns the current overview (for tests and the first render).
func (e *Engine) Snapshot() Overview {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *Engine) snapshotLocked() Overview {
	views := make([]RepoView, 0, len(e.repos))
	for _, r := range e.repos {
		views = append(views, r.view)
	}
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.needsAttention() != b.needsAttention() {
			return a.needsAttention()
		}
		if severity[a.State] != severity[b.State] {
			return severity[a.State] < severity[b.State]
		}
		return a.Rel < b.Rel
	})
	ov := Overview{
		Repos:     views,
		Watching:  "polling",
		LastFetch: e.lastFetch,
		NextFetch: e.nextFetch,
		Fetching:  e.fetching,
		ScanErr:   e.scanErr,
	}
	if e.watch != nil {
		ov.Watching = e.watch.Mode()
		if n := len(e.watch.Uncovered()); n > 0 && ov.Watching != "polling" {
			ov.Watching += fmt.Sprintf(" (%d polled)", n)
		}
	}
	return ov
}

// actionable lists the rows Sync all would touch: their targets, and the
// display names of the pulls and pushes for the confirmation.
func actionable(views []RepoView) (targets []SyncTarget, pulls, pushes []string) {
	for _, v := range views {
		switch {
		case v.Busy != "":
		case v.Action == ActionPull:
			pulls = append(pulls, v.Rel)
			targets = append(targets, SyncTarget{Root: v.Path, Action: ActionPull})
		case v.Action == ActionPush:
			pushes = append(pushes, v.Rel)
			targets = append(targets, SyncTarget{Root: v.Path, Action: ActionPush})
		}
	}
	slices.Sort(pulls)
	slices.Sort(pushes)
	return targets, pulls, pushes
}

// statusRuns reports how many status calls each repo has had.
func (e *Engine) statusRuns() map[string]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]int, len(e.repos))
	for _, r := range e.repos {
		out[r.view.Rel] = r.runs
	}
	return out
}
