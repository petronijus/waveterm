// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0
//
// Regression smoke suite for the built dev app. Run after every bigger feature:
//
//   task build:backend:quickdev && npm run build:dev     # pick up your changes
//   node .kilocode/skills/run-desktop/smoke.mjs
//
// What it verifies (one app launch, sequential):
//   1. launch        — the built app starts and a renderer window appears
//   2. repaint-guard — the on-screen tab has background throttling enabled, so Chromium
//                      records its hidden state and repaints after an OS-level unmap
//                      (runs first: a tab that has been backgrounded once reads true
//                      regardless, so a later check would not catch the regression)
//   3. badge         — a running command gets its backend-driven spinner badge in the tab bar
//   4. throttle      — a tab flooding terminal output stops burning CPU once its tab
//                      goes to the background (macOS only; needs `top`)
//   5. webview-throttle — a webview guest process stops its rAF loop once its tab goes
//                      to the background (guests don't inherit the embedder's throttling)
//   6. notify-skip   — command-done is suppressed while the window is focused
//   7. notify-fire   — command-done queues and fires once the app is hidden (app.hide,
//                      no OS-focus races; macOS only)
//
// Notes:
//   - Runs in a throwaway sandbox (WAVETERM_DATA_HOME/WAVETERM_CONFIG_HOME under a
//     mkdtemp dir, removed afterwards): every run starts from a fresh workspace, so
//     no state accumulates between runs and the user's waveterm-dev dirs are never
//     touched. The fresh install means the onboarding modal appears — the suite
//     clicks through it.
//   - Exit code 0 = all pass / skipped, 1 = any failure.

import { execSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const SKILL_DIR = path.dirname(fileURLToPath(import.meta.url));
const APP_DIR = path.resolve(SKILL_DIR, "../../..");
const require = createRequire(path.join(SKILL_DIR, "package.json"));
const { _electron: electron } = require("playwright-core");

const IsMac = process.platform === "darwin";
const electronBin = IsMac
    ? path.join(APP_DIR, "node_modules/electron/dist/Electron.app/Contents/MacOS/Electron")
    : path.join(APP_DIR, "node_modules/electron/dist/electron");
const SandboxDir = fs.mkdtempSync(path.join(os.tmpdir(), "wave-smoke-"));
const SandboxDataDir = path.join(SandboxDir, "data");
const SandboxConfigDir = path.join(SandboxDir, "config");
const DevLogPath = path.join(SandboxDataDir, "waveapp.log");
const DevSettingsPath = path.join(SandboxConfigDir, "settings.json");

const results = [];
function report(name, ok, detail) {
    results.push({ name, ok, detail });
    console.log(`${ok === null ? "SKIP" : ok ? "PASS" : "FAIL"}  ${name}${detail ? ` — ${detail}` : ""}`);
}

function sleep(ms) {
    return new Promise((r) => setTimeout(r, ms));
}

// --- log watching (the main process logs [term-notify] decisions to waveapp.log)
let logOffset = 0;
function logTail() {
    try {
        const buf = fs.readFileSync(DevLogPath, "utf8");
        return buf.slice(logOffset);
    } catch {
        return "";
    }
}
function markLog() {
    try {
        logOffset = fs.statSync(DevLogPath).size;
    } catch {
        logOffset = 0;
    }
}
async function waitForLog(re, timeoutMs) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        const m = logTail().match(re);
        if (m) {
            return m;
        }
        await sleep(500);
    }
    return null;
}

// --- instantaneous CPU of this app's renderer processes (macOS top; second sample)
function rendererCpuSum() {
    const out = execSync(`top -l 2 -s 2 -stats pid,cpu,command 2>/dev/null | grep "Electron Hel" || true`, {
        encoding: "utf8",
    });
    const lines = out.trim().split("\n").filter(Boolean);
    const second = lines.slice(Math.floor(lines.length / 2));
    let sum = 0;
    for (const l of second) {
        const cpu = parseFloat(l.trim().split(/\s+/)[1]);
        if (!Number.isNaN(cpu)) {
            sum += cpu;
        }
    }
    return { sum, detail: second.map((s) => s.trim()).join(" | ") };
}

// --- sandbox settings (fresh dirs each run; the whole sandbox is removed at the end)
function prepareSettings() {
    fs.mkdirSync(SandboxDataDir, { recursive: true });
    fs.mkdirSync(SandboxConfigDir, { recursive: true });
    fs.writeFileSync(
        DevSettingsPath,
        JSON.stringify({ "notify:commanddone": true, "notify:commanddonethresholdms": 1500 }, null, 2)
    );
}

// --- page helpers
async function clickWidget(page, label) {
    return page.evaluate((t) => {
        const leaf = [...document.querySelectorAll("*")].find(
            (e) => e.childElementCount === 0 && (e.textContent || "").trim() === t
        );
        if (!leaf) {
            return "NOT_FOUND";
        }
        (leaf.closest("[class*='cursor-pointer']") || leaf).click();
        return "OK";
    }, label);
}

async function runTerminalCommand(page, cmd) {
    // Another block (a webview, an earlier flooding terminal) can hold keyboard
    // focus — diff the block ids around the widget click and focus the NEW block's
    // xterm textarea explicitly, so the typed command deterministically lands in
    // the fresh terminal.
    const blocksBefore = await page.evaluate(() =>
        [...document.querySelectorAll("[data-blockid]")].map((e) => e.getAttribute("data-blockid"))
    );
    const w = await clickWidget(page, "terminal");
    if (w !== "OK") {
        throw new Error("widget terminal not found");
    }
    await sleep(3000);
    await page.evaluate((prev) => {
        const fresh = [...document.querySelectorAll("[data-blockid]")].find(
            (e) => !prev.includes(e.getAttribute("data-blockid")) && e.querySelector(".xterm-helper-textarea")
        );
        fresh?.querySelector(".xterm-helper-textarea")?.focus();
    }, blocksBefore);
    await page.keyboard.type(cmd, { delay: 30 });
    await page.keyboard.press("Enter");
}

// ---------------------------------------------------------------------------
if (!fs.existsSync(path.join(APP_DIR, "dist/frontend/index.html"))) {
    console.error("dist/ missing — build first: task build:backend:quickdev && npm run build:dev");
    process.exit(1);
}
prepareSettings();
markLog();

// The webview-throttle check needs a page running a rAF loop. A data: URL gets
// rewritten to a web search by the webview's URL handling, so serve the page from
// a throwaway localhost server instead — self-contained, no network needed.
const rafServer = http.createServer((_req, res) => {
    res.writeHead(200, { "content-type": "text/html" });
    res.end(
        "<body style='background:#222;margin:0'><div id='c' style='font:64px monospace;color:#0f0;padding:40px'></div>" +
            "<script>window.__raf=0;(function l(){window.__raf++;document.getElementById('c').textContent=window.__raf;requestAnimationFrame(l)})()</script>"
    );
});
await new Promise((r) => rafServer.listen(0, "127.0.0.1", r));
const rafUrl = `http://127.0.0.1:${rafServer.address().port}/`;

let app = null;
let failedHard = false;
try {
    app = await electron.launch({
        executablePath: electronBin,
        args: IsMac ? [APP_DIR] : ["--no-sandbox", APP_DIR],
        cwd: APP_DIR,
        env: {
            ...process.env,
            WAVETERM_DATA_HOME: SandboxDataDir,
            WAVETERM_CONFIG_HOME: SandboxConfigDir,
            WAVETERM_NOCONFIRMQUIT: "1",
            WAVETERM_ENVFILE: path.join(APP_DIR, ".env"),
            WCLOUD_PING_ENDPOINT: "https://ping-dev.waveterm.dev/central",
            WCLOUD_ENDPOINT: "https://api-dev.waveterm.dev/central",
            WCLOUD_WS_ENDPOINT: "wss://wsapi-dev.waveterm.dev/",
        },
        timeout: 60_000,
    });

    let page = null;
    const deadline = Date.now() + 40_000;
    while (Date.now() < deadline) {
        page = app.windows().find((w) => w.url().includes("index.html")) ?? null;
        if (page) {
            try {
                await page.waitForLoadState("domcontentloaded", { timeout: 2_000 });
                break;
            } catch {
                /* keep waiting */
            }
        }
        await sleep(500);
    }
    if (!page) {
        throw new Error("no renderer window within 40s");
    }
    // Background mode (default): don't disturb the user — the app becomes a macOS
    // "accessory" app (no dock icon, no cmd-tab, focus returns to the user's app)
    // parked in a small corner window. It keeps rendering there, which the visible-
    // baseline measurements need — app.hide() would zero them. The focus-dependent
    // notify-skip check needs SMOKE_FOREGROUND=1.
    const Background = process.env.SMOKE_FOREGROUND !== "1";
    if (Background) {
        const parked = await app
            .evaluate(({ app: eApp, BaseWindow, screen }) => {
                if (process.platform === "darwin") {
                    eApp.dock?.hide();
                    eApp.setActivationPolicy?.("accessory");
                }
                const wa = screen.getPrimaryDisplay().workArea;
                // Wave windows are BaseWindow, not BrowserWindow — BrowserWindow.getAllWindows()
                // returns nothing here, so this silently parked no window at all.
                const wins = BaseWindow.getAllWindows();
                for (const w of wins) {
                    w.setBounds({ x: wa.x + wa.width - 810, y: wa.y + wa.height - 610, width: 800, height: 600 });
                    w.blur();
                }
                return wins.length;
            })
            .catch((e) => `error: ${e.message}`);
        if (typeof parked !== "number" || parked === 0) {
            console.log(`WARN  background mode parked no window (${parked}) — the app may steal focus`);
        }
    }
    await sleep(6000);
    report("launch", true, `${app.windows().length} window(s)${Background ? " [background mode]" : ""}`);

    // Fresh sandbox install → the onboarding modal is up. Click through its pages
    // ("Continue" accepts the TOS, "Maybe Later" skips the GitHub-star page).
    for (let i = 0; i < 8; i++) {
        const clicked = await page.evaluate(() => {
            const labels = ["Continue", "Maybe Later", "Get Started", "Done"];
            const btns = [...document.querySelectorAll("button")];
            for (const label of labels) {
                const b = btns.find((el) => (el.textContent || "").trim() === label);
                if (b) {
                    b.click();
                    return label;
                }
            }
            return null;
        });
        if (!clicked) {
            break;
        }
        await sleep(1500);
    }

    // 2. repaint-guard: the on-screen tab must have background throttling *enabled*
    // (Electron's disable_hidden cleared). While it stays disabled, Chromium never
    // records the widget's hidden state when the OS unmaps the window (minimize,
    // workspace switch, screen blank/lock), WasShown() then early-returns without
    // asking the renderer for a frame, and the tab sits at its #222222 background
    // color until a tab switch changes its bounds. A screenshot cannot catch this —
    // Playwright captures the renderer, not the browser compositor — so assert the
    // invariant instead, plus a hide/show round trip that must leave the view visible.
    //
    // This has to run BEFORE any tab switch: positionTabOffScreen enables throttling
    // on its way to the background, so a tab that has already been backgrounded once
    // reads true either way and the check would pass against the unfixed code too.
    {
        // Wave windows are BaseWindow + WebContentsView, not BrowserWindow, so
        // BrowserWindow.getAllWindows() comes back empty.
        const readState = () =>
            app.evaluate(({ BaseWindow }) => {
                const win = BaseWindow.getAllWindows().find((w) => w.contentView?.children?.length > 0);
                if (!win) {
                    return { err: "no window" };
                }
                const onScreen = win.contentView.children.find((v) => {
                    const b = v.getBounds();
                    return b.x === 0 && b.y === 0 && b.width > 0;
                });
                if (!onScreen) {
                    return { err: "no on-screen tab view" };
                }
                return {
                    throttling: onScreen.webContents?.getBackgroundThrottling(),
                    visible: onScreen.getVisible(),
                };
            });
        const before = await readState().catch((e) => ({ err: e.message }));
        if (before.err) {
            report("repaint-guard", false, before.err);
        } else if (Background) {
            report(
                "repaint-guard",
                before.throttling === true,
                `throttling=${before.throttling} visible=${before.visible} (hide/show round trip needs SMOKE_FOREGROUND=1)`
            );
        } else {
            await app.evaluate(({ BaseWindow }) => {
                const win = BaseWindow.getAllWindows().find((w) => w.contentView?.children?.length > 0);
                win?.hide();
                win?.show();
            });
            await sleep(1000);
            const after = await readState().catch((e) => ({ err: e.message }));
            const ok = before.throttling === true && !after.err && after.visible === true;
            report(
                "repaint-guard",
                ok,
                after.err ??
                    `throttling=${before.throttling}, after hide/show: visible=${after.visible} throttling=${after.throttling}`
            );
        }
    }

    // copy-unwrap: write a URL hard-wrapped at the terminal width straight into an
    // existing terminal's xterm buffer (real newlines, the way Claude Code / tmux re-wrap
    // output — no shell involved, so the check is independent of $COLUMNS and of how
    // narrow the block happens to be), select both rows programmatically via the dev-only
    // __termwraps registry (the WebGL renderer leaves no DOM text to drag over), and
    // verify the copy pipeline joins them back into one unbroken URL. Asserts on
    // TermWrap.getCopyText() — the same code path every copy route uses; the actual
    // navigator.clipboard write needs document focus, which a background-mode window
    // doesn't have, so the clipboard itself is only reported as detail.
    //
    // The shell is still starting when the onboarding modal closes; its init sequence
    // (clear + prompt) would wipe anything written before it lands, so wait for the
    // prompt to show up in the buffer before writing.
    {
        const promptReady = await (async () => {
            const end = Date.now() + 20_000;
            while (Date.now() < end) {
                const ready = await page.evaluate(() => {
                    const tw = window.__termwraps ? [...window.__termwraps.values()][0] : null;
                    const term = tw?.terminal;
                    if (!term || term.cols <= 0) {
                        return false;
                    }
                    const buf = term.buffer.active;
                    for (let i = 0; i < buf.length; i++) {
                        if ((buf.getLine(i)?.translateToString(true) ?? "") !== "") {
                            return true;
                        }
                    }
                    return false;
                });
                if (ready) {
                    await sleep(1500);
                    return true;
                }
                await sleep(500);
            }
            return false;
        })();
        const sel = await page.evaluate(async (promptReady) => {
            if (!promptReady) {
                return { err: "terminal never printed a prompt within 20s" };
            }
            const wraps = window.__termwraps;
            if (!wraps || !wraps.size) {
                return { err: "no __termwraps registry (dev hook missing?)" };
            }
            const tw = [...wraps.values()][0];
            const term = tw.terminal;
            const cols = term.cols;
            const total = cols + 10;
            const url = ("https://example.com/SMOKEJOIN" + "x".repeat(total)).slice(0, total);
            const line1 = url.slice(0, cols);
            const line2 = url.slice(cols);
            await new Promise((r) => term.write(`\r\n${line1}\r\n${line2}\r\n`, r));
            const buf = term.buffer.active;
            let row = -1;
            for (let i = buf.length - 1; i >= 0; i--) {
                if ((buf.getLine(i)?.translateToString(true) ?? "") === line1) {
                    row = i;
                    break;
                }
            }
            if (row < 0) {
                return { err: "written line not found in terminal buffer" };
            }
            term.selectLines(row, row + 1);
            return { cols, url, copyText: tw.getCopyText() };
        }, promptReady);
        if (sel.err) {
            report("copy-unwrap", false, sel.err);
        } else {
            const ok = sel.copyText === sel.url;
            await sleep(500);
            const clip = await app.evaluate(({ clipboard }) => clipboard.readText()).catch(() => "");
            const clipJoined = clip.includes(sel.url);
            report(
                "copy-unwrap",
                ok,
                ok
                    ? `URL rejoined across the hard wrap (cols=${sel.cols}, copy-on-select clipboard ${clipJoined ? "matches too" : "not asserted — window unfocused"})`
                    : `copy text still broken: ${JSON.stringify(sel.copyText.slice(0, 120))} (cols=${sel.cols})`
            );
        }
    }

    // 3. badge: flood a fresh terminal, expect a spinner badge on a tab in the tab bar
    await runTerminalCommand(page, "while true; do date; done");
    const badge = await (async () => {
        const end = Date.now() + 15_000;
        while (Date.now() < end) {
            const found = await page.evaluate(
                () => document.querySelector(".tab-bar [data-tab-id] i[class*='fa-spin']") != null
            );
            if (found) {
                return true;
            }
            await sleep(1000);
        }
        return false;
    })();
    report("badge", badge, badge ? "spinner badge visible in tab bar" : "no spinner badge within 15s");

    // webview guest setup (current tab, still visible): open a web block and point its
    // webview at a self-contained rAF counter page, so step 4 can verify the guest
    // process actually throttles when the tab goes to the background.
    let wvReady = false;
    {
        const w = await clickWidget(page, "web");
        if (w !== "OK") {
            report("webview-throttle", false, "web widget not found");
        } else {
            await sleep(6000);
            const nav = await page.evaluate(async (url) => {
                // The starter layout (or leftovers) can contain other webviews — load
                // the counter page into the newest one; measurements select by URL.
                const wvs = [...document.querySelectorAll("webview")];
                if (!wvs.length) {
                    return "NO_WEBVIEW";
                }
                const wv = wvs[wvs.length - 1];
                let lastErr = null;
                for (let i = 0; i < 3; i++) {
                    try {
                        await wv.loadURL(url);
                        return "OK";
                    } catch (e) {
                        lastErr = String(e?.message ?? e);
                        await new Promise((r) => setTimeout(r, 2000));
                    }
                }
                return `LOAD_FAILED: ${lastErr}`;
            }, rafUrl);
            wvReady = nav === "OK";
            if (!wvReady) {
                report("webview-throttle", false, `setup: ${nav}`);
            }
        }
    }
    function readGuestRaf() {
        return page.evaluate((url) => {
            const wv = [...document.querySelectorAll("webview")].find((w) => {
                try {
                    return w.getURL() === url;
                } catch {
                    return false;
                }
            });
            if (!wv) {
                throw new Error("raf webview not found");
            }
            return wv.executeJavaScript("window.__raf");
        }, rafUrl);
    }
    async function guestRafRate(sampleMs) {
        try {
            const a = await readGuestRaf();
            await sleep(sampleMs);
            const b = await readGuestRaf();
            return ((b - a) * 1000) / sampleMs;
        } catch {
            return -1;
        }
    }
    const visRafRate = wvReady ? await guestRafRate(2000) : 0;

    // 4./5. background the flooding tab (and the webview) behind a fresh empty tab;
    // on macOS compare renderer CPU, everywhere compare the guest's rAF rate
    await sleep(4000);
    const active = IsMac ? rendererCpuSum() : null;
    const added = await page.evaluate(() => {
        const btn = document.querySelector("button[title='Add Tab']");
        if (!btn) {
            return "NOT_FOUND";
        }
        btn.click();
        return "OK";
    });
    if (added !== "OK") {
        report("throttle", false, "Add Tab button not found");
        if (wvReady) {
            report("webview-throttle", false, "could not background the tab");
        }
    } else {
        await sleep(12_000);
        if (IsMac) {
            const hidden = rendererCpuSum();
            const ok = hidden.sum < Math.max(15, active.sum * 0.5);
            report(
                "throttle",
                ok,
                `renderer CPU sum ${active.sum.toFixed(1)}% active → ${hidden.sum.toFixed(1)}% hidden (${hidden.detail})`
            );
        } else {
            report("throttle", null, "macOS-only (top sampling)");
        }
        if (wvReady) {
            const guestVis = await page
                .evaluate((url) => {
                    const wv = [...document.querySelectorAll("webview")].find((w) => {
                        try {
                            return w.getURL() === url;
                        } catch {
                            return false;
                        }
                    });
                    return wv ? wv.executeJavaScript("document.visibilityState") : "not-found";
                }, rafUrl)
                .catch(() => "unknown");
            const hidRafRate = await guestRafRate(3000);
            const mainState = await app
                .evaluate(({ webContents }, url) => {
                    const guest = webContents.getAllWebContents().find((w) => {
                        try {
                            return w.getType() === "webview" && w.getURL() === url;
                        } catch {
                            return false;
                        }
                    });
                    if (!guest) {
                        return "guest=not-found";
                    }
                    const host = guest.hostWebContents;
                    return `guestThrottling=${guest.getBackgroundThrottling()} embedderThrottling=${host?.getBackgroundThrottling()}`;
                }, rafUrl)
                .catch((e) => `main=? (${e.message})`);
            const embedderState = await page
                .evaluate((url) => {
                    const wv = [...document.querySelectorAll("webview")].find((w) => {
                        try {
                            return w.getURL() === url;
                        } catch {
                            return false;
                        }
                    });
                    return `embedderVis=${document.visibilityState} wvDisplay=${wv ? getComputedStyle(wv).display : "?"}`;
                }, rafUrl)
                .catch(() => "embedder=?");
            const ok = visRafRate > 20 && hidRafRate >= 0 && hidRafRate < 5;
            report(
                "webview-throttle",
                ok,
                `guest rAF ${visRafRate.toFixed(0)}/s visible → ${hidRafRate.toFixed(0)}/s hidden (guest visibilityState=${guestVis}, ${embedderState}, ${mainState})`
            );
        }
    }

    // 5b. webview-resume: switch back to the first tab — the guest must come back to
    // life (parked rAF callbacks release, compositing restores after display:none)
    if (wvReady) {
        const back = await page.evaluate(() => {
            const chip = document.querySelector(".tab-bar [data-tab-id]");
            if (!chip) {
                return "NOT_FOUND";
            }
            chip.click();
            return "OK";
        });
        if (back !== "OK") {
            report("webview-resume", false, "first tab chip not found");
        } else {
            await sleep(3000);
            const resumeRate = await guestRafRate(2000);
            const shotDir = process.env.SCREENSHOT_DIR || "/tmp/shots";
            fs.mkdirSync(shotDir, { recursive: true });
            await page.screenshot({ path: path.join(shotDir, "webview-resume.png") }).catch(() => {});
            report(
                "webview-resume",
                resumeRate > 20,
                `guest rAF ${resumeRate.toFixed(0)}/s after re-show (screenshot: ${shotDir}/webview-resume.png)`
            );
        }
    }

    // 6. notify-skip: done while focused → main process logs SKIP. Needs the window
    // to be OS-focused, which means stealing focus from the user — only done with
    // SMOKE_FOREGROUND=1; in background mode the check reports SKIP.
    markLog();
    if (Background) {
        report("notify-skip", null, "background mode (needs focus steal; run with SMOKE_FOREGROUND=1)");
    } else {
        await app.evaluate(({ app: eApp }) => eApp.focus({ steal: true }));
        await sleep(500);
        await runTerminalCommand(page, "sleep 3 && echo SMOKE_SKIP");
        await app.evaluate(({ app: eApp }) => eApp.focus({ steal: true }));
        const skip = await waitForLog(/\[term-notify\] done blk=\w+ SKIP \(window focused\)/, 20_000);
        if (skip != null) {
            report("notify-skip", true, skip[0].trim());
        } else {
            // On a desktop where the user is actively working, the app cannot hold OS
            // focus, the done event QUEUEs instead of SKIPping, and the check cannot
            // assert anything — report SKIP (not FAIL) in that case.
            const focused = await app
                .evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().some((w) => w.isFocused()))
                .catch(() => false);
            const termNotifyLines =
                logTail()
                    .split("\n")
                    .filter((l) => l.includes("[term-notify]"))
                    .slice(-3)
                    .join(" | ") || "(none)";
            if (!focused) {
                report("notify-skip", null, `window focus contended (user active elsewhere); ${termNotifyLines}`);
            } else {
                report("notify-skip", false, `no SKIP line within 20s; term-notify lines: ${termNotifyLines}`);
            }
        }
    }

    // 7. notify-fire: hide the app (all windows unfocused), done → QUEUED + fire
    if (IsMac) {
        markLog();
        await runTerminalCommand(page, "sleep 3 && echo SMOKE_FIRE");
        await sleep(500);
        await app.evaluate(({ app: eApp }) => eApp.hide());
        const queued = await waitForLog(/\[term-notify\] done blk=\w+ QUEUED/, 20_000);
        const fired = queued ? await waitForLog(/\[term-notify\] fire done/, 15_000) : null;
        report(
            "notify-fire",
            queued != null && fired != null,
            fired ? fired[0].trim() : queued ? "queued but never fired" : "no QUEUED line within 20s"
        );
    } else {
        report("notify-fire", null, "macOS-only (app.hide)");
    }
} catch (e) {
    failedHard = true;
    console.error("SUITE ERROR:", e.message);
} finally {
    try {
        await app?.close();
    } catch {
        /* already gone */
    }
    rafServer.close();
    fs.rmSync(SandboxDir, { recursive: true, force: true });
}

console.log("\n=== smoke summary ===");
for (const r of results) {
    console.log(`${r.ok === null ? "SKIP" : r.ok ? "PASS" : "FAIL"}  ${r.name}`);
}
const failed = failedHard || results.some((r) => r.ok === false);
process.exit(failed ? 1 : 0);
