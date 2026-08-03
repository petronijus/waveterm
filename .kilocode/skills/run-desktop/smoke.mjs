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
//   2. badge         — a running command gets its backend-driven spinner badge in the tab bar
//   3. throttle      — a tab flooding terminal output stops burning CPU once its tab
//                      goes to the background (macOS only; needs `top`)
//   4. notify-skip   — command-done is suppressed while the window is focused
//   5. notify-fire   — command-done queues and fires once the app is hidden (app.hide,
//                      no OS-focus races; macOS only)
//
// Notes:
//   - Uses the dev identity (waveterm-dev data/config dirs), same as the driver.
//   - Temporarily sets notify:commanddone + a low threshold in the dev settings.json
//     and restores the file afterwards.
//   - Leaves behind one extra tab with two terminal blocks in the dev workspace —
//     close them by hand if you care.
//   - Exit code 0 = all pass / skipped, 1 = any failure.

import { execSync } from "node:child_process";
import fs from "node:fs";
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
const DevLogPath = IsMac
    ? path.join(os.homedir(), "Library/Application Support/waveterm-dev/waveapp.log")
    : path.join(os.homedir(), ".local/share/waveterm-dev/waveapp.log");
const DevSettingsPath = path.join(os.homedir(), ".config/waveterm-dev/settings.json");

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

// --- dev settings (set low notify threshold, restore afterwards)
let settingsBackup = null;
function prepareSettings() {
    settingsBackup = fs.existsSync(DevSettingsPath) ? fs.readFileSync(DevSettingsPath, "utf8") : null;
    const cur = settingsBackup ? JSON.parse(settingsBackup) : {};
    cur["notify:commanddone"] = true;
    cur["notify:commanddonethresholdms"] = 1500;
    fs.mkdirSync(path.dirname(DevSettingsPath), { recursive: true });
    fs.writeFileSync(DevSettingsPath, JSON.stringify(cur, null, 2));
}
function restoreSettings() {
    if (settingsBackup == null) {
        fs.rmSync(DevSettingsPath, { force: true });
    } else {
        fs.writeFileSync(DevSettingsPath, settingsBackup);
    }
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
    const w = await clickWidget(page, "terminal");
    if (w !== "OK") {
        throw new Error("widget terminal not found");
    }
    await sleep(3000);
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

let app = null;
let failedHard = false;
try {
    app = await electron.launch({
        executablePath: electronBin,
        args: IsMac ? [APP_DIR] : ["--no-sandbox", APP_DIR],
        cwd: APP_DIR,
        env: {
            ...process.env,
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
    await sleep(6000);
    report("launch", true, `${app.windows().length} window(s)`);

    // 2. badge: flood a fresh terminal, expect a spinner badge on a tab in the tab bar
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

    // 3. throttle: CPU with the flooding tab active vs hidden behind a fresh empty tab
    if (IsMac) {
        await sleep(4000);
        const active = rendererCpuSum();
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
        } else {
            await sleep(12_000);
            const hidden = rendererCpuSum();
            const ok = hidden.sum < Math.max(15, active.sum * 0.5);
            report(
                "throttle",
                ok,
                `renderer CPU sum ${active.sum.toFixed(1)}% active → ${hidden.sum.toFixed(1)}% hidden (${hidden.detail})`
            );
        }
    } else {
        report("throttle", null, "macOS-only (top sampling)");
    }

    // 4. notify-skip: done while focused → main process logs SKIP
    markLog();
    await runTerminalCommand(page, "sleep 3 && echo SMOKE_SKIP");
    const skip = await waitForLog(/\[term-notify\] done blk=\w+ SKIP \(window focused\)/, 20_000);
    report("notify-skip", skip != null, skip ? skip[0].trim() : "no SKIP line within 20s");

    // 5. notify-fire: hide the app (all windows unfocused), done → QUEUED + fire
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
    restoreSettings();
}

console.log("\n=== smoke summary ===");
for (const r of results) {
    console.log(`${r.ok === null ? "SKIP" : r.ok ? "PASS" : "FAIL"}  ${r.name}`);
}
const failed = failedHard || results.some((r) => r.ok === false);
process.exit(failed ? 1 : 0);
