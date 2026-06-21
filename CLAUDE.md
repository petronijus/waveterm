# Working in this fork (petronijus/waveterm)

Personal fork of [Wave Terminal](https://github.com/wavetermdev/waveterm). This top section is
the **fork operational guide** — read it before branch / merge / release / build work. Companion
docs: **[FORK.md](./FORK.md)** (what the fork adds + roadmap) and **[BUILDING.md](./BUILDING.md)**
(per-OS build detail). Wave's own engineering skill guides are kept below.

> This repo is **public**. Never commit secrets, IPs, hostnames, tokens, or internal infra.
> Machine-specific build/deploy details stay out of this repo.

## Commit identity

Personal fork → always commit as **`petronijus@bastla.com`** (name `petronijus`), never the work
email. Set per checkout before committing:

```sh
git config user.email petronijus@bastla.com
git config user.name petronijus
```

## Branches

- **`main`** — mirrors upstream `wavetermdev/waveterm`. **Never commit fork work here**; only
  `git merge --ff-only upstream/main`. If fork commits ever land on `main`, reset it (needs
  explicit approval): `git push origin --force-with-lease <clean-upstream-commit>:main`.
- **`feat/<task>`** — one branch per task (e.g. `feat/theme-picker`, `feat/activity-indicator`),
  each focused on a single task so it stays a clean, rebaseable series.
- **`release`** — integration branch (`main` + merged task branches). **Builds & releases come
  from `release`**, and new task branches are cut from `release`.
- **`feat/dev-channel`** — build-only variant that rebrands the app as a side-by-side
  **"Wave (Dev)"** install (own identity + data dir + single-instance lock; apps launcher / dev
  widgets on by default). **Never merge into `release`** — it would rebrand the prod build. Keep
  it rebased on `release`; build from it for a dev install that runs next to a stock Wave.

## Workflow — one task = one branch = one merge

```sh
# 1) start a task
git checkout release && git pull
git checkout -b feat/<task>

# 2) work, committing as you go (identity = petronijus@bastla.com)

# 3) finish → fold into release + push
git checkout release
git merge feat/<task>            # fast-forward when release hasn't moved
git push origin release feat/<task>
```

No `--no-verify`. No force-push to `main` (except the documented recovery, with explicit approval).
Prefer new commits over amends.

## Parallel work across windows (worktrees)

This repo is often worked on from **two sessions at once**. To keep them from fighting over
the index, working tree, and `./make`, give each session its own **git worktree** — one clone,
separate directories, separate branches, shared history:

```sh
git worktree add ../waveterm-dev   feat/dev-channel   # build/test "Wave (Dev)" here
git worktree add ../waveterm-<task> feat/<task>        # one per in-flight feature
git worktree list                                      # see who's where
git worktree remove ../waveterm-<task>                 # when the task is merged
```

Convention: the main checkout (`~/Documents/Dev/waveterm`) stays on **`release`**; each feature /
the dev-channel lives in its own worktree. A fresh worktree has no deps — run `task init` in it
once before the first build. Each worktree has its own `./make`, so two `task package` builds can
run at once without clobbering each other.

Rules that hold **with or without** worktrees:
- **One branch per session** — never two sessions on `release` doing merges.
- **`git fetch` immediately before** any compare / merge / rebase / push — branch pointers move
  under you; never trust a cached `origin/*`.
- **Never run two `task package` builds in the same working tree** (`task package` does
  `rm -rf make` and spawns long-lived children — concurrent builds collide and orphan processes).
- An agent picking this repo up should `git fetch` and re-read `git rev-parse HEAD` + `git status`
  before assuming any branch is where it left it.

## Tasks / planning

- Roadmap & planned features: **[FORK.md](./FORK.md)** ("Planned").
- The detailed working plan and cross-machine build-handover tasks are tracked **privately,
  outside this public repo** — don't reproduce them here.

## Staying current with upstream

```sh
git fetch upstream
git checkout main && git merge --ff-only upstream/main && git push origin main
git checkout feat/<task> && git rebase main     # then re-integrate into release
```

## Building & running

Prereqs (all OSes): **Go**, **Node**, **[Task](https://taskfile.dev)** (`go-task`), **Zig**.
Once per checkout: `task init`.

```sh
task dev               # dev app, hot reload
task electron:quickdev # faster dev (native arch, no docsite/wsh rebuild)
task package           # installer for the CURRENT OS → ./make
```

`task package` builds **only for the OS it runs on**, so a full release is built per-OS:

| OS | how / notes | artifacts in `./make` |
|----|-------------|------------------------|
| **macOS** | `task package` on a Mac. Unsigned without a cert (right-click → Open on first run). | `Wave-darwin-{arm64,x64}-<ver>.{dmg,zip}` |
| **Linux** | `task package` on Linux (+ electron-builder deps for deb/AppImage/snap). | `*.deb` / `*.AppImage` / `*.snap` |
| **Windows** | `task package` on Windows (Node/Go/Zig/Task on PATH; MSVC Build Tools if a native module rebuilds). | `*.exe` (NSIS) |

Dev-only widgets (the `dev` shortcut, the `apps` launcher) appear only in `task dev`, not in a
packaged build (`isDev()` gating). To show the apps launcher in a packaged build, set
`"feature:waveappbuilder": true` in `settings.json`.

## Releasing

1. Ensure `release` is built and tested.
2. Tag + create a GitHub pre-release:
   `gh release create <tag> --target release --prerelease --title "…" --notes "…" --repo petronijus/waveterm`
3. Build on each OS, then attach every artifact:
   `gh release upload <tag> ./make/<artifact> --repo petronijus/waveterm`

Version comes from the upstream base (e.g. `0.14.5`); fork releases tag as `v<ver>-pj.<n>`.

---

@.kilocode/rules/rules.md

---

## Skill Guides

This project uses a set of "skill" guides — focused how-to documents for common implementation tasks. When your task matches one of the descriptions below, **read the linked SKILL.md file before proceeding** and follow its instructions precisely.

| Skill        | File                                     | Description                                                                                                                                                                                                                                 |
| ------------ | ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| add-config   | `.kilocode/skills/add-config/SKILL.md`   | Guide for adding new configuration settings to Wave Terminal. Use when adding a new setting to the configuration system, implementing a new config key, or adding user-customizable settings.                                               |
| add-rpc      | `.kilocode/skills/add-rpc/SKILL.md`      | Guide for adding new RPC calls to Wave Terminal. Use when implementing new RPC commands, adding server-client communication methods, or extending the RPC interface with new functionality.                                                 |
| add-wshcmd   | `.kilocode/skills/add-wshcmd/SKILL.md`   | Guide for adding new wsh commands to Wave Terminal. Use when implementing new CLI commands, adding command-line functionality, or extending the wsh command interface.                                                                      |
| context-menu | `.kilocode/skills/context-menu/SKILL.md` | Guide for creating and displaying context menus in Wave Terminal. Use when implementing right-click menus, adding context menu items, creating submenus, or handling menu interactions with checkboxes and separators.                      |
| create-view  | `.kilocode/skills/create-view/SKILL.md`  | Guide for implementing a new view type in Wave Terminal. Use when creating a new view component, implementing the ViewModel interface, registering a new view type in BlockRegistry, or adding a new content type to display within blocks. |
| electron-api | `.kilocode/skills/electron-api/SKILL.md` | Guide for adding new Electron APIs to Wave Terminal. Use when implementing new frontend-to-electron communications via preload/IPC.                                                                                                         |
| waveenv      | `.kilocode/skills/waveenv/SKILL.md`      | Guide for creating WaveEnv narrowings in Wave Terminal. Use when writing a named subset type of WaveEnv for a component tree, documenting environmental dependencies, or enabling mock environments for preview/test server usage.          |
| wps-events   | `.kilocode/skills/wps-events/SKILL.md`   | Guide for working with Wave Terminal's WPS (Wave PubSub) event system. Use when implementing new event types, publishing events, subscribing to events, or adding asynchronous communication between components.                            |
| run-desktop  | `.kilocode/skills/run-desktop/SKILL.md`  | Build, run, and drive the Wave Terminal Electron app via a Playwright `_electron` REPL driver. Use to launch the app, screenshot it, click through its UI, or verify a change works in the real app (not just tests).                        |
