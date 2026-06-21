---
name: run-desktop
description: Build, run, and drive the Wave Terminal Electron desktop app for verification. Use when asked to start the app, screenshot it, click through its UI, or confirm a change works in the real app (not just tests).
---

Wave Terminal is an Electron app (TypeScript/React renderer) backed by a Go
`wavesrv` process. For agent/automated use, drive it via the Playwright
`_electron` REPL at `.kilocode/skills/run-desktop/driver.mjs`, which launches the
**built** app (no Vite dev server needed) and exposes click/type/screenshot
commands.

All paths below are relative to the repo root.

## Prerequisites (once)

```bash
# Playwright is vendored inside the skill dir (keeps the main package.json clean)
cd .kilocode/skills/run-desktop && npm install && cd -
```

The skill reuses the repo's own `node_modules/electron`, so no extra Electron
download. macOS runs directly; on **headless Linux** prefix the launch with
`xvfb-run -a`.

## Build (before each run, to pick up your changes)

```bash
task build:backend:quickdev      # rebuild wavesrv (Go) → dist/bin/wavesrv.<arch>
npm run build:dev                # electron-vite build → dist/main, dist/preload, dist/frontend
```

`npm run build:dev` prints a `sharp` image-optimizer warning — harmless (images
just aren't recompressed).

## Run (batch / scripted — the agent path)

Pipe a newline-separated script into the driver. It reads the whole script up
front (before launching, so Electron can't steal stdin) and runs each command
sequentially, awaiting each:

```bash
printf 'launch\nsleep 5000\nwidget git\nsleep 4000\nss git-view\ntext body\nquit\n' \
  | node .kilocode/skills/run-desktop/driver.mjs
```

Screenshots land in `/tmp/shots/` (override with `SCREENSHOT_DIR`). After a run,
open the PNGs and **look at them** — a blank frame means launch failed.

## Run (interactive — under tmux)

```bash
tmux new-session -d -s wave -x 220 -y 50
tmux send-keys -t wave 'node .kilocode/skills/run-desktop/driver.mjs' Enter
tmux send-keys -t wave 'launch' Enter
# wait for "launched", then:
tmux send-keys -t wave 'widget git' Enter
tmux send-keys -t wave 'ss git' Enter
tmux capture-pane -t wave -p
```

(TTY stdin → REPL mode; piped stdin → batch mode. Both run commands strictly
sequentially.)

### Commands

| command | what it does |
|---|---|
| `launch` | launch the built app, poll for the renderer window |
| `ss [name]` | screenshot → `/tmp/shots/<name>.png` |
| `widget <label>` | click a widget-bar item by its **exact** label (e.g. `git`, `terminal`) |
| `click <css-sel>` | DOM `.click()` the first match |
| `click-text <text>` | click a button/link/div containing the text |
| `type <text>` / `press <key>` | keyboard input into the focused element |
| `wait <css-sel>` | wait up to 10s for a selector |
| `text [css-sel]` | print innerText of selector (default `body`) — great for asserting UI state |
| `eval <js>` | evaluate JS in the renderer, print JSON |
| `windows` | list windows + webContents |
| `sleep <ms>` | pause |
| `quit` | close the app and exit |

## Pointing a view at a specific directory

Widget-bar blocks open with no cwd, so cwd-derived views (e.g. the Git view)
resolve to `~`. To drive a view against a specific repo/dir, add a temporary
user widget in the **dev** config dir, then remove it after:

```bash
cat > ~/.config/waveterm-dev/widgets.json <<'EOF'
{ "widget@gitrepo": { "display:order": 1, "icon": "code-branch", "label": "gitrepo",
  "blockdef": { "meta": { "view": "git", "git:root": "/abs/path/to/repo" } } } }
EOF
# launch, `widget gitrepo`, screenshot, then:
rm -f ~/.config/waveterm-dev/widgets.json
```

User widgets (keys not starting with `defwidget@`) are merged on top of the
built-in ones.

## Gotchas

- **`wavesrv` exits instantly with `invalid wcloud endpoint`** unless the
  `WCLOUD_*` env vars are set. The driver sets them (mirroring
  `Taskfile.yml`'s `electron:quickdev`); don't strip them from the launch env.
- **`widget <label>` must match the leaf label, not a container.** The widget
  bar's container `textContent` includes every label, so a naive
  `includes('git')` clicks the whole bar. The driver matches the element whose
  own trimmed text **equals** the label.
- **Dev runs use a separate identity** ("Wave (Dev)", data dir
  `~/Library/Application Support/waveterm-dev`, config `~/.config/waveterm-dev`)
  — it won't touch a production Wave install, but blocks you open during a run
  **persist** in the dev workspace. Close them afterwards if you care.
- **Single instance:** only one Wave (Dev) at a time. Quit any running dev
  instance before launching, or the new one exits without a window.
- **Batch mode reads all of stdin before launching** — that's deliberate;
  Electron grabs the stdin fd on launch, which would otherwise drop later
  piped commands.

## Troubleshooting

- **Launch timeout / no window:** check `~/Library/Application Support/waveterm-dev/waveapp.log`
  — it has both the Electron main and `wavesrv` output. Most "no window"
  failures are `wavesrv` aborting at startup (see the wcloud gotcha).
- **Stale build:** re-run the two build commands. The renderer loads from
  `dist/frontend/index.html` and `wavesrv` from `dist/bin/wavesrv.<arch>`.
