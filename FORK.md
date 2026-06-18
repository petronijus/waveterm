# Fork notes — petronijus/waveterm

Status and plans for this personal fork of [Wave Terminal](https://github.com/wavetermdev/waveterm).
See [README](./README.md) for what the fork is and how to build it; upstream's own roadmap lives
in [ROADMAP.md](./ROADMAP.md).

## Branch model

- `main` — mirrors `wavetermdev/waveterm`, never carries fork features (only `git merge --ff-only upstream/main`).
- Topic branches — `feat/wave-theme`, `backport/ssh`, `feat/theme-picker`, etc. Each kept as a clean, rebaseable patch series.
- `release` — integration branch (`main` + merged topic branches); this is what gets built and released.

Staying current = rebase topic branches onto `main`, not merge.

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
- **Releases** — built per-platform and published on the fork's GitHub Releases (macOS first;
  Linux & Windows builds run on local machines — no hosted CI).

## Planned

- **Tab activity indicator** — show in the tab when a terminal is *working* (a long-running
  foreground command) vs *done*, generically — not tied to one specific tool. Built on the
  existing tab badge system; detection via shell-integration / command lifecycle.
- **Native OS notifications** — fire a system notification when a long command finishes, mainly
  when the window is unfocused. Builds on the activity detection above. Opt-in, throttled.
- **Sync** — sync tabs, settings, and window layout between machines.
- **Bookmarks in the Files view** — bookmark/favorite files & folders in the file browser for
  quick access.

## Known upstream bug to fix / report

- `wsh` install detection can misparse on hosts where `/bin/sh` is dash, due to stdout/stderr
  buffering order in the bootstrap command (`pkg/remote/conncontroller`). The logic is
  byte-identical in upstream and `waveterm-remote` — a clean upstream bug; candidate for our own
  patch plus an upstream report.
