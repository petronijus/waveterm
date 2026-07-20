# Backport ledger — `whoisjeremylam/waveterm-remote`

This fork carries cherry-picks from a **second** fork of Wave Terminal,
[`whoisjeremylam/waveterm-remote`](https://github.com/whoisjeremylam/waveterm-remote)
(git remote: `remote-fork`), on top of the upstream base (`wavetermdev/waveterm`).

This file is the **ledger**: what we took, what we deliberately did not take, and why.
Update it on every backport round so the next one doesn't have to re-derive the mapping.

## Sync pointers

| What | Value |
|------|-------|
| Last reviewed commit on `remote-fork/main` | `7ae1d393` (2026-07-19) |
| Last review date | 2026-07-20 |
| Previous review boundary | `72007f00` (2026-06-12) |

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
prefer *their* version when a conflict arises. Do not "improve" these locally unless you are
also prepared to maintain the divergence.

### Exceptions — where ours wins

| Area | Why ours stays |
|------|----------------|
| `pkg/wps/wps.go` event buffering | `44f1f317` re-implemented their buffering on our base and **fixes a reentrant-lock deadlock their version still has** (`getMatchingRouteIds_nolock`), and buffers a list per event type with TTL rather than a single event. Keep ours; drop their `PendingEvents map[string]*WaveEvent` hunks. |
| Job telemetry (`pkg/jobcontroller`) | `telemetry.GoRecordTEventWrap` calls are upstream's and absent from their fork; their diffs delete them as context. Always keep ours. |
| Terminal write batching (`termwrap.ts`) | `pendingWriteChunks` / flush timer is ours; their `dispose()` hunks must be merged, not taken wholesale. |
| Their connection dropdown, SCM widget, image rendering, file transfer | Features we don't carry — drop those hunks. |
| `.pi/` (their specs/journal) | Not carried. Drop on every pick. |

## Round 3 — 2026-07-20, fork-first migration (branch `feat/ssh-fork-first`)

Supersedes `feat/ssh-backport-2` entirely (contains all 6 of its picks plus the auth stack).
That branch can be deleted once this one lands.

Sequence: revert our divergence, then replay their line in chronological order.

| # | Ours | Theirs | Subject |
|---|------|--------|---------|
| 1 | `cd658501` | — | **Revert** `bcf806b3` (block-scoped auth prompts) |
| 2 | `061c0fa7` | `7982167e` | connection-scoped user input modal state |
| 3 | `69badf3d` | `56ceda2f` | connection-scoped dismissal, no timeout |
| 4 | `f05141c4` | `eddd42d6` | password modal dedup, caching, cross-host isolation |
| 5 | `a9d8b214` | `da8cb53a` | gate retry overlays on `CanAutoReconnect` |
| 6 | `8e7d106f` | `51645a74` | `CanAutoReconnect` tests, mockable config path |
| 7 | `520edeb8` | `37135636` | `UserInputModal` → `UserInputPrompt`, non-blocking panel |
| 8 | `007957b9` | `b10d91bd` | defer `clearPendingAuth`, fix unsafe keyboard cast |
| 9 | `6245e2da` | `490b4b88` | scope prompts to tabs, nil-pointer auth defaults |
| 10 | `2989ed1d` | `1101cdde` | remove debug noise, PW- prefixed logging |
| 11 | `91247def` | `271be5e9` | decouple prompt from connection lifecycle |
| 12 | `53b0971b` | `47e94598` | always use overlay, set `ConnName` in sshclient callbacks |
| 13 | `b595f81a` | `f805dd47` | `EnsureConnection` retries from `Status_Error` |
| 14 | `c7728be4` | `1123c7cb` | …plus remove redundant tab trigger |
| 15 | `b164d69e` | `20fb6728` | publish `controllerstatus done` on durable shell exit |
| 16 | `ac6365da` | `6b403784` | allow SSH connections without wsh |
| 17 | `73607329` | `9aacb9e7` | restore terminal rendering after sleep/resume |
| 18 | `90ac8c15` | `4f5a6451` | decouple wsh startup timeout from connect context |
| 19 | `45a354a9` | `634bdc27` | runtime auth-prompt tracking for auto-reconnect |
| 20 | `b526cadd` | `3cd17d3c` | per-job ctx + bounded retry in `onConnectionUp` |
| 21 | `f0bbec26` | `7ae1d393` | start reconnect scheduler for conns failing at startup |
| 22 | `c13cf78a` | — | **fork-local**: wire `StartupReconnectDurableShells` into `main-server` |

Note on #22: their `7ae1d393` exports the function, but the commit that *calls* it was never
backported here, so it would have landed as dead code. **Behaviour change**: durable shell blocks
on remote connections now connect at app start rather than on first tab switch.

Still deliberately skipped: their SCM/source-control widget and git push auth, image rendering
(local `feat/image-rendering`), remote file transfer, badge rotation (local `review/badge`),
connection dropdown.

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

| Ours | Theirs | Subject |
|------|--------|---------|
| `0c63327f` | `0cd6489b` | Fix crash on tab close after SSH session exit |
| `30e5b457` | `f8ea41d1` | ensure connection is alive before starting durable shell (#6) |
| `41d33b65` | `7ac402b5` | Fix/auto reconnect detection gaps (#10) |
| `384ef31e` | `4604df0d` | SSH handshake stall causes context deadline exceeded |
| `9b13b0fd` | `e42d1493` | handshake stall and reconnect scheduler gaps from #13 review |
| `d0e01506` | `a5e3adb7` | fire disconnect event before blocking cleanup (#15) |
| `6f3fb5ce` | `a183c32a` | run blocking close cleanup in goroutine (#15) |
| `7de6feea` | `938ac30d` | guard `waitForDisconnect` against stale client race (#16) |
| `afffaf39` | — | close `DomainSockListener` before Client to prevent CPU spin |
| `560ebc15` | `38fde94a` | reconnect improvements — fast reconnect + UI overlay + drain loop fix (#23) |
| `1d61d9de` | `c46ad70e` | SSH port forwarding (LocalForward/RemoteForward) (#24) |
| `85665fba` | `600f1f63` | prevent poisoned connection state when wsh fails to start (#28) |
| `dd5371bd` | `999f44a0` | Feature/port forwarding UI (#29) |
| `696f7778` | `72007f00` | skip `MacOSFirstClickHandler` on webview blocks (#32) |
| `f0fd8aca` | `26b1b34b` | Bug/tmux mouse reconnect (#3) |

## How to run the next round

```sh
git fetch remote-fork

# 1) what's new since the last reviewed commit (update the pointer above!)
git log --oneline --no-merges 7ae1d393..remote-fork/main

# 2) pick onto a fresh branch, with -x so the source SHA lands in the message
git worktree add ../waveterm-sshN -b feat/ssh-backport-N release
git cherry-pick -x <sha>
```

Always use `-x` — the recorded `(cherry picked from commit …)` line is what makes this ledger
reconstructible if it ever drifts. With the SSH area now fork-first, most picks should apply
cleanly; a conflict there usually means we drifted again and should be re-aligned to them.
