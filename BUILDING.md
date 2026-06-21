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
| **macOS** | Xcode CLT + an `Apple Development` cert in your keychain. **See "macOS notes" below** — sign the build (ad-hoc/unsigned silently breaks notifications), and run with network so the timestamp step succeeds. | `Wave-darwin-{arm64,x64}-<ver>.{dmg,zip}` |
| **Linux** | system build deps for electron-builder targets (e.g. `rpm`, `fakeroot`, `snapcraft` for snap; AppImage/deb work out of the box on most distros) | `*.deb` / `*.AppImage` / `*.snap` |
| **Windows** | Node/Go/Zig/Task on PATH; a recent MSVC / Build Tools if any native module needs rebuild. **See "Windows notes" below — a bare `task package` can ship a broken installer.** | `*.exe` (NSIS) + `*.zip` |

### macOS notes

- **Sign the build — notifications need it.** macOS delivers Electron notifications through
  `UNUserNotificationCenter`, which requires a real code signature (a Team ID). An
  **ad-hoc / unsigned** app is *silently* dropped: no banners, and the app never even appears
  in **System Settings → Notifications**. (Linux/Windows don't have this requirement — that's
  why notifications worked there but not on a `CSC_IDENTITY_AUTO_DISCOVERY=false` macOS build.)
  So let electron-builder sign with your `Apple Development` cert (auto-discovered from the
  keychain — do **not** pass `CSC_IDENTITY_AUTO_DISCOVERY=false`):
  ```sh
  task package          # signs with the Apple Development cert in your keychain
  ```
- **The timestamp "failure" was a sandboxed build, not the cert.** `codesign --timestamp`
  contacts `timestamp.apple.com`; if the build runs with no network it aborts with
  `A timestamp was expected but was not found` (electron-builder exits 201). Run the package
  step with normal network access and it timestamps fine. (Signing every Electron component
  with a per-file timestamp request is slow — a signed package takes noticeably longer.)
- **Gatekeeper & launch.** An `Apple Development`-signed (non-notarized) app is `spctl`-rejected
  but still runs locally because a locally-built app carries no `com.apple.quarantine`. Launch
  via Finder/`open`. If a launch flakes (app exits immediately), it's a stale process/lock —
  `pkill -9 -f WaveDev` (or `Wave`) then `open` again.
- **Per-machine.** The cert lives only in your Mac's keychain, so only your Mac produces a
  signed (notification-capable) build. A machine without the cert yields an ad-hoc build with
  no notifications.

## Two channels side by side — "Wave" and "Wave (Dev)"

The fork ships as two coexisting installs (see [CLAUDE.md](./CLAUDE.md) for the branch model):

- **"Wave"** — your stable/work install, built from `release`.
- **"Wave (Dev)"** — your testing install, built from `feat/dev-channel` (= `release` + a
  rebrand: `productName "Wave (Dev)"`, appId `dev.commandline.waveterm-dev`, own data dir
  `waveterm-dev`, own single-instance lock, apps launcher on by default). Distinct identity +
  data dir → it runs next to a stock Wave and won't touch its settings.

Keep the dev channel current and build it:

```sh
git checkout feat/dev-channel && git rebase release   # carry the rebrand on top of latest release
git push --force-with-lease origin feat/dev-channel   # rebase rewrites history → force-with-lease
CSC_IDENTITY_AUTO_DISCOVERY=false task package         # → "Wave (Dev)-darwin-…" artifacts
```

> ⚠️ **`task package` runs `clean` first (`rm -rf make`).** Building the second channel wipes
> the first channel's artifacts from `./make`. Copy them aside before the second build:
> ```sh
> mkdir -p artifacts/release && cp -p "make/Wave-darwin-"*.{dmg,zip} artifacts/release/
> ```
> The two channels' artifacts are named differently (`Wave-darwin-…` vs `Wave (Dev)-darwin-…`),
> so once both are in `./make` together they don't collide — only the `clean` step is the hazard.

### Windows notes (hard-won)

Symptom of most of these: the installer builds fine, but the **packaged app launches
with no window**. Check `%LOCALAPPDATA%\waveterm-dev\Data\waveapp.log` (or
`waveterm\Data` for a prod build) — `error running wavesrv ... ENOENT` means the
backend binary didn't make it into the package.

- **`wavesrv` silently dropped from the package.** `task package` runs `clean` and
  `build:backend` in parallel; once Task has cached `build:server`/`build:wsh` as
  up-to-date it *skips* them, while `clean` wipes `dist/bin` — so the installer ships
  without `wavesrv.x64.exe`. Build in two explicit steps instead of a bare `task package`:
  ```sh
  CC="zig cc" task --force build:backend                 # force-rebuild wavesrv + wsh
  ls dist/bin/wavesrv.x64.exe                             # verify before packaging
  CSC_IDENTITY_AUTO_DISCOVERY=false \
    npx electron-builder -c electron-builder.config.cjs --win nsis zip -p never
  ```
- **`CC="zig cc"` is required.** The `generate` step does a native cgo build that
  defaults to `gcc` (absent on Windows) → `cgo: C compiler "gcc" not found`. Pointing
  `CC` at `zig cc` fixes it; the `wavesrv` cross-compile sets its own `-target` on top.
- **Skip the MSI target.** electron-builder auto-discovers a code-signing cert from the
  Windows store and signs the binaries; WiX `light.exe` then runs `-wx` (warnings =
  errors) and dies on the cert-table ICE check (`light.exe process failed 1032`). Build
  **`nsis` + `zip` only**; `CSC_IDENTITY_AUTO_DISCOVERY=false` keeps everything unsigned
  (matching the macOS build).
- **Broken global npm.** A self-installed global npm under `%APPDATA%\npm` shadows the
  Node-bundled one (the `npm.cmd` shim prefers the global prefix). If `npm install`
  throws `Class extends value undefined …minipass-sized`, remove/rename
  `%APPDATA%\npm\node_modules\npm` so the bundled npm is used.

## Publish to a GitHub release

Build on each OS, then attach the artifacts to the fork's release from any machine
with `gh`:

```sh
gh release upload <tag> ./make/<artifact> --repo petronijus/waveterm
# e.g. gh release upload v0.14.5-pj.1 ./make/Wave-linux-x86_64.AppImage --repo petronijus/waveterm
```

(Create the release first with `gh release create <tag> --prerelease --title "…" --notes "…"`.)
