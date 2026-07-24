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

| OS          | how / notes                                                                                         | artifacts in `./make`                     |
| ----------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| **macOS**   | `task package` on a Mac. Unsigned without a cert (right-click → Open on first run).                 | `Wave-darwin-{arm64,x64}-<ver>.{dmg,zip}` |
| **Linux**   | `task package` on Linux (+ electron-builder deps for deb/AppImage/snap).                            | `*.deb` / `*.AppImage` / `*.snap`         |
| **Windows** | `task package` on Windows (Node/Go/Zig/Task on PATH; MSVC Build Tools if a native module rebuilds). | `*.exe` (NSIS)                            |

Dev-only widgets (the `dev` shortcut, the `apps` launcher) appear only in `task dev`, not in a
packaged build (`isDev()` gating). To show the apps launcher in a packaged build, set
`"feature:waveappbuilder": true` in `settings.json`.

## Releasing

1. Ensure `release` is built and tested.
2. Bump the version — `task version -- pj` (`0.14.5-pj.10` → `-pj.11` → `-pj.12`, …). Commit it.
   **The version must change every release or auto-update is a no-op**: electron-updater
   compares `package.json` versions, never git tags, so ten releases all reading `0.14.5`
   look identical to an installed app.
3. Tag + create the GitHub release. **Not a pre-release** — GitHub only puts the "Latest"
   badge on a full release, so marking these as pre-releases left the badge stranded on an
   old version:
   `gh release create <tag> --target release --latest --title "…" --notes "…" --repo petronijus/waveterm`
4. Build on each OS, then attach every artifact:
   `gh release upload <tag> ./make/<artifact> --repo petronijus/waveterm`
5. **Attach the update manifests and blockmaps** — without them the updater 404s and every
   client reports "up to date" forever. Per OS, upload `./make/pj*.yml`, plus a copy renamed
   to the `latest*` name, plus every `*.blockmap`:

   | OS      | built manifest | also upload as     | why the copy                                                                                                                          |
   | ------- | -------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
   | macOS   | `pj-mac.yml`   | `latest-mac.yml`   | installs still on a non-prerelease version (`0.14.5` and earlier) run with `allowPrerelease=false` and only ever fetch `latest-*.yml` |
   | Windows | `pj.yml`       | `latest.yml`       | same                                                                                                                                  |
   | Linux   | `pj-linux.yml` | `latest-linux.yml` | same                                                                                                                                  |

   electron-builder will not generate the `latest*` copies itself: `generateUpdatesFilesForAllChannels`
   is ignored for the GitHub provider (`app-builder-lib/out/publish/updateInfoBuilder.js:39`),
   so the rename is manual. The blockmaps are what make updates download a delta instead of
   the full ~200 MB.

**Upload as its own step, not chained onto the build.** A dropped SSH connection to a build VM
once killed electron-builder mid-write and produced a 474 KB "installer" instead of 158 MB.
Before uploading, compare artifact sizes against the previous release — a truncated artifact is
otherwise indistinguishable from a good one.

Fork releases keep the upstream base they were cut from and append the fork iteration:
base `0.14.5` → `0.14.5-pj.11`, `0.14.5-pj.12`, … and the tag matches (`v0.14.5-pj.11`).
The version in `package.json` now carries the `-pj.N` suffix too — before pj.11 it read a
bare `0.14.5` on every release, which is why auto-update could never work.

The counter is **global** — an upstream merge that lifts the base does _not_ reset it:
`0.14.5-pj.11` → `0.14.6-pj.12`. One always-growing number identifies a fork build regardless
of the base it sits on (ordering still works: the base is compared first). Because the merge
overwrites `package.json` with a bare upstream version, `version.cjs` recovers the last used N
from the `v*-pj.*` git tags, so bump only from a checkout that has the fork tags fetched.

The pj number is also shown in the app itself: a `pj.N` badge sits at the right end of the tab
bar (and in the vertical tab bar's macOS header) — click opens the About dialog with the full
version (`frontend/app/tab/versionbadge.tsx`).

One-time caveat, already spent: semver ranks `0.14.5-pj.N` _below_ a plain `0.14.5`, so the
pj.1–pj.10 builds (all reporting `0.14.5`) cannot auto-update to pj.11 and need a manual
reinstall — which they need anyway, since they are unsigned and pj.11 onward is signed.

The `pj` prerelease identifier doubles as the **update channel**, wired in
`electron-builder.config.cjs` (`publish.channel`) — the tag's channel and the updater's
channel have to agree or `GitHubProvider` matches no release at all.

### macOS signing

Release builds are signed with the **Developer ID Application** cert (Team `ASFPR2T2DQ`) and
notarized; Squirrel.Mac refuses to install an update whose bundle isn't signed with the same
identity as the running app, so an unsigned mac build can download an update but never apply
it. Signing activates only when the env is present — `CSC_LINK`, `CSC_KEY_PASSWORD`,
`APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID` — so plain local builds are
unaffected. Credentials live in 1Password; the release skill pulls them, they are never
stored in this repo.

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
| run-desktop  | `.kilocode/skills/run-desktop/SKILL.md`  | Build, run, and drive the Wave Terminal Electron app via a Playwright `_electron` REPL driver. Use to launch the app, screenshot it, click through its UI, or verify a change works in the real app (not just tests).                       |
