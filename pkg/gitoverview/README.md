# gitoverview — the engine behind the "Repos" block

Which local git repos are out of sync, kept current without polling. The
`gitoverview` block (`frontend/app/view/gitoverview/`) calls
`GitOverviewStartCommand` with its folders (block meta `gitoverview:roots`,
`gitoverview:ignore`, `gitoverview:maxdepth`, `gitoverview:fetchminutes`);
`service.go` keeps one engine per block in `wavesrv`, publishes every change
as `Event_GitOverview` scoped to the block and stops the engine on
`Event_BlockClose`. Nothing runs while no Repos block exists.

## Pieces

```
FSEvents / inotify ──► Engine.changed ──► pathIndex.lookup ──► ignore cache ──► requestStatus
                                                                                     │ debounce, min gap
fetch cycle (15 min) ──► git fetch (stale only) ─────────────────────────────────────┤
                                                                                     ▼
                                                     2 workers ──► git status (low priority)
                                                                                     │
                                                       classify ──► RepoView ──► publish (if changed) ──► UI
```

- **Discovery** (`discover.go`) walks each root to depth 4 reading
  directory entries only. A `.git` directory is a repo; a `.git` file is a
  linked worktree or submodule, resolved to its own git dir and the shared
  common dir. Dependency and build directories, dot-directories and
  symlinks are not entered; roots and git dirs are symlink-resolved, since
  the watchers report real paths. Rediscovery runs every fetch cycle, on
  "Rescan and fetch", and 5 s after a change outside every known repo that can be a
  new one: a directory holding a `.git`, within maxDepth, not below a
  skipped or hidden directory and not on the ignore list. Churn elsewhere
  under a root (Finder metadata, dot directories, ignored repos) costs
  nothing.
- **Change detection** (`watch*.go`). macOS: one recursive FSEvents stream
  over the roots (plus any git dir outside them), kernel-coalesced with 1 s
  latency, zero cost while idle; a coalesced `MustScanSubDirs` rechecks
  only the repos below that path. Linux: inotify watches on every worktree
  directory git does not ignore (one `git ls-files --ignored --directory`
  per repo; nested repos have their own entry), on each git dir, the common
  dir and its `refs/` tree. New directories, in the worktree or in `refs/`,
  are watched as they appear. A repo may hold 4000 watches over its life and
  the panel a quarter of `max_user_watches`; a repo that does not fit
  releases all its watches and is polled every 2 minutes. Other OSes:
  everything is polled.
- **Mapping** (`pathIndex.lookup`). A changed directory belongs to the
  innermost worktree containing it. A change in a worktree's private git dir
  affects that worktree; one in the shared common dir (refs after a fetch)
  affects every worktree sharing it. Object, reflog, LFS, submodule and
  fsmonitor-cookie writes are noise, as is a `.git` of a repo discovery
  skipped.
- **Ignored directories.** A change below a worktree root is first checked
  against a per-repo cache of `git check-ignore` answers (10 min TTL, cleared
  when any `.gitignore` in a changed directory or `.git/info/exclude`
  changes). An ignored directory that still holds force-added tracked files
  is caught by the fetch cycle's full rescan. Unknown directories are batched into one
  `git check-ignore --stdin -z`. Churn in ignored directories — build
  output, caches, tool state inside a dotfiles repo — then costs a map
  lookup.
- **Scheduling** (`requestStatusLocked`). One pending timer per repo: a
  burst of events becomes one status run after 1.5 s, never closer than 5 s
  to the previous run of that repo; a change during a run re-runs once
  after it. At most two status calls run at once.
- **Git calls** (`gitexec.go`). `git status --porcelain=v2 --branch -z` with
  `GIT_OPTIONAL_LOCKS=0` (status neither refreshes nor locks the index, so it
  cannot wake the watcher and loop), `-c core.fsmonitor=false
  -c maintenance.auto=false -c gc.auto=0` (no daemon, no detached
  maintenance or gc), no prompt of any kind (`GIT_TERMINAL_PROMPT=0`, empty
  `GIT_ASKPASS`, `SSH_ASKPASS_REQUIRE=never`, ssh in BatchMode unless the
  user configured an ssh command), inherited repo-location variables
  (`GIT_DIR`, …) stripped, a new session so a timeout kills git's whole
  process group. Background calls run under `taskpolicy -b` (macOS:
  background QoS, efficiency cores, throttled I/O) or `nice -n 19 ionice -c 3`
  (Linux).
- **Fetch cycle.** First 3 s after the block opens, then every 15 minutes:
  fetch (four at a time, low priority) each repo with a remote whose
  `FETCH_HEAD` is older than 90 % of the interval — so reopening the block
  does not refetch — then re-read every status as a safety net for anything
  the watcher missed.
- **Publishing.** State changes schedule one publish 150 ms later; it is
  skipped when the overview equals the last one sent, so an idle engine
  sends no event.

## Sync rules

`classify` (`model.go`) gives exactly one state per repo, most urgent
first, and an action only in two cases:

| State | Condition | Action |
|---|---|---|
| behind | clean, tracking, behind > 0, ahead = 0 | `git merge --ff-only --no-overwrite-ignore --no-autostash @{upstream}` |
| ahead | clean, tracking, ahead > 0, behind = 0 | `git push --no-follow-tags <remote> refs/heads/<b>:<upstream ref>` |
| diverged, dirty, no upstream, upstream gone, detached, no commits | — | none; the row says why |

Sync always fetches and re-reads the status first, and acts only when the
fresh action equals the one the row showed when the user clicked: a row
that said Pull never pushes, even if the upstream was rewound meanwhile.
Pull refuses rather than overwrite a gitignored file the incoming commits
start tracking. Push sends exactly one explicit, non-forced refspec, which
overrides configured push refspecs, `push.default=matching` and mirror
remotes. Sync all acts on the rows as rendered and asks for confirmation
when any of them would push.

## Tests

`go test -race ./pkg/gitoverview/` runs the engine against real git repos
with a bare remote (`engine_test.go`, `safety_test.go`): every state, Pull
and Push, the safety rules above (ignored `.env`, a configured
`+refs/heads/*` push refspec, the clicked action), the watcher as the only
source of change, an idle engine publishing nothing, ignored directories and
non-repo churn costing no status run, symlinked roots and inherited
`GIT_DIR`. The Linux watcher runs natively on Linux, or on macOS with
`docker run --rm -v "$PWD":/src:ro -w /src golang:1.26 go test -race ./pkg/gitoverview/`.
`TestMeasureRealTree` reports the cost on a real tree:
`WGO_MEASURE_ROOT=~/src go test -run TestMeasureRealTree -v ./pkg/gitoverview/`
(no fetch, no sync).

## Cost (measured)

`TestMeasureRealTree` on an Apple-silicon MacBook, 48 repos, 2026-10-30:

| Phase | Wall | CPU |
|---|---|---|
| discovery | 0.3 s | in-process, directory reads only |
| first full scan | 1.7 s | 2.3 s of git (40–80 ms per repo) |
| idle, 60 s, other sessions editing two repos | — | 0.15–0.25 s git + 0.015 s own |

Before the ignore cache an idle minute cost about 1.4 s of git, mostly a
dotfiles repo reacting to tool writes in an ignored directory.
