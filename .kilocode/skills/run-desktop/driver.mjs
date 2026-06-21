// REPL driver for the Wave Terminal Electron app.
// Designed for agents: wrap in tmux, send-keys commands, capture-pane output.
//
// Launches the *built* app (dist/main + dist/frontend + dist/bin/wavesrv.<arch>)
// via Playwright's _electron, so no Vite dev server is required. Build first:
//   task build:backend:quickdev && npm run build:dev
//
// macOS/Linux. On headless Linux wrap with `xvfb-run -a`. On macOS run directly.
import { _electron as electron } from "playwright-core";
import * as readline from "node:readline";
import * as fs from "node:fs";
import * as path from "node:path";

const APP_DIR = path.resolve(import.meta.dirname, "../../..");
const SHOT_DIR = process.env.SCREENSHOT_DIR || "/tmp/shots";
fs.mkdirSync(SHOT_DIR, { recursive: true });

const electronBin =
    process.platform === "darwin"
        ? path.join(APP_DIR, "node_modules/electron/dist/Electron.app/Contents/MacOS/Electron")
        : path.join(APP_DIR, "node_modules/electron/dist/electron");

let app = null;
let page = null;

function pickPage() {
    return app.windows().find((w) => w.url().includes("index.html")) ?? app.windows().find((w) => !w.url().startsWith("devtools://")) ?? null;
}

const COMMANDS = {
    async launch() {
        if (app) return console.log("already launched");
        const args = [APP_DIR];
        if (process.platform !== "darwin") args.unshift("--no-sandbox");
        app = await electron.launch({
            executablePath: electronBin,
            args,
            cwd: APP_DIR,
            // wavesrv aborts at startup if the WCLOUD_* endpoints are missing/invalid.
            // These mirror Taskfile.yml's electron:quickdev env (dev telemetry endpoints).
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
        // Poll for the real renderer window instead of a blind sleep.
        const deadline = Date.now() + 40_000;
        while (Date.now() < deadline) {
            page = pickPage();
            if (page) {
                try {
                    await page.waitForLoadState("domcontentloaded", { timeout: 2_000 });
                    break;
                } catch {
                    /* keep waiting */
                }
            }
            await new Promise((r) => setTimeout(r, 500));
        }
        if (!page) page = await app.firstWindow();
        console.log("launched.", app.windows().length, "windows:");
        for (const w of app.windows()) console.log("  ", w.url());
    },

    async ss(name) {
        if (!page) return console.log("ERROR: launch first");
        const f = path.join(SHOT_DIR, (name || `ss-${Date.now()}`) + ".png");
        await page.screenshot({ path: f });
        console.log("screenshot:", f);
    },

    async click(sel) {
        if (!page) return console.log("ERROR: launch first");
        const r = await page.evaluate((s) => {
            const el = document.querySelector(s);
            if (!el) return "NOT_FOUND";
            el.click();
            return "OK";
        }, sel);
        console.log("click", sel, "→", r);
    },

    // Click a widget in the widget bar by its exact label (e.g. "git").
    // Matches the leaf label element (no child elements) whose text === label,
    // not an ancestor container whose textContent merely includes it.
    async widget(label) {
        if (!page) return console.log("ERROR: launch first");
        const r = await page.evaluate((t) => {
            const leaf = [...document.querySelectorAll("*")].find(
                (e) => e.childElementCount === 0 && (e.textContent || "").trim() === t
            );
            if (!leaf) return "NOT_FOUND";
            // Click the nearest clickable ancestor (the widget button has the handler).
            (leaf.closest("[class*='cursor-pointer']") || leaf).click();
            return "OK";
        }, label);
        console.log("widget", JSON.stringify(label), "→", r);
    },

    async "click-text"(text) {
        if (!page) return console.log("ERROR: launch first");
        const r = await page.evaluate((t) => {
            const els = [...document.querySelectorAll("button, a, [role='button'], div")];
            const el = els.find((e) => e.textContent?.trim() === t) ?? els.find((e) => e.textContent?.includes(t));
            if (!el) return "NOT_FOUND";
            el.click();
            return "OK: " + el.tagName;
        }, text);
        console.log("click-text", JSON.stringify(text), "→", r);
    },

    async type(text) {
        if (page) await page.keyboard.type(text, { delay: 30 });
    },
    async press(key) {
        if (page) await page.keyboard.press(key);
    },

    async wait(sel) {
        if (!page) return console.log("ERROR: launch first");
        try {
            await page.waitForSelector(sel, { timeout: 10_000 });
            console.log("found:", sel);
        } catch {
            console.log("TIMEOUT:", sel);
        }
    },

    async eval(expr) {
        if (!page) return console.log("ERROR: launch first");
        try {
            console.log(JSON.stringify(await page.evaluate(expr)));
        } catch (e) {
            console.log("ERROR:", e.message);
        }
    },

    async text(sel) {
        if (!page) return console.log("ERROR: launch first");
        console.log(await page.evaluate((s) => (s ? document.querySelector(s) : document.body)?.innerText ?? "(null)", sel || null));
    },

    async windows() {
        if (!app) return console.log("ERROR: launch first");
        for (const w of app.windows()) console.log("  ", w.url());
        const wcs = await app.evaluate(({ webContents }) =>
            webContents.getAllWebContents().map((w) => ({ id: w.id, type: w.getType(), url: w.getURL() }))
        );
        console.log("webContents:");
        for (const w of wcs) console.log(` [${w.id}] ${w.type}: ${w.url}`);
    },

    async sleep(ms) {
        await new Promise((r) => setTimeout(r, parseInt(ms || "1000", 10)));
    },

    async quit() {
        if (app) await app.close().catch(() => {});
        app = null;
        page = null;
    },
    help() {
        console.log("commands:", Object.keys(COMMANDS).join(", "));
    },
};

async function runLine(line) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) return false;
    const idx = trimmed.indexOf(" ");
    const cmd = idx === -1 ? trimmed : trimmed.slice(0, idx);
    const arg = idx === -1 ? "" : trimmed.slice(idx + 1);
    const fn = COMMANDS[cmd];
    if (!fn) {
        console.log("unknown:", cmd, "— try: help");
        return false;
    }
    try {
        await fn(arg);
    } catch (e) {
        console.log("ERROR:", e.message);
    }
    return cmd === "quit";
}

console.log('wave driver — "help" for commands, "launch" to start');

if (process.stdin.isTTY) {
    // Interactive REPL (use under tmux). Process lines strictly sequentially.
    const stdin = fs.createReadStream(null, { fd: fs.openSync("/dev/stdin", "r") });
    const rl = readline.createInterface({ input: stdin, output: process.stdout, prompt: "driver> " });
    let chain = Promise.resolve();
    rl.on("line", (line) => {
        chain = chain.then(async () => {
            const quit = await runLine(line);
            if (quit) {
                rl.close();
                process.exit(0);
            }
            rl.prompt();
        });
    });
    rl.on("close", async () => {
        await chain.catch(() => {});
        await COMMANDS.quit();
        process.exit(0);
    });
    rl.prompt();
} else {
    // Batch mode: read the whole script up front (BEFORE launching, so Electron
    // can't grab the stdin fd), then run each line sequentially.
    const script = fs.readFileSync(0, "utf8");
    for (const line of script.split("\n")) {
        const quit = await runLine(line);
        if (quit) break;
    }
    await COMMANDS.quit();
    process.exit(0);
}
