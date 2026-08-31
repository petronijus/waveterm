# Fork notes — petronijus/waveterm

Status and plans for this personal fork of [Wave Terminal](https://github.com/wavetermdev/waveterm).
See [README](./README.md) for what the fork is and how to build it; upstream's own roadmap lives
in [ROADMAP.md](./ROADMAP.md).

## Branch model

- `main` — mirrors `wavetermdev/waveterm`, never carries fork features (only `git merge --ff-only upstream/main`).
- Topic branches — `feat/wave-theme`, `backport/ssh`, `feat/theme-picker`, etc. Each kept as a clean, rebaseable patch series.
- `release` — integration branch (`main` + merged topic branches); this is what gets built and released.

Staying current = rebase topic branches onto `main`, not merge.

## Development workflow (per task)

One branch per task, branched off `release`, merged back when the task is done:

```sh
# start a task
git checkout release
git pull            # if release is on the remote
git checkout -b feat/<task>      # e.g. feat/tab-notifications

# … work, committing as you go …

# finish a task → fold it into the integration branch
git checkout release
git merge feat/<task>            # fast-forward if release didn't move
git push origin release feat/<task>
```

- Build & release always from `release` (see [BUILDING.md](./BUILDING.md)).
- Never commit on `main` — it only mirrors upstream. If you accidentally push fork
  commits to `main`, reset it: `git push origin --force-with-lease <upstream-commit>:main`.
- Keep each `feat/*` branch focused on one task so it stays a clean, rebaseable series.
- Set your commit identity per machine before committing (this is a personal fork):
  `git config user.email <your-personal-email>`.

## Syncing with upstream

```sh
git fetch upstream
git checkout main && git merge --ff-only upstream/main && git push origin main
# then rebase each topic branch and rebuild release
git checkout feat/<task> && git rebase main
```

## Done

- **Hard-wrap aware copy & links** — TUI programs (Claude Code, tmux, less, ...) re-wrap
  their output themselves and print real newlines at the terminal width, so copied text came
  out shredded into width-sized lines and URLs split across lines weren't clickable. The
  terminal link detector now follows a URL across such hard wraps (a line running through its
  last column joins the next one), and every copy path (Cmd+C, Ctrl+Shift+C, copy-on-select,
  context menu) re-joins hard-wrapped lines via the same heuristic. Governed by
  `term:copyunwrap` (default true); the context menu gains **Copy Raw** as an exact-selection
  escape hatch.
- **Claude session resume per terminal** — a terminal block remembers the Claude Code session
  it ran, and after a Wave restart shows a small history button in the block header that types
  `claude --resume <id>` at the prompt (without pressing Enter, so you can edit or back out).
  The id is bound **per block**, because several terminals in one tab routinely run separate
  sessions in the same directory. Works out of the box — no Claude-side configuration. See
  [Claude session resume](#claude-session-resume).
- **SSH backport** — curated cherry-picks from `whoisjeremylam/waveterm-remote`: hardened SSH
  reconnect, **SSH port forwarding** (Local/RemoteForward, which upstream lacks), and related
  crash / CPU-spin fixes. The x/crypto drain-loop fix is taken via the tagged `v0.53.0` bump
  rather than vendoring a patched copy. The SSH/auth area is now **fork-first**: we track their
  implementation instead of maintaining a parallel one. Every pick, every deliberate skip, and
  the few places where our version deliberately wins are recorded in **[BACKPORT.md](./BACKPORT.md)**
  — read it before starting another backport round.
- **Remote sessions survive sleep** (Round 4) — closing the lid used to _freeze the remote
  process_: it blocked inside `write()` until TCP recovered or a server-side keepalive expired,
  which can take hours. A 5s send timeout breaks that backpressure chain, bounding the freeze to
  ~5 seconds, and output produced while disconnected is buffered **to disk** instead of being
  dropped past the 2 MB in-memory window, then replayed on reconnect. Note their 5s timeout still
  leaks a goroutine per timed-out send — acknowledged in their own spec, not yet mitigated.
- **Reconnect UX** (Round 4) — reconnect on tab switch and app focus for all view types; overlay
  hysteresis so brief blips stop flashing a disconnect banner; a "gave up" overlay with stop-retry
  and an attention heartbeat; sticky suppress after an explicit Disconnect / Stop / password
  cancel; permanent failures (host key changed, credentials rejected) stop retrying instead of
  hammering; per-connection retry jitter; Linux/Windows resume parity. Password prompts are
  serialized one-per-window, the cached password survives network flaps but is cleared when the
  server actually rejects it, and a wrong password now says so instead of silently re-prompting.
- **UI theme picker** — app-wide color themes (Dracula, Dark+/Light+, One Dark, Monokai, Nord,
  Solarized), live-switchable; a dedicated Themes editor (also a tab in Wave Config) with GUI
  color pickers and live preview; no flash-of-default-theme on launch. The terminal background /
  foreground follow the active theme.
- **Named tab flags** — Finder-tags-style labeled, colored flags managed in the Themes editor;
  assign one per tab (shown as a colored dot). Editing a flag's color updates flagged tabs live.
- **Light-mode polish** — themed the tab bar, tab close button, sidebar/widget icons, AI panel,
  popovers, workspace accent, and CPU/Mem graphs.
- **Tab activity indicator** — shows in the tab when a terminal is _working_ (a long-running
  foreground command) vs _done_, generically — not tied to one specific tool. Built on the
  existing tab badge system; detection via shell-integration / command lifecycle. Long-running
  dev servers (`shopify theme dev`, `vite`, `rails server`, `npm run dev`, Django/Laravel, … —
  matched by command, seeing through `npx` / `bundle exec` / `poetry run` wrappers) leave the tab
  clean instead of spinning forever: a running server is its normal state, not "work in progress".
  AI agents still spin while working, and ordinary commands still spin until they finish.
- **Native OS notifications** — fire a system notification when a long command (≥ a configurable
  threshold, default 30 s) finishes while the window is unfocused; clicking it focuses the window
  and switches to that tab. Bursts of finishes coalesce into one summary notification. Opt-in
  (`notify:commanddone`), toggled from a visual settings panel in Wave Config → General (shown
  side-by-side with the raw `settings.json`, kept in sync live). Built on the activity detection
  above. Inherits its shell-integration limitation: the very first command in a fresh terminal
  isn't detected (bash-preexec doesn't fire `preexec` for it), so it doesn't notify — every command
  after the first does.
- **Agent-waiting notification** — a distinct "waiting for you" tab state + OS notification when
  an AI agent needs input, generalized across Claude, Gemini & Codex via an OSC 9 signal. Always
  on (no toggle).
- **Background tabs don't burn CPU** — background tab renderers used to be kept unthrottled so
  they could badge their tab and fire notifications, which meant every cached tab (and its
  webview guests — a backgrounded Jira tab, say) kept painting, animating and polling at full
  speed forever; with many tabs open that was ~90 % CPU across renderers even at idle. Both
  responsibilities moved off the renderers — badges were already backend-driven, and the
  command-done / agent-waiting notifications now fire from the electron main process off the
  backend activity stream (events carry tab/workspace routing; the focus gate uses the OS-truth
  `BaseWindow.isFocused`) — so background tabs are now actually hidden and Chromium-throttled.
  Measured: a terminal flooding output drops from ~17 % renderer CPU to under 1 % the moment its
  tab goes to the background. Notifications now also work for tabs whose renderer was evicted
  from the cache or whose workspace isn't shown in any window — cases the old renderer-side
  path silently missed. A regression smoke suite guards the pipeline
  (`node .kilocode/skills/run-desktop/smoke.mjs` — badges, throttling, notification gate).
  **Round 2 (pj.15)** closed what the first pass couldn't reach. Webview guest processes
  escaped the throttling entirely — Electron has no way to hide a guest's render widget
  (its guest delegate lacks Chrome's visibility plumbing), so a Jira board in a hidden tab
  kept running at ~19 % CPU forever. Fixed in layers: guests are made throttleable before
  they exist (`will-attach-webview`), tab switches now emit Electron's internal
  window-visibility event so guests get a correct `document.visibilityState`, and a
  main-world polyfill parks `requestAnimationFrame` and clamps sub-second timers while
  hidden — emulating native background throttling exactly (measured: guest rAF 120/s → 0/s,
  clean resume, audible pages exempt so background music keeps playing). Since
  `document.visibilityState` never flips on tab switches (Electron shims it per-window),
  a new `tab-visibility-change` IPC drives `atoms.tabVisibleAtom`; the git view's 2 s
  status poll and the sysinfo plot rebuilds pause on it in background tabs and catch up
  on re-show. The tab-bar working spinner — which a long-running agent session keeps
  alive for hours — now animates at 6 steps/s instead of 60 (it was the main feeder of a
  ~20 % GPU-process load), and the derived tab-badge atom got an equality check so badge
  events stop re-rendering tab chips in every renderer. The smoke suite grew
  webview-throttle + webview-resume checks, runs fully sandboxed (throwaway data dirs),
  and defaults to a background mode that never steals focus.
  **Round 3 (pj.17)** removed the last remnant of the old always-unthrottled design, which
  had been quietly causing a grey window. Tab views were still constructed with
  `backgroundThrottling: false`, which sets Chromium's `disable_hidden_` flag and makes
  `RenderWidgetHostImpl::WasHidden()` a no-op — and a widget that never recorded a hide also
  early-returns out of `WasShown()`. The browser side still evicts the compositor surface
  whenever the OS unmaps the window (minimize, workspace switch, screen blank/lock, occlusion
  by a fullscreen window), so on the way back the renderer was never asked for a new frame and
  the tab showed nothing but its `#222222` background until something forced a repaint —
  which is why switching tabs, the only path that changes a view's bounds, brought the UI back.
  The flag is only needed while a hot-spare tab boots detached, so throttling is now enabled for
  real the moment a tab goes on screen (a visible tab is never throttled anyway). Belt and
  braces on top: window `show`/`restore` and `powerMonitor`'s `unlock-screen`/`resume` drive a
  `forceRepaint()` that cycles `setVisible` — chosen over nudging the bounds, which would reflow
  every block and resize every terminal for a purely visual repair. The smoke suite guards it
  with a check that has to run before any tab switch, since backgrounding a tab enables
  throttling on its own and would mask the bug.
- **Git view** — a first-class Git block: branch switcher, file change list, inline diff,
  double-click a file for the full file with `+`/`-` markers, and an "Open Git Here" context-menu
  entry. Backed by `RemoteGit*` RPC over `wshremote`, so it works locally and over remote SSH
  connections. Backend git RPC test coverage included. The 2s status auto-refresh is wedge-proof:
  git RPCs carry explicit per-class timeouts (read/action/sync — so pushes get their full 90s
  budget instead of a silent 5s default), a client-side settle timer catches responses lost to
  sleep/wake or a ws reconnect, and a toolbar ⚠ shows when refresh is failing instead of silently
  serving stale data. The History list has an All | Branch switch — "Branch" shows only commits
  unique to the current branch (what it adds on top of wherever it was cut from).
- **Config sync** — a per-install last-writer-wins merge engine (`wsync`) with tombstones that
  converges settings across machines. Transports: WebDAV (mtime-stamped change detection) or a
  credential-free local-folder mode (Nextcloud / Drive desktop client). Background scheduler wired
  into `wavesrv` startup, a "Sync now" RPC, status UI, and a native folder picker. An empty or
  invalid config file no longer breaks Save settings with a cryptic marshal error (empty files
  are skipped, invalid JSON reports the file by name), and the settings bundle + layout files on
  the share are written indented — readable, unlike the byte-compared merge-engine state files.
- **Folder bookmarks ("projects")** — bookmark folders, surfaced across the Files view, the
  connection dropdown, and a two-pane Connections & Projects settings panel.
- **System monitor — project resource attribution** — the sysinfo (CPU/Mem) block can show how
  much of the system load is _the project you're building_, not just global totals. It attributes
  the tracked project's **host processes** (those whose cwd is under the project path) **and its
  containers** (Docker **and** Podman, spoken to directly over the engine unix socket — no CLI/SDK
  dep — matched by the `com.docker.compose.project` label _or_ an image/container-name token, so
  plain `docker run` builds are caught too) into dedicated series (`cpu/mem:proj:host` in accent,
  `cpu/mem:proj:docker` in docker-blue). Per-process and per-container CPU% is normalized to a
  share of _total_ capacity, so it overlays/stacks under the system line. New plot views: "CPU +
  Project", "Mem + Project", and a combined **"CPU & Mem + Project"** dual-chart view. A crosshairs
  button in the block header opens a folder picker that sets the tracked project
  (`sysinfo:trackpath` / `sysinfo:dockerproject`) — no hand-editing `settings.json`.
- **UI & robustness polish** — renderer-crash **auto-recovery** (a crashed tab reloads its
  renderer in place, with a loop guard, while the backend/shells survive) + logging; autoupdate
  feed pointed at the fork's **own GitHub Releases** (otherwise the fork silently reverts to stock
  Wave); the top tab bar themed to match the widgets sidebar; the cloud-sync button restyled to the
  accent style, flush to the window edge, with a per-layout save action and a "Sync settings…"
  link; the terminal header cwd shown as `~/…` for local connections (matching the files/git
  panels); synced config files pretty-printed instead of one long line; and a Wave Config
  **Debug-mode** toggle for the tab-activity logging.
- **Session sync — manual Save/Load** — cloud Save (☁↑) / Load (☁↓) actions in the tab bar
  snapshot every workspace's windows, tabs, blocks and window geometry to the sync transport
  (WebDAV / local folder) and restore them on another machine: windows reopen at their saved
  positions (clamped to the local display), surplus windows close (never the last one), and saved
  layouts restore losslessly via a single `settree` layout action — nested splits, sizes, focus
  and magnify survive exactly.
- **Auto-update from the fork's own releases** — the full electron-updater flow runs against this
  repo's GitHub Releases on a dedicated **`pj` channel**: fork versions are real semver
  prereleases (`0.14.5-pj.11`), the counter is **global** across upstream rebases
  (`0.14.5-pj.11` → `0.14.6-pj.12`, recovered from the `v*-pj.*` tags), a **`pj.N` badge** in
  the tab bar shows the running build (click → About), and on Linux the post-update restart is
  owned by the fork end to end: it waits for the old instance to fully exit before relaunching
  (fixes the silent single-instance-lock death) and starts the new instance as a transient
  `systemd --user` service. That second half is what makes `sudo` work inside Wave. Electron's
  own relauncher starts the replacement through Chromium's `base::LaunchProcess`, which sets
  `PR_SET_NO_NEW_PRIVS` on the child — a bit that can never be cleared in a running process and
  is inherited by wavesrv, by every terminal under it and by everything those terminals run, so
  every setuid binary broke from the first auto-update onward and stayed broken across later
  ones. Only having the service manager fork the new process escapes it; a detached spawn
  inherits the bit, and so does `systemd-run --scope`. Applies to deb/rpm/pacman as well as
  AppImage. Windows ships a signed NSIS build, macOS a Developer-ID-signed and notarized build;
  per-OS `pj*.yml` + `latest*.yml` manifests are attached to every release.
- **Portable layout paths** — saved layouts store block locations (terminal cwd, preview file)
  machine-neutrally so a layout saved on one OS restores on another: paths under a named root
  from the machine-local `sync:pathroots` setting save as `${name}/rest` (e.g.
  `{"dev": "~/Documents/Dev"}` on Linux vs `{"dev": "D:/Dev"}` on Windows), other home paths
  save as `~/rest`, and an unknown root falls back to `~` on load instead of erroring. Remote
  blocks keep their paths verbatim — they're valid from any machine that reaches the host.
- **Agent waiting detection** — the tab activity badge now reliably flips to "waiting for you"
  (💬) when an interactive AI agent needs input, through three layers: the agent's bell/OSC 9
  "your turn" signal is honored even when the shell-integration command marker was never seen
  (a durable session that outlived wavesrv, broken preexec) by identifying the agent from the
  pty's process tree (foreground-process matching, the technique agent multiplexers use); and
  `wsh agentstate waiting|done` lets agent lifecycle hooks (Claude Code `Notification`/`Stop`,
  codex `notify`) push exact states in-band — precise turn semantics, no false "waiting" while
  the agent runs subagents or long tools. Silence is deliberately _not_ treated as waiting, and
  waiting is sticky against output (an idle agent TUI repaints continuously — no volume
  threshold separates that dribble from real work): only a deliberate keypress (text/Enter,
  not arrow-browsing or the terminal's automatic escape replies) releases it. Safety net for
  setups where neither signal ever arrives (agent has no notif channel configured, no hooks
  wired): a _running agent_ whose output stops entirely resolves to a ✓ "done" after the long
  idle window instead of parking in "thinking" — agent TUIs repaint continuously while they
  actually work, so prolonged true silence means the turn is over. Previously such an agent
  pinned the tab spinner for the life of the process. Architecture doc:
  `aiprompts/tab-activity-badges-notifications.md`.
- **Terminal write batching** — streaming pty output (an agent thinking, a build log) coalesces
  into at most ~30 xterm flushes/s instead of a parse+repaint per chunk; a visible streaming
  terminal dropped from 36–40% renderer CPU (+ ~40% GPU) to ~13–15% (+ ~12%). The first chunk
  after a quiet period flushes immediately, so keystroke echo is unaffected.
- **Terminal color-scheme report hygiene** — programs that subscribe to dark/light change
  notifications (`DECSET 2031`, e.g. Claude Code's theme detection) no longer get spammed with
  `CSI ?997;n` reports on every window/focus switch: the terminal theme is reapplied only when
  its colors actually changed (xterm.js treats every `options.theme` assignment as a change),
  and the shell-termination reset sequence now clears mode 2031, so a dead program's
  subscription can't leak literal `997;1n` garbage into the next shell's prompt.
- **Releases** — built per-platform and published on the fork's GitHub Releases (macOS on the
  MacBook, Windows & Linux on the homelab build VMs — no hosted CI).
- **Git branch field & switcher** — the git toolbar's branch button used to be capped at 40% of
  the toolbar width, ellipsising long branch names even with free space next to them; it now
  sizes to its content and shrinks only when the toolbar actually runs out of room (full name
  in the tooltip). The branch switcher bolds the checked-out branch instead of only marking it
  with a trailing check.
- **Linux dock icon** — the window is matched to its `waveterm.desktop` entry again, instead of
  showing up as a blank icon beside the launcher it was started from. Electron derives
  `CHROME_DESKTOP` from the app name, and Chromium names both the systemd scope it relocates
  itself into (`app-wave-<pid>.scope`) and its XDG app id after it; GNOME resolves a window's
  desktop entry from that scope, so it looked for a `wave.desktop` that does not exist. The fork
  pins the desktop name to the packaged executable's basename — the same value electron-builder
  names the entry after, so the dev-channel build stays correct too. Regressed with the Electron
  41.1.0 → 41.10.3 bump in pj.20.
- **Ported upstream PRs** — merged from `wavetermdev/waveterm` pull requests that are open but
  unmerged upstream (upstream's last merge to `main` was 2026-07-29). See
  [Ported upstream PRs](#ported-upstream-prs).

## Ported upstream PRs

Upstream has gone quiet — `main` last moved on 2026-07-29 and ~40 community PRs sit open. These
were cherry-picked into `release` (2026-08-28). Each keeps its original author; a fork-side
deviation is noted where the patch had to change.

| PR                                                         | What it adds                                                                                                                                                                | Fork notes                                                                                                                                                                                                                                                                    |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [#3406](https://github.com/wavetermdev/waveterm/pull/3406) | `Ctrl+[ \ ] /` send their ASCII control codes on non-US keyboard layouts (the browser reports the wrong `event.key` while Ctrl is held, so xterm.js sent GS instead of ESC) | Reimplemented: upstream derived a byte for _any_ `Ctrl+char` via `charCode & 0x1f` (making Ctrl+1 send DC1) and routed every Ctrl+letter around xterm's own encoding. The fork maps only the four keys that have a real ASCII control equivalent, resolved from `event.code`. |
| [#3413](https://github.com/wavetermdev/waveterm/pull/3413) | Web blocks respect the system color scheme (`nativeTheme.themeSource = "system"`)                                                                                           | Wave's own CSS never uses `prefers-color-scheme`, so only webview content and native dialogs follow the system.                                                                                                                                                               |
| [#3429](https://github.com/wavetermdev/waveterm/pull/3429) | A block losing focus no longer re-grabs it (two web blocks could flap forever)                                                                                              | —                                                                                                                                                                                                                                                                             |
| [#3407](https://github.com/wavetermdev/waveterm/pull/3407) | `window:restoreallwindows` — every window is restored on next launch, not just the last one (default `true`)                                                                | —                                                                                                                                                                                                                                                                             |
| [#3440](https://github.com/wavetermdev/waveterm/pull/3440) | `preview:openfileinnewblock` — Enter / double-click in the directory preview opens the file in a new block (default `false`)                                                | —                                                                                                                                                                                                                                                                             |
| [#3467](https://github.com/wavetermdev/waveterm/pull/3467) | Electron 41.1.0 → 41.10.3                                                                                                                                                   | —                                                                                                                                                                                                                                                                             |
| [#3421](https://github.com/wavetermdev/waveterm/pull/3421) | SSH agent forwarding (`ForwardAgent` / `ssh:forwardagent`), dialing the agent per channel so it survives an agent restart (1Password relock)                                | `ConnectToClient` gained the fork's `AuthTracker` + resolved `ConnKeywords` returns; the merged signature carries both.                                                                                                                                                       |
| [#3480](https://github.com/wavetermdev/waveterm/pull/3480) | Configurable keybindings via `keybindings.json`, editable in the config view                                                                                                | The fork's Escape chain (pop modal → dismiss the focused connection's password prompt → close search) was re-applied inside the new `generic:escape` command.                                                                                                                 |
| [#3484](https://github.com/wavetermdev/waveterm/pull/3484) | Auto-attach a tmux session on remote blocks (`term:tmux:session`) and list/switch sessions from the remote block's context menu                                             | Test file merged with the fork's own `blockcontroller_test.go`; rpc bindings regenerated.                                                                                                                                                                                     |
| [#3479](https://github.com/wavetermdev/waveterm/pull/3479) | Excalidraw diagram widget (`view: "excalidraw"`, `wsh excalidraw`)                                                                                                          | —                                                                                                                                                                                                                                                                             |
| [#3443](https://github.com/wavetermdev/waveterm/pull/3443) | File/document bookmarks: `filebookmarks.json`, a star dropdown in the preview toolbar, an edit modal, and document-position restore for markdown/code                       | Squashed (12 commits, all touching the generated rpc bindings). Independent of the fork's own `projects.json` folder bookmarks — both are kept.                                                                                                                               |

Upstream `main` itself is fully merged; the only thing it carries beyond the last fork release
is dependabot noise plus `wsh tab list` / `tab move`, which `release` already has.

Shipped in **v0.14.5-pj.21**.

> **Linux build note (pj.21).** The Excalidraw port grows the renderer bundle enough that
> `vite build:prod` on the 4 GB Linux VM now trips **`systemd-oomd`**, which kills on PSI memory
> _pressure_ rather than actual exhaustion — the build died at "rendering chunks" with `exit=137`
> while 17 GB of swap sat 94% unused, so adding RAM would not necessarily have helped. Run the
> build inside a scope oomd will not pick as a victim:
> `systemd-run --user --scope -p ManagedOOMPreference=avoid bash -lc '… task package …'`
> (no root needed). `journalctl -u systemd-oomd` names the killed cgroup and the pressure that
> triggered it.

## Claude session resume

A terminal block remembers the Claude Code session started in it and, once that session is no
longer running, shows a **Resume session** button in the block header. One click resumes it —
the command is sent with its newline and focus returns to the terminal.

**How the binding works.** Shell integration already reports every command line to the backend
(OSC 16162 `C`), which is how the activity indicator spots an agent. When that command is
`claude`, `pkg/blockcontroller/claudesession.go` locates the claude process under the block's
shell (the same process walk the agent probe uses) and reads claude's own session registry at
`<config>/sessions/<pid>.json`, which holds the pid, session id and cwd. Claude writes it about
a second after launch and removes it on exit, so the mapping is exact — no guessing which file
in a directory changed.

The watch keeps running for as long as claude does, because resuming _from inside_ claude (the
session picker, `/resume`) swaps the session id on the same process; the block follows it and
ends up pointing at whatever was actually used. An explicit `--resume <id>` on the command line
is taken straight from the command — it _seeds_ the binding and the watch keeps going, since
claude can still move off that session from inside.

**Surviving a wavesrv restart.** The watch is armed from the shell-integration `C` marker, which
a durable claude that outlives the wavesrv it started under never emits again — so the block used
to keep the session id it held before the restart and the button handed back the wrong
conversation, which is precisely when it matters most. A claude discovered any other way — the
process probe that already recovers the agent kind on a bell, or an `wsh agentstate` report —
now reconciles the recorded id against the live process through the same pid registry.
Reconciliation is throttled (bells arrive every turn and resolving the pid walks the process
tree) and stands down while a watch owns the block, so the two paths never race on the meta write.

**Why not the obvious alternatives.** The id is not in claude's environment and claude does not
hold its transcript open, so a pid alone tells you nothing. Watching transcripts is worse still:
one is only written on the first _user message_, so a session sitting at the prompt is invisible,
and claude's startup touches an unrelated transcript in the same directory, which looks exactly
like activity. Both cost real debugging before the registry turned up. Transcript watching
survives only as a fallback for builds with no registry, and it requires the file to _grow_, not
merely change mtime.

**Limits.** Remote blocks are skipped — the registry lives on the remote host. A session id whose
transcript was later deleted still offers a button, and the resume fails at the prompt; checking
would need a new RPC. The fallback path cannot follow an in-session resume. Reconciliation after
a restart needs _something_ to happen in the block (a bell or an agentstate report) before it
fires — a claude sitting idle since the restart is still shown with its pre-restart id.

Meta keys: `claude:sessionid`, `claude:cwd` (`pkg/waveobj/metaconsts.go`), stored with the block
row so they survive a restart. The button is `TermViewModel.getClaudeResumeHeaderElem`
(`frontend/app/view/term/term-model.ts`), rendered last in `viewText` so it sits left of the
end-icon strip; it hides while claude is running in that block, and prepends a `cd` when the
recorded cwd differs from the terminal's current one.

Set `term:activitydebug` to log the binding (`[claudesession]` in `waveapp.log`) and the button's
gate conditions (`[tabactivity][fe] claude-resume`).

## Planned

- **System monitor — manual tracker** — an escape-hatch to also count a process tree / container /
  cgroup that the cwd + container heuristics miss (e.g. `abuild`/`fakeroot`/`chroot` sandboxes,
  whose cwd is inside the sandbox and which aren't Docker). Remaining out-of-scope blind spots:
  remote/VM builds, short-lived compiler swarms (1 s sampling undercounts them), and
  kernel-IO/GPU/network load (not captured by CPU + mem).

## Known upstream bug to fix / report

- `wsh` install detection can misparse on hosts where `/bin/sh` is dash, due to stdout/stderr
  buffering order in the bootstrap command (`pkg/remote/conncontroller`). The logic is
  byte-identical in upstream and `waveterm-remote` — a clean upstream bug; candidate for our own
  patch plus an upstream report.
