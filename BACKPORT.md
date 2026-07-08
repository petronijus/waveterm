# Backport ledger — `whoisjeremylam/waveterm-remote`

This fork carries cherry-picks from a **second** fork of Wave Terminal,
[`whoisjeremylam/waveterm-remote`](https://github.com/whoisjeremylam/waveterm-remote)
(git remote: `remote-fork`), on top of the upstream base (`wavetermdev/waveterm`).

This file is the **ledger**: what we took, what we deliberately did not take, and why.
Update it on every backport round so the next one doesn't have to re-derive the mapping.

## Sync pointers

| What                                       | Value                                             |
| ------------------------------------------ | ------------------------------------------------- |
| Last reviewed commit on `remote-fork/main` | `6d6128e5` (2026-08-12)                           |
| Last review date                           | 2026-08-16 (Round 4 reviewed, **not yet picked**) |
| Last _landed_ round                        | Round 3, boundary `7ae1d393` (2026-07-19)         |
| Previous review boundary                   | `72007f00` (2026-06-12)                           |
| Upstream base of `release`                 | `a4447c15`, merged in `9480714f` (2026-08-16)     |

## Policy: SSH/auth is fork-first

**In the SSH, connection and auth-prompt area, `waveterm-remote` is the source of truth.**
We track their implementation rather than maintaining a parallel one, so their fixes
cherry-pick cleanly instead of needing a hand port every round.

This was decided on 2026-07-20 after the alternative was tried and failed. Briefly:
`bcf806b3` (2026-07-01) had scoped SSH auth prompts to the originating **block**, while they
scope by **connection** on top of a password-cache / pending-auth layer. Their entire reconnect
line builds on that layer, so with our divergence in place most of their post-June work was
simply not cherry-pickable. Our version had also never been exercised against a live password
login (it shipped compile-verified only), and the bugs it was written to avoid had since been
fixed on their side. So `bcf806b3` was reverted and their stack adopted wholesale.

**Practical rule**: in `pkg/remote/`, `pkg/userinput/`, `pkg/wps/` (prompt buffering),
`frontend/app/modals/userinputprompt*`, `frontend/app/tab/tabuserinputpromptoverlay.tsx` —
prefer _their_ version when a conflict arises. Do not "improve" these locally unless you are
also prepared to maintain the divergence.

### Exceptions — where ours wins

| Area                                                                    | Why ours stays                                                                                                                                                                                                                                                                                                       |
| ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pkg/wps/wps.go` event buffering                                        | `44f1f317` re-implemented their buffering on our base and **fixes a reentrant-lock deadlock their version still has** (`getMatchingRouteIds_nolock`), and buffers a list per event type with TTL rather than a single event. Keep ours; drop their `PendingEvents map[string]*WaveEvent` hunks.                      |
| Job telemetry (`pkg/jobcontroller`)                                     | `telemetry.GoRecordTEventWrap` calls are upstream's and absent from their fork; their diffs delete them as context. Always keep ours.                                                                                                                                                                                |
| Terminal write batching (`termwrap.ts`)                                 | `pendingWriteChunks` / flush timer is ours; their `dispose()` hunks must be merged, not taken wholesale.                                                                                                                                                                                                             |
| Their connection dropdown, SCM widget, image rendering, file transfer   | Features we don't carry — drop those hunks.                                                                                                                                                                                                                                                                          |
| `.pi/` (their specs/journal)                                            | Not carried. Drop on every pick.                                                                                                                                                                                                                                                                                     |
| Their rename / rebrand to **RemoteTerm** (`673d1d72` and any successor) | **Permanent drop — never carried, never re-proposed.** Our identity is load-bearing: the `/opt/Wave` and `/opt/Wave (Dev)` install paths, the `waveterm` deb package name, the `pj` update channel and the `pj.N` version badge all key off it. Drop every rename hunk on sight; do not raise renaming as an option. |

## Round 4 — proposed 2026-08-16 (reviewed, **not yet picked**)

Boundary `7ae1d393` → `6d6128e5`, 73 commits on `remote-fork/main`, which reduce to
**19 first-parent units**: 15 direct commits and 4 PR merges.

Decisions taken for this round:

- **Upstream first.** `main` fast-forwarded to `upstream/main` `a4447c15` and `release` merged it
  (`9480714f`). Only `go.sum` conflicted; resolved with `go mod tidy`, `go mod verify` clean.
  Doing this first matters because their `main` already contains the same upstream bumps — without
  it we would resolve the same dependency conflicts twice.
- **Pick by PR, not commit-by-commit.** Round 3 replayed individual commits; that is not safe here.
  Their post-July history contains commits that do not build in isolation — `65a77c06` "repair broken
  JSX/merge leftovers blocking frontend build", `afee9768` "close renderPrompt arrow — repair
  build-blocking syntax", `bb2eecea` "remove leftover merge conflict marker". Replaying one by one
  means resolving conflicts against tree states that never compiled. Merge-picks
  (`cherry-pick -x -m 1`) land each PR as one reviewed unit; the ledger records both levels.
- **Scope is `remote-fork/main` only.** Their `odds-and-ends` branch (27 commits ahead, active
  2026-08-16) is explicitly out of scope for this round, including `172c9652` (nil-pointer deref in
  failed `RemoteForward` cleanup) which touches port forwarding we do carry. Revisit in Round 5.

### Pick list, in order

Status column is from a real dry run on a throwaway worktree cut from `release` `9480714f`
(picks applied in this order, `.pi/` dropped before evaluating conflicts).

| #   | Theirs     | Kind      | Subject                                                                           | Dry run                                           |
| --- | ---------- | --------- | --------------------------------------------------------------------------------- | ------------------------------------------------- |
| 1   | `b6f7487a` | single    | reset xterm.js `_isPaused` on resume to restore rendering (Phase 2G)              | clean                                             |
| 2   | `6f04028a` | single    | close previous stream reader on reconnect — output-loop goroutine leak (Phase 2H) | clean                                             |
| 3   | `cf039928` | single    | **disk-backed stream history** + round 1–3 edge-case hardening                    | clean                                             |
| 4   | `953a4961` | single    | round-4 review fixes for disk-backed stream history                               | clean                                             |
| 5   | `402acb77` | single    | preserve cached password on involuntary disconnect (`CloseInvoluntary`)           | clean                                             |
| 6   | `d519f484` | single    | visibility-driven reconnect on tab switch and app focus                           | **conflict**: `workspace.tsx`                     |
| 7   | `98bbd632` | single    | serialize password prompts per-window (one at a time)                             | clean                                             |
| 8   | `fd78d03a` | single    | scheduler bounds + early terminate on auth-failed / connection-refused            | clean                                             |
| 9   | `8f9c0a67` | single    | test: `CloseInvoluntary` preserves cached password                                | clean                                             |
| 10  | `0e284c8b` | merge #40 | **reconnect UX P0** (13 commits)                                                  | **conflict**: 12 files, 6 of them dropdown → drop |
| 11  | `3752222a` | merge #41 | **reconnect UX P1** (3 commits)                                                   | **conflict**: `wshrpctypes.go`                    |
| 12  | `e1437470` | merge #42 | **reconnect UX P2 / UX-2.x** (17 commits)                                         | **conflict**: `.gitignore`                        |
| 13  | `6d6128e5` | merge #43 | background terminal resize + term-file corruption fix (3 commits)                 | clean                                             |

What the three UX waves contain:

- **#40 P0** — sticky `SuppressAutoReconnect`, permanent-failure classification, stop-retry RPC,
  overlay stop-retry button, attention heartbeat, job-level honesty, hard-abort `Stop` cancelling all
  password prompts for a connection, cached password kept across network flaps, cold-start password
  prompt, soft-cancel for hung dials.
- **#41 P1** — gave-up overlay, stall heal-first, password queue, passphrase icons,
  incorrect-password feedback, drain progress, forced-reconnect stall heal, auth queue wait.
- **#42 P2 / UX-2.x** — overlay hysteresis for brief blips (2.1), flap-stable overlay chrome (2.2),
  visibility triggers for all view types (2.3), **Linux/Windows resume parity (2.4)**, SSH agent
  unavailability surfaced after sleep (2.5), accessibility (2.6), per-connection retry jitter (2.7),
  port-forward bind errors on reconnect (2.8), plus `f4a2a60c` term-file corruption on stream
  supersession/seq gaps and `f2877d67` ESC key + trailing-space trim on native copy.

### Prerequisite: disk-backed stream history (picks 3–4)

`pkg/jobmanager/streammanager.go` is **upstream-clean in `release`** — we have never carried their
disk-backed stream history. It is theirs alone: `diskEndSeq`, `diskReadPos`, `drainGen`, `diskFile`
and `drainDiskToCirBuf` appear 0× in `upstream/main` and 0× in `release`.

**#41 hard-depends on it.** Its UX-1.7 drain-progress work adds `GetDrainProgress()` reading
`sm.diskEndSeq`/`sm.diskReadPos` and updates counters inside `drainDiskToCirBuf`. Without picks 3–4
those hunks reference fields that do not exist, and #41 would have to be hand-edited apart — exactly
the divergence the fork-first policy exists to prevent. So picks 3–4 are taken as authored
(~530 lines of production code plus tests), which also gets us terminal history that survives to disk
across a reconnect — worth having on its own for durable shells.

### Drop list for this round

| What                                                                          | Why                                                                                                                                                                                                                                                                             |
| ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `817bc49d`                                                                    | their merge of upstream dependency bumps — we take those from `upstream` directly                                                                                                                                                                                               |
| `8024da00`, `a2114e05`, `83a357f7`                                            | new-tab **connection dropdown** with typeahead + frecency. Not carried; `connectiondropdown.tsx`, `connectiondropdown.scss`, `conn-suggestions.ts` and `conn-suggestions.test.ts` do not exist in `release` at all, so their hunks inside #40 are dropped by deleting the files |
| `b8090029`, `c1d4162f`, `6075de8c`, `10f9a6c0`                                | `.pi/` spec/journal only                                                                                                                                                                                                                                                        |
| `.pi/**` hunks in every pick                                                  | per standing policy                                                                                                                                                                                                                                                             |
| `d4d5a158`, `6f7ad3fc`                                                        | their macOS CI workflow                                                                                                                                                                                                                                                         |
| `05bebc21`                                                                    | removal of build artifacts they committed by accident                                                                                                                                                                                                                           |
| `AGENTS.md`, `.github/workflows/build-macos-ci.yml`, their `.gitignore` hunks | their repo furniture                                                                                                                                                                                                                                                            |
| `673d1d72` (rename to RemoteTerm)                                             | **permanent drop** — see the exceptions table above                                                                                                                                                                                                                             |

### Conflict map

Files where `release` has diverged from `upstream/main` **and** a pick touches them. Most divergence
is there because we already carry their code from Round 3, so those should merge in their favour per
the fork-first rule. The genuinely ours-vs-theirs cases are marked.

| File                                                                                                                                                                                                 | Divergence      | Note                                                                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `frontend/app/workspace/workspace.tsx`                                                                                                                                                               | +22/−1 **ours** | only conflict among picks 1–9; our own workspace wiring                                                                                                               |
| `pkg/remote/conncontroller/conncontroller.go`                                                                                                                                                        | +1032/−82       | ours = their Round 3 stack; conflict in #40 comes from the skipped dropdown commit's hunks                                                                            |
| `pkg/wshrpc/wshrpctypes.go`                                                                                                                                                                          | +83/−14         | #41 conflict; regenerate afterwards                                                                                                                                   |
| `pkg/wconfig/settingsconfig.go`, `pkg/wcore/workspace.go`, `frontend/app/block/blockenv.ts`, `frontend/app/store/keymodel.ts`                                                                        | mixed           | #40 conflicts, small                                                                                                                                                  |
| `emain/emain.ts` (+18), `emain/emain-window.ts` (+45), `frontend/app/view/term/termwrap.ts` (+276/−24), `frontend/app/view/term/term.tsx` (+68/−17), `pkg/blockcontroller/blockcontroller.go` (+152) | **ours**        | our background throttling / window-unmap repaint / write batching. **Did not conflict in the dry run** — good news, but re-verify, these are the ones that would hurt |

Caveat on the dry run: conflicts were auto-resolved by taking their side so the chain could continue,
so "clean" for a later pick means "applied without textual conflict on that base", not "verified
correct". Nothing was kept — the worktree and its `tmp/round4-dryrun` branch were deleted.

### After the picks

1. `task generate` — `wshrpctypes.go` changes in #40/#41/#42, so `frontend/types/gotypes.d.ts` and
   `pkg/wshrpc/wshclient/wshclient.go` must be regenerated, never hand-edited.
2. `tsc --noEmit` must stay at the **16-error baseline**, all in `frontend/preview/**` mocks
   (confirmed still exactly 16 on `release` `9480714f` after the upstream merge). `go vet ./pkg/... ./cmd/...` clean.
3. `go test -race -count=1` for `jobcontroller`, `jobmanager`, `remote`, `conncontroller`, `wps`,
   `userinput`, `streamclient`, `blockcontroller`. `TestTermActivity_OutputDrivenSpinner` is a known
   pre-existing failure — see the Round 3 notes.
4. Regression smoke suite: `node .kilocode/skills/run-desktop/smoke.mjs`.
5. **Live SSH password login test.** Still the outstanding gap carried over from Round 3 — the auth
   stack has never been exercised against a real password login here, and this round doubles down on
   it. Do not release Round 4 without it.

## Round 3 — 2026-07-20, fork-first migration (branch `feat/ssh-fork-first`)

Supersedes `feat/ssh-backport-2` entirely (contains all 6 of its picks plus the auth stack).
That branch can be deleted once this one lands.

Sequence: revert our divergence, then replay their line in chronological order.

| #   | Ours       | Theirs     | Subject                                                                 |
| --- | ---------- | ---------- | ----------------------------------------------------------------------- |
| 1   | `cd658501` | —          | **Revert** `bcf806b3` (block-scoped auth prompts)                       |
| 2   | `061c0fa7` | `7982167e` | connection-scoped user input modal state                                |
| 3   | `69badf3d` | `56ceda2f` | connection-scoped dismissal, no timeout                                 |
| 4   | `f05141c4` | `eddd42d6` | password modal dedup, caching, cross-host isolation                     |
| 5   | `a9d8b214` | `da8cb53a` | gate retry overlays on `CanAutoReconnect`                               |
| 6   | `8e7d106f` | `51645a74` | `CanAutoReconnect` tests, mockable config path                          |
| 7   | `520edeb8` | `37135636` | `UserInputModal` → `UserInputPrompt`, non-blocking panel                |
| 8   | `007957b9` | `b10d91bd` | defer `clearPendingAuth`, fix unsafe keyboard cast                      |
| 9   | `6245e2da` | `490b4b88` | scope prompts to tabs, nil-pointer auth defaults                        |
| 10  | `2989ed1d` | `1101cdde` | remove debug noise, PW- prefixed logging                                |
| 11  | `91247def` | `271be5e9` | decouple prompt from connection lifecycle                               |
| 12  | `53b0971b` | `47e94598` | always use overlay, set `ConnName` in sshclient callbacks               |
| 13  | `b595f81a` | `f805dd47` | `EnsureConnection` retries from `Status_Error`                          |
| 14  | `c7728be4` | `1123c7cb` | …plus remove redundant tab trigger                                      |
| 15  | `b164d69e` | `20fb6728` | publish `controllerstatus done` on durable shell exit                   |
| 16  | `ac6365da` | `6b403784` | allow SSH connections without wsh                                       |
| 17  | `73607329` | `9aacb9e7` | restore terminal rendering after sleep/resume                           |
| 18  | `90ac8c15` | `4f5a6451` | decouple wsh startup timeout from connect context                       |
| 19  | `45a354a9` | `634bdc27` | runtime auth-prompt tracking for auto-reconnect                         |
| 20  | `b526cadd` | `3cd17d3c` | per-job ctx + bounded retry in `onConnectionUp`                         |
| 21  | `f0bbec26` | `7ae1d393` | start reconnect scheduler for conns failing at startup                  |
| 22  | `c13cf78a` | —          | **fork-local**: wire `StartupReconnectDurableShells` into `main-server` |

Note on #22: their `7ae1d393` exports the function, but the commit that _calls_ it was never
backported here, so it would have landed as dead code. **Behaviour change**: durable shell blocks
on remote connections now connect at app start rather than on first tab switch.

Still not carried: their SCM/source-control widget and git push auth, remote file transfer, and
their connection dropdown. Re-fetch any of them from the `remote-fork` remote if wanted — there
is no need to keep local review branches around for it.

Two things once listed here as skipped have since landed by other routes: **inline terminal
images** (`0c0e4950`, `@xterm/addon-image`) and **badge rotation** (`e8db3ed0` — the `Rotation`
field in `pkg/baseds`, applied as a CSS transform by the tab badge). Check before assuming a
skipped item is still missing; two stale local branches were deleted for exactly this reason.

**Verification**: `go build ./...` and `go vet ./pkg/...` clean. `go test -race -count=1` green
for `jobcontroller`, `remote`, `conncontroller`, `wps`, `userinput`, `blockcontroller`.
`tsc --noEmit` produces 16 errors, all in `frontend/preview/**` mocks and **identical to the
`release` baseline** — no new type errors. Not yet runtime-tested against a live SSH password
login; that is the one thing this branch still needs.

`blockcontroller` has one failing test, `TestTermActivity_OutputDrivenSpinner` — a pre-existing
race in that test's own helper (`termactivity_test.go` `captureEvents`), reproducible on clean
`release`, unrelated to this work. Worth fixing separately.

## Round 1 — 2026-06-16 (initial SSH backport)

Cherry-picked as of `72007f00`. Mapping ours → theirs:

| Ours       | Theirs     | Subject                                                                     |
| ---------- | ---------- | --------------------------------------------------------------------------- |
| `0c63327f` | `0cd6489b` | Fix crash on tab close after SSH session exit                               |
| `30e5b457` | `f8ea41d1` | ensure connection is alive before starting durable shell (#6)               |
| `41d33b65` | `7ac402b5` | Fix/auto reconnect detection gaps (#10)                                     |
| `384ef31e` | `4604df0d` | SSH handshake stall causes context deadline exceeded                        |
| `9b13b0fd` | `e42d1493` | handshake stall and reconnect scheduler gaps from #13 review                |
| `d0e01506` | `a5e3adb7` | fire disconnect event before blocking cleanup (#15)                         |
| `6f3fb5ce` | `a183c32a` | run blocking close cleanup in goroutine (#15)                               |
| `7de6feea` | `938ac30d` | guard `waitForDisconnect` against stale client race (#16)                   |
| `afffaf39` | —          | close `DomainSockListener` before Client to prevent CPU spin                |
| `560ebc15` | `38fde94a` | reconnect improvements — fast reconnect + UI overlay + drain loop fix (#23) |
| `1d61d9de` | `c46ad70e` | SSH port forwarding (LocalForward/RemoteForward) (#24)                      |
| `85665fba` | `600f1f63` | prevent poisoned connection state when wsh fails to start (#28)             |
| `dd5371bd` | `999f44a0` | Feature/port forwarding UI (#29)                                            |
| `696f7778` | `72007f00` | skip `MacOSFirstClickHandler` on webview blocks (#32)                       |
| `f0fd8aca` | `26b1b34b` | Bug/tmux mouse reconnect (#3)                                               |

## How to run the next round

```sh
git fetch upstream && git fetch remote-fork

# 0) upstream first — their main already carries the same bumps, so doing this
#    afterwards means resolving the same dependency conflicts twice
git checkout main && git merge --ff-only upstream/main && git push origin main
git checkout release && git merge main        # go.sum conflicts -> go mod tidy

# 1) what's new since the last reviewed commit (update the pointer above!)
#    --first-parent collapses their PR branches into reviewable units
git log --oneline --first-parent <last-boundary>..remote-fork/main

# 2) pick onto a fresh branch, with -x so the source SHA lands in the message
git worktree add ../waveterm-sshN -b feat/ssh-backport-N release
git cherry-pick -x <sha>                      # direct commit
git cherry-pick -x -m 1 <merge-sha>           # whole PR as one unit
```

Always use `-x` — the recorded `(cherry picked from commit …)` line is what makes this ledger
reconstructible if it ever drifts. With the SSH area now fork-first, most picks should apply
cleanly; a conflict there usually means we drifted again and should be re-aligned to them.
