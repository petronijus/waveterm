# Building & releasing the fork

This fork is built **per-platform on each OS** (no hosted CI) — macOS on a Mac,
Linux on a Linux box/VM, Windows on a Windows box/VM. Build from the `release`
branch (or whatever branch you want to ship).

## Prerequisites (all platforms)

- **Go** (1.24+)
- **Node** (22+) + npm
- **[Task](https://taskfile.dev)** (`go-task`) — the build runner
- **Zig** — used to cross-compile the `wsh` helper

Then, once per checkout:

```sh
git clone https://github.com/petronijus/waveterm.git
cd waveterm
git checkout release
task init        # npm install + go mod tidy (+ docsite deps)
```

## Run the dev app (any OS)

```sh
task dev         # full dev app with hot reload
task electron:quickdev   # faster: arm64/native only, no docsite, no wsh rebuild
```

Note: dev-only widgets (the `dev` shortcut and the `apps` launcher) appear in `task dev`
but **not** in a packaged build — that's expected (`isDev()` gating). To show the apps
launcher in a packaged build, set `"feature:waveappbuilder": true` in `settings.json`.

## Package an installer

```sh
task package     # builds an installer for the CURRENT platform → ./make
```

`task package` only builds for the OS it runs on. Artifacts land in `./make`:

| OS | toolchain notes | artifacts in `./make` |
|----|-----------------|------------------------|
| **macOS** | Xcode CLT. Signing optional — with no cert it's unsigned (right-click → Open on first launch). | `Wave-darwin-{arm64,x64}-<ver>.{dmg,zip}` |
| **Linux** | system build deps for electron-builder targets (e.g. `rpm`, `fakeroot`, `snapcraft` for snap; AppImage/deb work out of the box on most distros) | `*.deb` / `*.AppImage` / `*.snap` |
| **Windows** | Node/Go/Zig/Task on PATH; a recent MSVC / Build Tools if any native module needs rebuild | `*.exe` (NSIS installer) |

## Publish to a GitHub release

Build on each OS, then attach the artifacts to the fork's release from any machine
with `gh`:

```sh
gh release upload <tag> ./make/<artifact> --repo petronijus/waveterm
# e.g. gh release upload v0.14.5-pj.1 ./make/Wave-linux-x86_64.AppImage --repo petronijus/waveterm
```

(Create the release first with `gh release create <tag> --prerelease --title "…" --notes "…"`.)
