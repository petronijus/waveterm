# Handover — current cross-machine state

Short, dated snapshot of work that spans machines (macOS / Linux / Windows), so picking the fork
up on any box starts from the truth. For *what* the fork adds see [FORK.md](./FORK.md); for *how*
to build/release see [BUILDING.md](./BUILDING.md); the branch model + workflow live in
[CLAUDE.md](./CLAUDE.md).

> Public repo — keep this file public-safe. No secrets, IPs, hostnames, or per-machine infra.
> The detailed working plan and machine-specific handover steps are tracked **privately, outside
> this repo**.

## Active work in progress

- **Native OS notifications** (planned feature in FORK.md) — **design phase**. The approach is
  decided (frontend-driven, on the existing command-activity detection that powers the tab
  activity indicator); next steps are finishing the design decisions, then a spec, then
  implementation on a `feat/native-notifications` branch cut from `release`. No code branch yet.
  Full design notes are kept privately. Resume from there before writing code.

## Build / release status per OS

Built and released per-OS (no hosted CI) — see BUILDING.md. Current tag: `v0.14.5-pj.1`.

| OS | toolchain set up | latest build | published artifacts |
|----|------------------|--------------|---------------------|
| **macOS** | yes | `v0.14.5-pj.1` | `Wave-darwin-{arm64,x64}` (dmg/zip), unsigned-dev |
| **Linux** | build on first use | — | not yet built |
| **Windows** | build on first use | — | not yet built (mind the `task package` gotcha in BUILDING.md) |

To produce the missing builds: on that OS, install the prereqs (Go / Node / Task / Zig — see
BUILDING.md), `git checkout release`, `task init`, then `task package`, and upload the artifacts to
the `v0.14.5-pj.1` GitHub release.

## Per-machine reminders

- **Commit identity** — set `git config user.email petronijus@bastla.com` (name `petronijus`)
  in this checkout before committing; this is a personal fork, never the work email.
- Local checkouts are usually on a detached tag or `main` only — `git fetch` and check out
  `release` before starting a task.
