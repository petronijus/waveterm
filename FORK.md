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

- **SSH backport** — curated cherry-picks from `whoisjeremylam/waveterm-remote`: hardened SSH
  reconnect, **SSH port forwarding** (Local/RemoteForward, which upstream lacks), and related
  crash / CPU-spin fixes. The x/crypto drain-loop fix is taken via the tagged `v0.53.0` bump
  rather than vendoring a patched copy.
- **UI theme picker** — app-wide color themes (Dracula, Dark+/Light+, One Dark, Monokai, Nord,
  Solarized), live-switchable; a dedicated Themes editor (also a tab in Wave Config) with GUI
  color pickers and live preview; no flash-of-default-theme on launch. The terminal background /
  foreground follow the active theme.
- **Named tab flags** — Finder-tags-style labeled, colored flags managed in the Themes editor;
  assign one per tab (shown as a colored dot). Editing a flag's color updates flagged tabs live.
- **Light-mode polish** — themed the tab bar, tab close button, sidebar/widget icons, AI panel,
  popovers, workspace accent, and CPU/Mem graphs.
- **Tab activity indicator** — shows in the tab when a terminal is *working* (a long-running
  foreground command) vs *done*, generically — not tied to one specific tool. Built on the
  existing tab badge system; detection via shell-integration / command lifecycle.
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
- **Git view** — a first-class Git block: branch switcher, file change list, inline diff,
  double-click a file for the full file with `+`/`-` markers, and an "Open Git Here" context-menu
  entry. Backed by `RemoteGit*` RPC over `wshremote`, so it works locally and over remote SSH
  connections. Backend git RPC test coverage included.
- **Config sync** — a per-install last-writer-wins merge engine (`wsync`) with tombstones that
  converges settings across machines. Transports: WebDAV (mtime-stamped change detection) or a
  credential-free local-folder mode (Nextcloud / Drive desktop client). Background scheduler wired
  into `wavesrv` startup, a "Sync now" RPC, status UI, and a native folder picker.
- **Folder bookmarks ("projects")** — bookmark folders, surfaced across the Files view, the
  connection dropdown, and a two-pane Connections & Projects settings panel.
- **System monitor — project resource attribution** — the sysinfo (CPU/Mem) block can show how
  much of the system load is *the project you're building*, not just global totals. It attributes
  the tracked project's **host processes** (those whose cwd is under the project path) **and its
  containers** (Docker **and** Podman, spoken to directly over the engine unix socket — no CLI/SDK
  dep — matched by the `com.docker.compose.project` label *or* an image/container-name token, so
  plain `docker run` builds are caught too) into dedicated series (`cpu/mem:proj:host` in accent,
  `cpu/mem:proj:docker` in docker-blue). Per-process and per-container CPU% is normalized to a
  share of *total* capacity, so it overlays/stacks under the system line. New plot views: "CPU +
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
- **Releases** — built per-platform and published on the fork's GitHub Releases (macOS first;
  Linux & Windows builds run on local machines — no hosted CI).

## Planned

- **Sync — window/tab layout** — `wsync` currently converges settings; extend it to also sync
  open tabs and window layout between machines.
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
