<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/waveterm-logo-horizontal-dark.png">
  <img alt="Wave Terminal — petronijus fork" src="assets/waveterm-logo-horizontal-light.png" width="380">
</picture>

### The open-source graphical terminal — with a hand-built power layer on top

A personal fork of [**Wave Terminal**](https://github.com/wavetermdev/waveterm) that tracks upstream
and adds a curated set of features: a real **Git view**, **inline terminal images**, **remote
file transfer**, cross-machine **config sync**, desktop **notifications**, live **UI theming**, and
more — each reimplemented cleanly on Wave's own architecture.

<br/>

[![Latest release](https://img.shields.io/github/v/release/petronijus/waveterm?include_prereleases&sort=semver&label=release&color=6b5cff&style=for-the-badge)](https://github.com/petronijus/waveterm/releases)
[![Platforms](https://img.shields.io/badge/macOS%20·%20Linux%20·%20Windows-informational?style=for-the-badge&color=00c2c7)](https://github.com/petronijus/waveterm/releases)
[![Based on Wave](https://img.shields.io/badge/based%20on-Wave%20Terminal-00c2c7?style=for-the-badge)](https://github.com/wavetermdev/waveterm)
[![License](https://img.shields.io/github/license/petronijus/waveterm?style=for-the-badge&color=444)](./LICENSE)

<br/>

<img alt="Wave Terminal screenshot" src="assets/wave-screenshot.webp" width="820">

</div>

---

## 🌊 What is this?

Upstream **Wave** is an open-source terminal that fuses a classic shell with inline graphical
widgets — web pages, file previews, code editors, and AI — laid out in a dynamic, tiling
workspace. This fork keeps that foundation and layers on the tools I want in a daily driver,
built to work **locally and over SSH** alike.

> New to Wave? The [upstream README](https://github.com/wavetermdev/waveterm) has the full tour of
> the base terminal. Everything below is what this fork adds on top.

---

## ✨ Highlights

### 🔀 A first-class Git workflow

- **Git view** — branch switcher, staged/unstaged/untracked file lists, inline diff, commit &
  push — all running over `wshremote`, so it works on the **local machine and remote SSH hosts**.
- **Per-hunk stage / unstage** — stage or revert individual hunks straight from the diff, powered
  by `git apply --cached` (git does the splitting, so patches always apply cleanly).
- **Multi-file review mode** — a *Review* action walks every changed file in one flow;
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
  as an overlay *inside the block that asked*, so the rest of the UI (and your other tabs) stays
  fully interactive.
- **Tab activity indicator** — an output-driven "working" spinner and a "done" badge on tabs, so a
  glance tells you which terminal is busy.
- **Badge rotation** — `wsh badge --rotation <deg>` spins a badge icon for animated status cues.

### 🔄 Cross-machine & workflow

- **Config sync** — a last-writer-wins merge engine (`wsync`) that converges settings across
  machines over **WebDAV** or a credential-free **local-folder** mode (drop it in a Nextcloud /
  Drive folder). Background scheduler, a *Sync now* action, and a native folder picker.
- **Folder bookmarks ("projects")** — bookmark folders and reach them from the Files view, the
  connection dropdown, and a Connections & Projects settings panel.
- **Desktop notifications** — get a system notification when a long command finishes while the
  window is unfocused; plus an **agent-waiting** state that flags a tab as "waiting for you"
  across Claude, Gemini & Codex.

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

- **WPS broker** — user-input events are buffered so a password prompt fired during startup or a
  reconnect is never lost, and locked route-matching was split to remove a reentrant-lock deadlock.
- **SSH** — tracks upstream's hardened reconnect / sleep-wake handling and port forwarding, plus
  fork-side robustness fixes.
- Features that landed upstream in the meantime (SSH port forwarding, auto-reconnect, the base git
  RPCs) are intentionally **not** re-added — this fork builds on top of them rather than around
  them.

</details>

---

## 📦 Install

Grab the latest build from **[Releases](https://github.com/petronijus/waveterm/releases)**.

| Platform | Notes |
| --- | --- |
| **macOS** (arm64 / x64) | Signed with a development certificate but **not notarized** — on first launch, right-click the app → **Open**. |
| **Linux** | `.deb`, `.AppImage`, and `.zip`. |
| **Windows** | NSIS installer (per release). |

Want to run the fork **side by side** with a stock Wave? Each release also ships a **Wave (Dev)**
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

## 🙏 Credits

Built on the excellent [**Wave Terminal**](https://github.com/wavetermdev/waveterm) by Command Line
Inc. and its contributors. Several terminal/git features here were inspired by
[whoisjeremylam/waveterm-remote](https://github.com/whoisjeremylam/waveterm-remote) and
reimplemented on Wave's own codebase.

## 📄 License

Apache-2.0, same as upstream — see [LICENSE](./LICENSE).

<div align="center"><sub>A personal fork · not affiliated with or endorsed by Command Line Inc.</sub></div>
