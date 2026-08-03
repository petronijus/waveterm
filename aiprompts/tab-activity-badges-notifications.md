# Tab activity badges & notifications — how the whole system works

Reference for the terminal activity indicator (tab spinner / ✓ / 💬), the badge
plumbing underneath it, and the OS notifications built on top. Read this before
touching any of it — the logic is spread across backend + frontend and is easy to
misread from any single file.

## The three questions the system answers per terminal block

1. **Is something running / producing output?** → *working* (spinner) /
   *thinking* (spinner — command still running, output paused)
2. **Is an AI agent waiting for MY input?** → *waiting* (💬 comment-dots, amber)
3. **Did a command finish?** → *done* (✓ green / ✗ red by exit code)

All detection is **backend-side** (wavesrv) so it works for background tabs whose
renderer/xterm is unmounted. The frontend only renders badges and fires OS
notifications.

## Data flow (end to end)

```
pty output bytes
  → blockcontroller.HandleAppendBlockFile (single choke point, blockcontroller.go)
  → FeedTermActivity → termActivityTracker state machine   (termactivity.go)
  → on each state transition, publish TWO event streams:
      1. Event_TermActivity  (scoped to the block)
         → frontend term-activity.ts → OS notifications (focus-aware)
      2. Event_Badge         (via publishActivityBadge)
         → wcore/badge.go   backend in-memory badge store (survives renderer
                            reloads; GetAllBadges hydrates a fresh renderer)
         → frontend store/badge.ts (jotai atoms per oref)
         → tab.tsx / vtab.tsx → tabbadges.tsx renders the badge on the tab

user input bytes (keystrokes → pty)
  → shellcontroller.go / durableshellcontroller.go → FeedTermUserInput
  → releases the sticky "waiting" state (see below)
```

## The backend state machine — `pkg/blockcontroller/termactivity.go`

One `termActivityTracker` per block (`activityTrackers` map). Inputs:

| Input | Source | Effect |
| --- | --- | --- |
| OSC 16162 `C` (+cmd64) | shell integration preexec | `startCommand`: `running=true`, classify command (agent? server?), state → **working** immediately |
| OSC 16162 `D` (exitcode) / `A` | precmd / new prompt | `finishCommand`: state → **done** (✓/✗), always visible for a C→D tracked command |
| OSC 16162 `R` | shell reset | `cancelCommand`: state → **none**, clears everything |
| raw output volume | every pty chunk | `markOutput`: "sustained output ⇒ working" heuristic (constants below) |
| BEL / OSC 9 (non-`9;4`) | agent "your turn" signal | `markWaiting`: state → **waiting** — only if an agent is identified (tracked command or process-tree probe); non-agent bells ignored |
| user keystrokes | `FeedTermUserInput` | deliberate keypress (text/Enter/Tab, not arrows/escape replies) releases sticky `waiting` |
| `wsh agentstate waiting\|done` | agent lifecycle hooks via RPC | `SetExternalAgentState`: explicit state, beats all heuristics |
| block destroy / shell restart | `ResetTermActivity` | tracker torn down, badge cleared |

### Timing constants (must match the documented behavior)

| Const | Value | Meaning |
| --- | --- | --- |
| `cmdActivityDelay` | 1200 ms | ignore first output burst of a tracked command (quick commands don't flash) |
| `cmdActivitySustain` | 700 ms | output must flow this long before spinner turns on (output-driven path) |
| `cmdActivityGap` | 1000 ms | a quiet gap longer than this ends the continuous-output stretch |
| `cmdActivityIdle` | 2500 ms | command-tracked: quiet this long ⇒ **thinking** (spinner stays) |
| `cmdActivityDoneIdle` | 4000 ms | output-only (no C marker): quiet this long ⇒ **done** ✓ |

### The two spinner paths

- **Command-tracked** (`C` marker fired, `running=true`): spinner turns on at
  `startCommand` and the state can only leave via `D`/`A` (→ done), `R` (→ none),
  bell/agentstate (→ waiting), or idle. The idle-fire forks on what's running:
  - **non-agent command** (a build, tests): → **thinking** after 2.5 s — spinner
    stays; a running command never times out to done on its own.
  - **agent command** (`agentKind != ""`): → **done ✓** after the longer 4 s
    window (`cmdActivityDoneIdle`), with sticky `waiting` so the idle TUI's
    repaint dribble can't re-trip the spinner over the ✓. Rationale: agent TUIs
    repaint continuously while genuinely working (spinner animation, streaming),
    so prolonged full silence = turn over. This is the safety net for setups
    where the agent's bell/OSC 9 never arrives and no agentstate hook is wired —
    without it a quiet agent parked in *thinking* and the spinner spun for the
    life of the agent process. `running`/`agentKind` stay set: a later bell still
    flips to 💬 and the agent's eventual real `D` finalizes with the exit code.
    The done event's `DurationMs` is the turn length (anchored via `turnStartTs`
    when the spinner turned on), not time since agent launch.
- **Output-driven** (`outputDriven=true`, no C marker — broken preexec, durable
  session that outlived wavesrv, output from inside a TUI): spinner from the
  sustain heuristic; 4 s of silence ⇒ done ✓ (a later real `D` upgrades to true
  exit status).

### Command classification (all on the *normalized* command)

`normalizeCmd` strips `env`, `VAR=x` prefixes, and package-runner wrappers
(`npx`, `bunx`, `pnpm/yarn/bun exec|dlx`, `bundle exec`, `poetry/uv/... run`) —
so `npx vite` classifies as `vite`.

- **Agents** — `agentCommandRegexes`: `claude`, `gemini`, `codex`. Sets
  `agentKind`; enables the waiting state; `claude` additionally starts
  session-binding (`claudesession.go`, writes `claude:sessionid` block meta).
- **Servers** — `serverCommandRegexes` (npm/pnpm/yarn/bun dev|start|serve…,
  vite, next, uvicorn, rails s, `shopify theme dev`, `docker compose up`, …):
  a server's normal state is "runs forever", so **working/thinking badges are
  suppressed** for them at badge-publish time (`publishActivityBadge`). The
  *state machine* still runs; only the spinner badge is withheld. `task dev` /
  `make …` are NOT in the list.

### Agent identification fallback — `agentprobe.go`

When a bell arrives but no agent command is tracked (broken preexec, durable
session reattach), the pty's **process tree** is ground truth: BFS from the
block's local shell pid (capped 64 procs / depth 6, 10 s cache), match process
names/argv against the agent regexes. Remote blocks can't be probed.

### The sticky *waiting* state (critical, easy to break)

An idle agent TUI **repaints continuously** (claude idles at ~350 B/s in one
unbroken stretch) — no volume threshold can tell that dribble from real work.
Therefore:

- `waiting` **blocks the spinner** in `markOutput` (`!t.waiting` condition).
- Only a **deliberate keypress** releases it (`scanInputForUserAction`):
  printable chars, Enter, Tab. Arrow keys, focus events, bracketed-paste frames
  and the terminal's automatic escape replies (DSR/DA/OSC responses) do NOT —
  browsing an agent's menu must not release waiting; answering does.
- `SetExternalAgentState("done")` also leaves `waiting=true` on purpose: the
  agent idles at its prompt after the turn and its repaint dribble must not
  re-trip the spinner over the ✓.
- **Silence is deliberately NOT treated as waiting** — an agent running
  subagents/long tools can be quiet without wanting input. Waiting comes only
  from an explicit signal: bell / OSC 9 / `wsh agentstate waiting`.

## State → badge mapping — `publishActivityBadge` (termactivity.go)

Every transition publishes one `Event_Badge` per block oref, using a **stable
per-block badgeid** (uuidv7, `activityBadgeIds`) so a set is an in-place update
and a clear-by-id removes exactly this badge (avoids the clear-then-set broker
race that used to wipe badges instantly). Priority 5.

| State | Badge | pidlinked | Notes |
| --- | --- | --- | --- |
| working / thinking | `spinner+spin`, accent color | yes | suppressed (cleared) if `isServerCommand(ev.Command)` |
| waiting | `comment-dots`, `#fbbf24` | no | |
| done, visible, exit 0/nil | `circle-check`, success | no | not pidlinked so focusing the tab clears it — it's an attention cue for tabs you're NOT on |
| done, visible, exit ≠ 0 | `circle-xmark`, error | no | |
| done, not visible / none | *(clear by id)* | | |

**pidlinked semantics**: the frontend focus-clear (`app.tsx`) skips pidlinked
badges. So the live spinner survives focusing its tab; ✓/✗/💬 get cleared
~500 ms after you focus the block / 3 s if already focused (`clearBadgesForBlockOnFocus`,
`clearBadgesForTabOnFocus` — non-pidlinked only).

## Badge plumbing

- **Backend store** — `pkg/wcore/badge.go`: in-memory `oref → Badge`
  (transient, lost on wavesrv restart; that's fine — trackers republish).
  Subscribes to `Event_Badge` itself, so it sees the same stream the renderers
  do. Rule: same badgeid ⇒ always apply (in-place update); otherwise only a
  strictly higher (priority, badgeid) badge wins. `GetAllBadgesCommand` returns
  a snapshot — used by a fresh renderer to hydrate.
- **Frontend store** — `frontend/app/store/badge.ts`: same wins/update rule,
  jotai atom per oref (`BadgeMap`). `getTabBadgeAtom(tabId)` derives a tab's
  badge list = badges of all its blocks + the tab's own badge, sorted by
  priority. Rendered by `frontend/app/tab/tabbadges.tsx` (first badge = icon,
  up to 2 more as 4 px dots) inside `tab.tsx` / `vtab.tsx`.
- A **tab-level flag color** (user-set) is merged in as a priority-0 badge.

## OS notifications — `frontend/app/view/term/term-activity.ts` + `notify-commanddone.ts`

Driven by `Event_TermActivity` (NOT by badges). Every renderer gets every event
(AllScopes); **only the renderer whose tab owns the block fires** (de-dupe).
Both kinds are suppressed while the window has focus.

- **Command done** — opt-in `notify:commanddone`, duration ≥
  `notify:commanddonethresholdms` (default 30 s). Bursts coalesce for 5 s into
  one summary notification; refocusing the window during the window drops the
  queue. Routed to electron (`NotifyCommand`, route "electron"); clicking
  focuses window + tab.
- **Agent waiting** — always on, fires immediately ("Claude is waiting for
  you" + tab name).

## `wsh agentstate` — the precise-signal side channel

`cmd/wsh/cmd/wshcmd-agentstate.go` → `SetTermAgentStateCommand` RPC →
`SetExternalAgentState`. For agent lifecycle hooks running *inside* the Wave
terminal (Claude Code `Notification`/`Stop` hooks, codex `notify`):

```sh
wsh agentstate waiting --agent claude   # needs-attention 💬 immediately
wsh agentstate done    --agent claude   # turn-finished ✓ (agent keeps running)
```

Explicit signals beat every heuristic. Hooks must guard on being inside Wave
(`[ -n "$WAVETERM_BLOCKID" ]`) or `wsh` won't resolve a block.

## Claude session binding (adjacent, same signal) — `claudesession.go`

On a tracked `claude` command start, a goroutine binds the claude session id to
the block (meta `claude:sessionid`), preferring claude's own pid-keyed session
registry (`~/.claude/sessions/<pid>.json`), falling back to transcript-dir
watching. Poll lifetime = as long as claude runs (`claudeStillRunning`), so
in-session `/resume` re-binds. Used for the "resume this session" affordance
after a restart.

## Debugging

Setting **`term:activitydebug: true`** (Wave Config → Debug mode toggle) turns on:

- Backend: `[tabactivity]` lines in `waveapp.log` — every transition, badge
  set/clear, bell handling, throttled 1/s "feed" lines (bytes/chunks/state).
  `[claudesession]` for session binding.
- Frontend: `[tabactivity][fe]` via `activityLog` — every event each renderer
  sees (`ownThisTab=` shows which renderer owns it) and every notification
  FIRE/SKIP decision with the reason.

Useful greps (log at `~/Library/Application Support/waveterm/waveapp.log`):

```sh
grep tabactivity waveapp.log | grep blk=<first8>          # one block's story
grep tabactivity waveapp.log | grep -v "feed \|fe-log"    # transitions only
grep "bell/osc9" waveapp.log                              # did agent bells ever arrive?
grep "external agentstate" waveapp.log                    # are wsh hooks wired?
```

Diagnosis cheat-sheet:

| Symptom | Look for | Likely cause |
| --- | --- | --- |
| spinner never stops, terminal looks idle | 1/s `feed` lines still flowing | a TUI/agent dribbles output; each chunk re-arms the idle timer |
| spinner never stops, NO feed lines | last transition `-> thinking`, `running=true` | a tracked non-agent command still open with output paused (by design), or — before the agent-idle-done fix — an agent at its prompt whose bell/`agentstate` never arrived |
| no indicator for first command in a fresh terminal | no `osc-C` line | bash-preexec doesn't fire preexec for the very first command (known limitation) |
| 💬 never shows for an agent | `bell/osc9 ignored (no tracked or probed agent)` | command not classified as agent AND probe failed (remote block?) |
| badge vanishes instantly | `badge set` then `badge clear` | state flapped; check transitions |

## Known gaps (as of 0.14.5-pj.12)

1. ~~A finished agent turn with no signal parks in *thinking* → spinner forever.~~
   **Fixed** (feat/agent-idle-done): the idle-fire for a running agent command now
   resolves to done ✓ instead of thinking (see "The two spinner paths"). Field
   evidence that motivated it (2026-08-03): `bell/osc9 -> waiting` had fired
   **zero** times in the entire log — claude never rings in Wave without a
   configured notif channel, so the waiting leg was dead. The *precise* signal is
   still the better one where available: wire Claude Code hooks
   `Stop` → `wsh agentstate done` and `Notification` → `wsh agentstate waiting`
   (guarded on `$WAVETERM_BLOCKID` + `command -v wsh`) — explicit states beat the
   silence heuristic and light up 💬 the moment claude asks a question.
2. **Server suppression is command-classification only** (`serverCommandRegexes`).
   `task dev`, `make watch`, unlisted runners still spin forever while running.
3. **First command in a fresh terminal** has no C marker (bash-preexec quirk) —
   detection is output-driven for it.
4. **Remote blocks can't be process-probed** — bell from an untracked agent on a
   remote connection is ignored.
