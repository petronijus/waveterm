# Handover — current cross-machine state

Short, dated snapshot of work that spans machines (macOS / Linux / Windows), so picking the fork
up on any box starts from the truth. For _what_ the fork adds see [FORK.md](./FORK.md); for _how_
to build/release see [BUILDING.md](./BUILDING.md); the branch model + workflow live in
[CLAUDE.md](./CLAUDE.md).

> Public repo — keep this file public-safe. No secrets, IPs, hostnames, or per-machine infra.
> The detailed working plan and machine-specific handover steps are tracked **privately, outside
> this repo**.

## Auto-update wired to the fork's own releases (as of 2026-07-24)

**DONE and runtime-verified on Linux (2026-07-24).** Everything ships in release
`v0.14.5-pj.11`; the interim test releases (pj.12–pj.14, used only for the end-to-end
verification: run old, updater finds + downloads new, Restart, app comes back as the new
version) were deleted afterwards so the numbering reflects real features — the next feature
release is `pj.12`. Original wiring in `97cc9822`:

- **Versioning** — `package.json` used to stay at a bare `0.14.5` for every pj release, so an
  installed app never saw a version change and reported "up to date" forever. New `pj` action in
  `version.cjs` keeps the upstream base and appends the fork iteration (`0.14.5-pj.11`, `-pj.12`,
  …); `package.json` is seeded at `0.14.5-pj.10` so the next bump yields `-pj.11`.
- **Update channel** — the `pj` prerelease identifier is also the updater channel.
  `electron-builder.config.cjs` now sets `publish.channel = "pj"`; without it the tag-derived
  channel in `GitHubProvider` matches no release.
- **Stale setting** — existing installs carry `autoupdate:channel: "latest"`; `emain/updater.ts`
  now lets the binary's channel win over that stale setting.

Added while testing (all on `feat/fork-autoupdate`):

- **Global pj counter** (`170a00e6`) — an upstream base bump no longer resets the number
  (`0.14.5-pj.11` → `0.14.6-pj.12`); `version.cjs` recovers the last used N from the `v*-pj.*`
  git tags, so bump from a checkout with the fork tags fetched.
- **Version badge** (`170a00e6`) — a `pj.N` badge at the right end of the tab bar (and in the
  vertical tab bar's macOS header); click opens About (`frontend/app/tab/versionbadge.tsx`).
- **Post-update restart fix** (`ac91a739`) — electron-updater's AppImage relaunch spawned the
  new instance while the old one still held the single-instance lock, so the update applied but
  the app never came back. `installUpdate()` now installs silently and a detached waiter starts
  the new AppImage only after the old process exits. **The running (old) version performs the
  relaunch, so the fix only helps once a machine runs pj.11 or newer.**
- Linux build deps for a full `task package`: `rpm` (rpmbuild) + `libarchive-tools` (bsdtar for
  the pacman target) — without them the failed target aborts the publish phase and **no
  `pj-linux.yml` is generated**.

macOS signing/notarization is **opt-in via env** (`APPLE_TEAM_ID` + `APPLE_ID` present activate
`mac.notarize`), using the Developer ID Application cert now in 1Password. Plain local builds are
unaffected. Squirrel.Mac only applies an update whose bundle is signed with the **same** identity
as the running app, so mac auto-update needs the signed build; Windows/Linux (AppImage) auto-update
on an unsigned build.

⚠️ The GitHub provider ignores `generateUpdatesFilesForAllChannels`, so it emits only `pj*.yml`.
The release step must **also** upload a `latest*.yml` copy per OS (+ every `.blockmap`; on Linux
the AppImage blockmap is embedded, no separate file) or older `0.14.5` installs — which fetch only
`latest-*.yml` — see no update. Full step list is in [CLAUDE.md](./CLAUDE.md) "Releasing".

Semver caveat (one-time): pj.1–pj.10 installs report a bare `0.14.5` and can never see the pj
prereleases — every machine needs **one manual reinstall** of pj.11+; auto-update flows from
there on.

### Per-OS status (all pj.11 artifacts are on the release)

- **Linux:** done — built + e2e-verified on the desktop; prod `/opt/Wave` reinstalled from the
  pj.11 deb.
- **Windows:** done — built on the build VM (NSIS signed via the cert in the Windows store),
  `.exe`/`.zip` + `pj.yml`/`latest.yml`/blockmap uploaded. Remaining: install pj.11 on the
  actual Windows machines once (older installs can't see pj prereleases).
- **macOS:** built on the build VM **signed with Developer ID** (arm64 + x64 dmg/zip +
  `pj-mac.yml`/`latest-mac.yml`/blockmaps uploaded). The `errSecInternalComponent`
  SSH-codesign failure is solved by importing the .p12 and running
  `security set-key-partition-list -S apple-tool:,apple:` on the login keychain — no GUI
  session needed. Remaining: **notarization** (needs the `APPLE_ID` e-mail confirmed and set
  as `username` on the 1Password app-specific-password item; team is `ASFPR2T2DQ`, cert says
  "Petr Parkan Janda") and a one-time pj.11 install on the real Macs.

## Manual session sync — Save/Load (as of 2026-06-24)

**Implemented on `release`, NOT yet runtime-tested.** Replaces the background autosync with a
**manual** model: two buttons in the top-right of the tab bar — **Save session** (☁↑) and **Load
session** (☁↓). Save writes one `session.json` snapshot (workspaces + tabs + blocks + layouts +
open windows incl. position/size) to the configured transport; Load restores it — upserts the
objects and reconciles the OS windows (opens missing, closes extras, **never the last**). Config
files are **excluded** from the snapshot so a Load never clobbers a machine's own settings
(including the sync transport config itself). The background scheduler is **disabled**
(`main-server.go`). A new `electron:newwindow` event lets Go open a window; window identity is
keyed by **workspaceid** (the per-machine window OID can't be shared across installs).

Files: `pkg/wsync/session.go`, `pkg/wcore/window.go` (`OpenWindowForSync`,
`CloseWindowKeepWorkspace`, `WindowForWorkspace`), `pkg/eventbus/eventbus.go`, RPC
`SaveSession`/`LoadSessionCommand` (wshrpctypes + wshserver), `frontend/app/tab/vtabbar.tsx`.

### Next up — per OS

- **All OSes:** pull `release`, `task init` if needed, build. Configure a transport in Settings
  (`sync:folderpath` = a Nextcloud desktop-client folder, or WebDAV). Then **test**: Save on one
  machine, Load on another → workspaces/tabs/blocks restore and the saved windows open at their
  saved positions (Electron clamps to the local display).
- **macOS:** buttons already present (macOS tab-bar header) — test Save/Load + window open/close,
  including Load closing local extra windows and the never-close-last-window guard.
- **Windows / Linux:** the Save/Load buttons currently render **only in the macOS header**
  (`MacOSHeader` in `vtabbar.tsx`). **Add them to the Windows/Linux header** before testing there.

## Active work in progress (as of 2026-06-21)

All of the below is **merged to `release`** and pushed.

- **Native OS notifications** — command-done notification (`notify:commanddone`, threshold
  `notify:commanddonethresholdms`, default 30 s) plus a distinct **"agent is waiting for you"**
  tab state + OS notification (always on, no setting) for **Claude Code / Gemini CLI / Codex**.
  Trigger is the terminal **bell** the agent rings on "your turn" **or an OSC 9 notification**
  (Gemini/Codex prefer OSC 9, bell fallback). Scoped via `agentKindForCommand`. Known limitation:
  the very first command in a fresh terminal isn't detected (bash-preexec quirk).
- **Sync** (config + workspaces): WebDAV (Nextcloud) **and** a credential-free **local-folder**
  transport (`sync:folderpath`, e.g. a Nextcloud desktop-client folder). `pkg/wsync`.
- **⚠️ macOS-only finding — notifications need a SIGNED build.** macOS (`UNUserNotificationCenter`)
  silently drops notifications from **ad-hoc / unsigned** apps and never registers them in System
  Settings → Notifications. So on macOS you MUST sign (Apple Development cert, with network so the
  timestamp step succeeds) — see BUILDING.md "macOS notes". **Linux/Windows are unaffected**
  (notifications work on an unsigned build).

### Next up (continue on Linux)

- Pull `release`, `task init` if needed, `task package` (Linux notifications work **unsigned**).
- **Functionally test** the agent-waiting feature: run claude/gemini/codex, finish a turn with the
  Wave window unfocused → tab should flip to "waiting" + an OS notification fires. Enable each
  agent's bell/notification (Gemini `enableTerminalBell`; Codex `auto`; Claude terminal bell).

## Build / release status per OS

Built per-OS (no hosted CI) — see BUILDING.md.

| OS          | toolchain set up   | latest local build                                                    | notes                                                                                                                                                                                                                    |
| ----------- | ------------------ | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **macOS**   | yes                | `release` @ 2026-06-21, **signed** (Apple Dev cert, Team ID on file)  | Wave + Wave (Dev) built & installed; notifications confirmed working on the **signed** Wave (Dev).                                                                                                                       |
| **Linux**   | build on first use | —                                                                     | next: build `feat/fork-autoupdate` @ pj.11 + run the auto-update end-to-end test (unsigned AppImage OK); also still owes agent-waiting test                                                                              |
| **Windows** | yes                | `release` @ 2026-06-21, signed (cert auto-found in the Windows store) | Wave + Wave (Dev) built & installed side-by-side. Built `nsis`+`zip` only (MSI skipped); two-step backend build (`task --force build:backend` before electron-builder) avoids the wavesrv-drop gotcha — see BUILDING.md. |

## Per-machine reminders

- **Commit identity** — set `git config user.email petronijus@bastla.com` (name `petronijus`)
  in this checkout before committing; this is a personal fork, never the work email.
- Local checkouts are usually on a detached tag or `main` only — `git fetch` and check out
  `release` before starting a task.
