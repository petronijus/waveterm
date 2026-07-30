<div align="center">

# Wave Terminal — petronijus fork

### A personal Wave build with a hand-built power layer on top

Tracks upstream [Wave Terminal](https://github.com/wavetermdev/waveterm) and adds a curated set of
features — a real **Git view**, **inline terminal images**, **remote file transfer**, cross-machine
**config sync**, desktop **notifications**, and live **UI theming** — each reimplemented cleanly on
Wave's own architecture and built to work **locally and over SSH** alike.

<br/>

[![Latest release](https://img.shields.io/github/v/release/petronijus/waveterm?include_prereleases&sort=semver&label=release&color=6b5cff&style=for-the-badge)](https://github.com/petronijus/waveterm/releases)
[![Platforms](https://img.shields.io/badge/macOS%20·%20Linux%20·%20Windows-informational?style=for-the-badge&color=00c2c7)](https://github.com/petronijus/waveterm/releases)
[![License](https://img.shields.io/github/license/petronijus/waveterm?style=for-the-badge&color=444)](./LICENSE)

</div>

---

## ✨ What this fork adds

### 🔀 A first-class Git workflow

- **Git view** — branch switcher, staged/unstaged/untracked file lists, inline diff, commit &
  push — all running over `wshremote`, so it works on the **local machine and remote SSH hosts**.
- **Per-hunk stage / unstage** — stage or revert individual hunks straight from the diff, powered
  by `git apply --cached` (git does the splitting, so patches always apply cleanly).
- **Multi-file review mode** — a _Review_ action walks every changed file in one flow;
  <kbd>F7</kbd> / <kbd>Shift</kbd>+<kbd>F7</kbd> to jump between files, <kbd>Esc</kbd> to exit.
- **Push authentication** — HTTPS push prompts for a username / token, stores it in Wave's
  **secret store** (host-keyed), and supplies it to git via a temporary `GIT_ASKPASS` helper —
  the token never touches a command line.

### 🖼️ A richer terminal

- **Inline images** — Sixel and the iTerm2 inline-image protocol (IIP) render pictures right in
  the terminal (`chafa`, image CLIs, and friends just work).
- **Remote file transfer** — paste or drop an image/file into a **remote** SSH terminal and it's
  uploaded to that host, with the remote path pasted back (upload indicator included).
- **Non-blocking SSH auth prompts** — password / passphrase / keyboard-interactive prompts appear
  as an overlay scoped to the connection that asked, shown only on the tabs using it, so the rest
  of the UI stays fully interactive. Answered prompts dismiss everywhere at once, and a prompt
  raised during startup or a wake-time reconnect isn't lost before a window is listening.
- **Resume a Claude session** — a terminal remembers the Claude Code session run in it; once that
  session stops, a **Resume session** button in the block header brings it back in one click.
  Tracked per block, so several terminals in one repo each keep their own, and it follows a
  resume done from inside claude. Nothing to configure on the Claude side.
- **Tab activity indicator** — an output-driven "working" spinner and a "done" badge on tabs, so a
  glance tells you which terminal is busy. Long-running dev servers (`shopify theme dev`, `vite`,
  `rails server`, `npm run dev`, …) are recognized and leave the tab clean instead of spinning
  forever — while AI agents keep spinning as they work.
- **Badge rotation** — `wsh badge --rotation <deg>` spins a badge icon for animated status cues.

### 🔄 Cross-machine & workflow

- **Config sync** — a last-writer-wins merge engine (`wsync`) that converges settings across
  machines over **WebDAV** or a credential-free **local-folder** mode (drop it in a Nextcloud /
  Drive folder). Background scheduler, a _Sync now_ action, and a native folder picker.
- **Session save / load** — snapshot all windows, tabs and block layouts to the same sync
  transport and restore them on another machine — nested splits, sizes, focus and window
  positions survive exactly. Block locations travel machine-neutrally (named **path roots** +
  `~`-form paths), so a layout saved on Linux opens the right folders on Windows or macOS.
- **Duplicate a tab** — right-click a tab → _Duplicate_: a copy opens immediately to its right
  with the same arrangement and the same block settings (cwd, connection, theme), and fresh
  shells. Nested splits survive intact.
- **Folder bookmarks ("projects")** — bookmark folders and reach them from the Files view, the
  connection dropdown, and a Connections & Projects settings panel.
- **Desktop notifications** — get a system notification when a long command finishes while the
  window is unfocused; plus an **agent-waiting** state that flags a tab as "waiting for you"
  across Claude, Gemini & Codex — driven by agent hooks (`wsh agentstate`) with a process-tree
  fallback, and sticky until you actually answer (an idle agent's TUI repaints can't clear it).
- **Auto-updates from this fork's releases** — the built-in updater runs against this repo's
  GitHub Releases on a dedicated `pj` channel: new fork versions download in the background and
  install on restart (on Linux the app relaunches itself once the old instance is really gone).
  A **`pj.N` badge** in the tab bar always shows which build you're on — click it for the full
  version.

### 🎨 Make it yours

- **UI theme picker** — app-wide themes (Dracula, Dark+/Light+, One Dark, Monokai, Nord, Solarized
  …) switchable live from the block gear menu, with a full **Themes** editor and live color
  pickers; the terminal palette follows the active theme.
- **Named tab flags** — Finder-tags-style labeled, colored flags you can pin to a tab; edit a
  flag's color and every flagged tab updates live.
- **Polish** — configurable default terminal font size, a live-synced GUI/JSON settings split, and
  refined light-mode theming across the tab bar, sidebar, AI panel, and system graphs.

<details>
<summary><b>Under the hood</b> — reliability fixes that keep all of the above solid</summary>

<br/>

- **Terminal write batching** — streaming output (an agent thinking, a build log) coalesces into
  ≤30 renders/s, cutting a visible streaming terminal from ~40% to ~13% renderer CPU without
  adding any typing latency.
- **Terminal escape hygiene** — dark/light color-scheme reports (`DECSET 2031` subscribers like
  Claude Code) fire only on real theme changes, and a restarted shell clears the stale
  subscription — no more literal `997;1n` junk typed into the prompt when switching windows.
- **Git panel auto-refresh** — explicit RPC timeouts plus a client-side settle timer keep the 2s
  status poll alive across sleep/wake and reconnects, with a toolbar warning when refresh fails;
  history gets an All | Branch switch showing only the current branch's own commits.
- **WPS broker** — user-input events are buffered so a password prompt fired during startup or a
  reconnect is never lost, and locked route-matching was split to remove a reentrant-lock deadlock.
- **SSH** — fork-side reconnect / sleep-wake robustness on top of upstream's handling.
- Features that landed upstream in the meantime (SSH port forwarding, auto-reconnect, the base git
  RPCs) are intentionally **not** re-added — this fork builds on top of them rather than around
  them.

</details>

---

## 📦 Install

Grab the latest build from **[Releases](https://github.com/petronijus/waveterm/releases)**.

| Platform                | Notes                                                                                                          |
| ----------------------- | -------------------------------------------------------------------------------------------------------------- |
| **macOS** (arm64 / x64) | Signed (Developer ID) but **not notarized** — on first launch, right-click the app → **Open**. |
| **Linux**               | `.deb`, `.AppImage`, and `.zip`.                                                                               |
| **Windows**             | NSIS installer (per release).                                                                                  |

Want to run this fork **side by side** with a stock Wave? Each release also ships a **Wave (Dev)**
build with its own app identity and data directory — install both without conflict.

---

## 🛠️ Build from source

Prerequisites: **Go**, **Node**, **[Task](https://taskfile.dev)**, and **Zig** (for the `wsh`
cross-compile).

```sh
task init                # one-time: install dependencies
task dev                 # run the dev app with hot reload
task package             # build an installer for the current OS → ./make
```

`task package` builds for the OS it runs on, so a full release is produced per-platform. See
**[BUILDING.md](./BUILDING.md)** for per-OS detail.

---

## 🧭 Fork model

- **`main`** mirrors upstream `wavetermdev/waveterm` and never carries fork work.
- **`release`** is the integration branch — everything here is built and shipped from it.
- Each feature lives on a focused `feat/*` branch, kept as a clean, rebaseable series.

Deeper notes on what the fork adds and how it's maintained live in **[FORK.md](./FORK.md)**;
day-to-day build/release detail is in **[BUILDING.md](./BUILDING.md)**.

---

## 📄 License

Apache-2.0, same as upstream Wave Terminal — see [LICENSE](./LICENSE). A personal, unofficial fork,
not affiliated with or endorsed by Command Line Inc.
