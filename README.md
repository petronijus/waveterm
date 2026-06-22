# Wave Terminal — petronijus fork

A personal fork of [Wave Terminal](https://github.com/wavetermdev/waveterm) that tracks
upstream and adds a curated layer on top: a Git view, cross-machine config sync,
desktop notifications, folder bookmarks, cherry-picked SSH improvements, and a
VS Code-like UI theming system.

> Upstream Wave is an open-source terminal that combines a classic terminal with the
> ability to render graphical widgets (web, files, editors, AI) inline. See the
> [upstream README](https://github.com/wavetermdev/waveterm) for the full feature tour.

## What this fork adds

- **Git view** — a first-class Git block with a branch switcher, file change list, and inline
  diff; double-click a file for the full file with `+`/`-` markers; **"Open Git Here"** in the
  file-preview context menu. Backed by `RemoteGit*` RPC over `wshremote`, so it works on the
  local machine **and** over remote SSH connections.
- **Config sync (cross-machine)** — a per-install last-writer-wins merge engine (`wsync`) that
  converges settings across machines. Transports: **WebDAV**, or a credential-free
  **local-folder** mode (drop it in a Nextcloud / Drive desktop-client folder). Background
  scheduler, a **"Sync now"** action, status UI, and a native folder picker in Wave Config.
- **Folder bookmarks ("projects")** — bookmark folders and reach them quickly from the Files
  view, the connection dropdown, and a two-pane Connections & Projects settings panel.
- **Desktop notifications** — fire a system notification when a long-running command finishes
  while the window is unfocused (clicking it focuses the tab); plus an **agent-waiting** state
  that flags a tab as "waiting for you" across Claude, Gemini & Codex. Configurable from a
  visual settings panel.
- **Tab activity indicator** — output-driven "working" spinner and "done" badge on tabs, so you
  can see at a glance which terminal is busy.
- **UI theme picker** — app-wide color themes (Dracula, Dark+/Light+, One Dark, Monokai,
  Nord, Solarized Dark/Light), switchable live from the block gear menu (**UI Theme**) or a
  dedicated **Themes** editor (also a tab in Wave Config). Edit any theme's colors with live
  GUI color pickers; the terminal background/foreground follow the active theme too.
- **Named tab flags** (Finder-tags style) — define labeled, colored flags in the Themes editor
  and assign one to a tab; shown as a colored dot. Editing a flag's color updates flagged tabs
  live.
- **SSH backport** — curated cherry-picks from
  [whoisjeremylam/waveterm-remote](https://github.com/whoisjeremylam/waveterm-remote):
  hardened SSH reconnect, **SSH port forwarding (Local/RemoteForward)** which upstream lacks,
  and related crash/CPU-spin fixes.
- **Settings & terminal polish** — default terminal font size in General settings, a resizable
  GUI/JSON settings split kept in sync live, and light-mode theming across the tab bar, sidebar/
  widget icons, AI panel, popovers, workspace accent, and CPU/Mem graphs.

## Install

Grab a build from [Releases](https://github.com/petronijus/waveterm/releases). macOS builds are
signed with a development certificate but **not notarized** — on first launch right-click the app
→ **Open**. Linux and Windows builds are produced per-platform (see Build below).

## Build from source

Requires Go, Node, [Task](https://taskfile.dev), and Zig (for `wsh` cross-compile).

```sh
task init        # install deps
task dev         # run the dev app (hot reload)
task package     # build an installer for the current platform → ./make
```

## Relationship to upstream

- `main` mirrors `wavetermdev/waveterm` and never carries fork features.
- Fork work lives on topic branches (`feat/wave-theme`, `backport/ssh`, `feat/theme-picker`),
  integrated into `release`, which is what gets built.

## License

Apache-2.0, same as upstream Wave Terminal. This is an unofficial personal fork and is not
affiliated with or endorsed by Command Line Inc.
