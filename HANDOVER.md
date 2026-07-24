# Handover — current cross-machine state

Short, dated snapshot of work that spans machines (macOS / Linux / Windows), so picking the fork
up on any box starts from the truth. For *what* the fork adds see [FORK.md](./FORK.md); for *how*
to build/release see [BUILDING.md](./BUILDING.md); the branch model + workflow live in
[CLAUDE.md](./CLAUDE.md).

> Public repo — keep this file public-safe. No secrets, IPs, hostnames, or per-machine infra.
> The detailed working plan and machine-specific handover steps are tracked **privately, outside
> this repo**.

## Auto-update wired to the fork's own releases (as of 2026-07-24)

**Committed on `feat/fork-autoupdate` (branch pushed), NOT yet built or runtime-tested.**
Makes `task version -- pj` / the GitHub feed actually deliver updates instead of silently
no-op'ing. Three fixes in `97cc9822`:

- **Versioning** — `package.json` used to stay at a bare `0.14.5` for every pj release, so an
  installed app never saw a version change and reported "up to date" forever. New `pj` action in
  `version.cjs` keeps the upstream base and appends the fork iteration (`0.14.5-pj.11`, `-pj.12`,
  …); `package.json` is seeded at `0.14.5-pj.10` so the next bump yields `-pj.11`.
- **Update channel** — the `pj` prerelease identifier is also the updater channel.
  `electron-builder.config.cjs` now sets `publish.channel = "pj"`; without it the tag-derived
  channel in `GitHubProvider` matches no release.
- **Stale setting** — existing installs carry `autoupdate:channel: "latest"`; `emain/updater.ts`
  now lets the binary's channel win over that stale setting.

macOS signing/notarization is **opt-in via env** (`APPLE_TEAM_ID` + `APPLE_ID` present activate
`mac.notarize`), using the Developer ID Application cert now in 1Password. Plain local builds are
unaffected. Squirrel.Mac only applies an update whose bundle is signed with the **same** identity
as the running app, so mac auto-update needs the signed build; Windows/Linux (AppImage) auto-update
on an unsigned build.

⚠️ The GitHub provider ignores `generateUpdatesFilesForAllChannels`, so it emits only `pj*.yml`.
The release step must **also** upload a `latest*.yml` copy per OS (+ every `.blockmap`) or older
`0.14.5` installs — which fetch only `latest-*.yml` — see no update. Full step list is in
[CLAUDE.md](./CLAUDE.md) "Releasing".

### Next up — per OS
- **Linux (do this here):** `git fetch && git checkout feat/fork-autoupdate`, `task init` if
  needed, `task version -- pj` → `0.14.5-pj.11`, `task package` (unsigned AppImage auto-updates
  fine). Then the **end-to-end test nobody has run yet**: cut the pj.11 GitHub release with the
  AppImage + `pj-linux.yml` + a `latest-linux.yml` copy + blockmap, install pj.11, bump to pj.12,
  release, and confirm the pj.11 install actually offers and applies the update.
- **macOS:** needs the Developer ID env (`CSC_LINK`/`CSC_KEY_PASSWORD`/`APPLE_ID`/
  `APPLE_APP_SPECIFIC_PASSWORD`/`APPLE_TEAM_ID` from 1Password) wired into the private release
  skill, then a signed+notarized build. **Open question:** confirm `APPLE_ID` value (likely
  `petronijus@bastla.com`) and add it as a `username` field on the 1Password app-specific-password
  item.
- **Windows:** unsigned NSIS auto-updates; just needs a pj.11 build + `pj.yml`/`latest.yml`/blockmap
  uploaded.

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

| OS | toolchain set up | latest local build | notes |
|----|------------------|--------------------|-------|
| **macOS** | yes | `release` @ 2026-06-21, **signed** (Apple Dev cert, Team ID on file) | Wave + Wave (Dev) built & installed; notifications confirmed working on the **signed** Wave (Dev). |
| **Linux** | build on first use | — | next: build `feat/fork-autoupdate` @ pj.11 + run the auto-update end-to-end test (unsigned AppImage OK); also still owes agent-waiting test |
| **Windows** | yes | `release` @ 2026-06-21, signed (cert auto-found in the Windows store) | Wave + Wave (Dev) built & installed side-by-side. Built `nsis`+`zip` only (MSI skipped); two-step backend build (`task --force build:backend` before electron-builder) avoids the wavesrv-drop gotcha — see BUILDING.md. |

## Per-machine reminders

- **Commit identity** — set `git config user.email petronijus@bastla.com` (name `petronijus`)
  in this checkout before committing; this is a personal fork, never the work email.
- Local checkouts are usually on a detached tag or `main` only — `git fetch` and check out
  `release` before starting a task.
