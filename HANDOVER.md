# Handover — current cross-machine state

Short, dated snapshot of work that spans machines (macOS / Linux / Windows), so picking the fork
up on any box starts from the truth. For *what* the fork adds see [FORK.md](./FORK.md); for *how*
to build/release see [BUILDING.md](./BUILDING.md); the branch model + workflow live in
[CLAUDE.md](./CLAUDE.md).

> Public repo — keep this file public-safe. No secrets, IPs, hostnames, or per-machine infra.
> The detailed working plan and machine-specific handover steps are tracked **privately, outside
> this repo**.

## Active work in progress

- **Native OS notifications** — **implemented** on branch `feat/native-notifications` and verified
  working on Linux (notification fires, click focuses window + tab). Opt-in via
  `notify:commanddone`; threshold `notify:commanddonethresholdms` (default 30 s). Known limitation:
  the very first command in a fresh terminal isn't detected (bash-preexec shell-integration quirk,
  shared with the tab activity indicator). Not yet merged to `release`. Detailed notes are private.

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
