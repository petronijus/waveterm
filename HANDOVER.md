# Handover — current cross-machine state

Short, dated snapshot of work that spans machines (macOS / Linux / Windows), so picking the fork
up on any box starts from the truth. For *what* the fork adds see [FORK.md](./FORK.md); for *how*
to build/release see [BUILDING.md](./BUILDING.md); the branch model + workflow live in
[CLAUDE.md](./CLAUDE.md).

> Public repo — keep this file public-safe. No secrets, IPs, hostnames, or per-machine infra.
> The detailed working plan and machine-specific handover steps are tracked **privately, outside
> this repo**.

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
| **Linux** | build on first use | — | next: build + test agent-waiting (unsigned OK) |
| **Windows** | build on first use | — | not yet built (mind the `task package` gotcha in BUILDING.md) |

## Per-machine reminders

- **Commit identity** — set `git config user.email petronijus@bastla.com` (name `petronijus`)
  in this checkout before committing; this is a personal fork, never the work email.
- Local checkouts are usually on a detached tag or `main` only — `git fetch` and check out
  `release` before starting a task.
