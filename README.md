# Wave Terminal — petronijus fork

A personal fork of [Wave Terminal](https://github.com/wavetermdev/waveterm) that tracks
upstream and adds a curated layer on top: cherry-picked SSH improvements and a
VS Code-like UI theming system.

> Upstream Wave is an open-source terminal that combines a classic terminal with the
> ability to render graphical widgets (web, files, editors, AI) inline. See the
> [upstream README](https://github.com/wavetermdev/waveterm) for the full feature tour.

## What this fork adds

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
- **Light-mode polish** — themed the tab bar, tab close button, sidebar/widget icons, AI panel,
  popovers, workspace accent, and the CPU/Mem graphs so light themes look right.

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
