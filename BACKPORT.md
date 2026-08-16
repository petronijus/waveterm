# Backport ledger — `whoisjeremylam/waveterm-remote`

This fork carries cherry-picks from a **second** fork of Wave Terminal,
[`whoisjeremylam/waveterm-remote`](https://github.com/whoisjeremylam/waveterm-remote)
(git remote: `remote-fork`), on top of the upstream base (`wavetermdev/waveterm`).

This file is the **ledger**: what we took, what we deliberately did not take, and why.
Update it on every backport round so the next one doesn't have to re-derive the mapping.

## Sync pointers

| What                                       | Value                                         |
| ------------------------------------------ | --------------------------------------------- |
| Last reviewed commit on `remote-fork/main` | `6d6128e5` (2026-08-12)                       |
| Last review date                           | 2026-08-16                                    |
| Last _landed_ round                        | Round 4, boundary `6d6128e5` (2026-08-12)     |
| Previous review boundary                   | `7ae1d393` (2026-07-19)                       |
| Upstream base of `release`                 | `a4447c15`, merged in `9480714f` (2026-08-16) |

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

## Round 4 — 2026-08-16, reconnect UX waves (branch `feat/ssh-backport-4`)

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

### Pick list, as landed

Picked onto `feat/ssh-backport-4`, cut from `release` `9480714f`. All picks used `-x`, merges
used `-m 1`. `.pi/` was dropped from every pick.

| #   | Ours       | Theirs     | Kind      | Subject                                                                           |
| --- | ---------- | ---------- | --------- | --------------------------------------------------------------------------------- |
| 1   | `c5354b88` | `b6f7487a` | single    | reset xterm.js `_isPaused` on resume to restore rendering (Phase 2G)              |
| 2   | `ede68594` | `6f04028a` | single    | close previous stream reader on reconnect — output-loop goroutine leak (Phase 2H) |
| 3   | `49c4b074` | `cf039928` | single    | **disk-backed stream history** + round 1–3 edge-case hardening                    |
| 4   | `354a5816` | `953a4961` | single    | round-4 review fixes for disk-backed stream history                               |
| 5   | `b50d0611` | `402acb77` | single    | preserve cached password on involuntary disconnect (`CloseInvoluntary`)           |
| 6   | `838c7ac2` | `d519f484` | single    | visibility-driven reconnect on tab switch and app focus                           |
| 7   | `a52bb776` | `98bbd632` | single    | serialize password prompts per-window (one at a time)                             |
| 8   | `d181b3dc` | `fd78d03a` | single    | scheduler bounds + early terminate on auth-failed / connection-refused            |
| 9   | `50d5ebca` | `8f9c0a67` | single    | test: `CloseInvoluntary` preserves cached password                                |
| 10  | `ef97fbb3` | `0e284c8b` | merge #40 | **reconnect UX P0** (13 commits)                                                  |
| 11  | `3dc72e55` | `3752222a` | merge #41 | **reconnect UX P1** (3 commits)                                                   |
| 12  | `fd4a7be5` | `e1437470` | merge #42 | **reconnect UX P2 / UX-2.x** (17 commits)                                         |
| 13  | `daf64cdc` | `6d6128e5` | merge #43 | background terminal resize + term-file corruption fix (3 commits)                 |
| 14  | `64178815` | —          | fork      | regenerate wshrpc bindings (`task generate`)                                      |
| 15  | `d3b5280c` | —          | fork      | wire `tabData` in workspace, `ConnStatus` mocks, drop orphan dropdown test        |

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

### Conflict resolutions

Every conflict and how it was settled. Ten of thirteen picks needed hand work in at least one file.

| Pick | File                                      | Resolution                                                                                                                                                                                                                                                               |
| ---- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 6    | `frontend/app/workspace/workspace.tsx`    | Took theirs. They **moved** `TabUserInputPromptOverlay` from `tabcontent.tsx` to workspace level (their `271be5e9` = our Round 3 `91247def`, where the move was not carried). Removed our duplicate render + now-dead import from `tabcontent.tsx` so it renders once.   |
| 10   | `conncontroller.go` hunk 1                | Took their sticky-suppress block and their `errorSubCode` naming; kept **our** `isContextError`, retargeted our telemetry's `ConnSubErrorCode` to `errorSubCode`.                                                                                                        |
| 10   | `conncontroller.go` hunk 4                | Kept **both** — our `conn:connect` telemetry _and_ their `conn:authpromptused` persist.                                                                                                                                                                                  |
| 10   | `conncontroller.go` hunk 5 + `SSHConn`    | Took theirs; had to re-add the `ConnectCount int64` struct field and `ConnectCount: connectCount` literal, which arrive only via the skipped dropdown commit but are referenced by #40 code that merged clean.                                                           |
| 10   | `pkg/wconfig/settingsconfig.go`           | Merged: our stall fields + their `ConnConnectCount`, `ConnLastConnectTime`, `ConnAuthPromptUsed`.                                                                                                                                                                        |
| 10   | `pkg/wcore/workspace.go`                  | Kept our telemetry; dropped their `RecordConnectionUsage(connName)` call and its import — their `CreateTab` has an extra `connName` parameter that only the dropdown adds. **`RecordConnectionUsage` is therefore never called in our tree and `ConnectCount` stays 0.** |
| 10   | `frontend/app/block/blockenv.ts`          | Kept our `ActivityCommand`; their `ConnStopAutoRetryCommand` / `JobControllerReconnectJobCommand` additions merged clean.                                                                                                                                                |
| 10   | `frontend/app/store/keymodel.ts`          | Kept ours (`createTab()`); their `Cmd:t` change toggles the dropdown.                                                                                                                                                                                                    |
| 10   | `conntypeahead.tsx`, `typeaheadmodal.tsx` | Kept ours wholesale. Their `conntypeahead.tsx` imports `conn-suggestions`, which we drop; `typeaheadmodal.tsx` only gains a `showFilter` prop that nothing in our tree would pass.                                                                                       |
| 10   | `conncontroller_test.go`                  | Took theirs (both hunks are pure additions).                                                                                                                                                                                                                             |
| 11   | `pkg/wshrpc/wshrpctypes.go`               | Dropped the whole 122-line block — it is their **Git/SCM types** (`CommandGitStatusData` … `CommandGitSaveCredentialsData`), a feature we do not carry.                                                                                                                  |
| 11   | `conncontroller.go`                       | Took theirs — UX-1.4 "Incorrect password — please try again." prompt copy.                                                                                                                                                                                               |
| 12   | `.gitignore`, `keymodel.ts`               | Kept ours both times: their repo furniture, and our `recordTEvent` import.                                                                                                                                                                                               |

Two fork-local follow-ups were needed and are commit `d3b5280c`:

- `workspace.tsx` had no `tabData` — added the `tabOref`/`tabAtom`/`tabData` hooks their tree already
  had. Missing these was a genuine breakage introduced by the pick-6 resolution, caught by `tsc`.
- `ConnStatus` gained `connectcount` / `lastconnecttime`, so `makeDefaultConnStatus` in
  `frontend/app/store/global.ts` had to set them; and `connectiondropdown.test.ts` arrived with #40
  importing a module we drop, so it was deleted.

`emain/emain.ts`, `emain/emain-window.ts`, `termwrap.ts`, `term.tsx` and `blockcontroller.go` — our
own background throttling, window-unmap repaint and write batching — **did not conflict at all**.

### Verification

| Check                        | Result                                                                                                                               |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `go vet ./pkg/... ./cmd/...` | clean                                                                                                                                |
| `tsc --noEmit`               | **16 errors = baseline**, all in `frontend/preview/**` mocks                                                                         |
| `go test -race -count=1`     | `jobcontroller`, `jobmanager`, `remote`, `conncontroller`, `connparse`, `userinput`, `streamclient` — all **ok**                     |
| `blockcontroller`            | 3 failures: `TestTermActivity_OutputDrivenSpinner`, `_AgentQuietResolvesDoneNotThinking`, `_NonAgentQuietStillThinking`              |
| ↳ baseline check             | **identical 3 failures on clean `release` `9480714f`** — pre-existing, unrelated to this round (Round 3 notes listed only the first) |

Still outstanding before this ships:

1. Regression smoke suite: `node .kilocode/skills/run-desktop/smoke.mjs`.
2. **Live SSH password login test.** Still the gap carried over from Round 3 — the auth stack has
   never been exercised against a real password login here, and this round doubles down on it.
3. **Their 5s `SendData` timeout leaks a goroutine per timed-out send** (`mainserverconn.go`
   `routedDataSender.SendData` — the `go func()` stays blocked in `StreamDataCommand`). Their own
   spec acknowledges it and estimates ~500 goroutines/sec of output during a disconnect for a
   fast-output process; the suggested cap/context-cancel mitigation is **not implemented**. Worth our
   own test before trusting it under `dd`-class output.

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
