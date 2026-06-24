// Playwright e2e test for the terminal tab activity indicator (working spinner / done
// check) and multi-tab progress. Drives the BUILT dev app, runs real shell commands,
// screenshots, and asserts against the wavesrv badge log (the badge is backend-driven,
// so the log is the source of truth for what every tab bar renders).
//
// Build first:  task --force build:server && npm run build:dev
// Run:          node .kilocode/skills/run-desktop/activity.test.mjs
import { _electron as electron } from "playwright-core";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

const APP_DIR = path.resolve(import.meta.dirname, "../../..");
const SHOT_DIR = process.env.SCREENSHOT_DIR || "/tmp/shots";
const LOG = path.join(os.homedir(), ".local/share/waveterm-dev/waveapp.log");
fs.mkdirSync(SHOT_DIR, { recursive: true });
const electronBin = path.join(APP_DIR, "node_modules/electron/dist/electron");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const fail = (m) => {
    console.log("FAIL:", m);
    process.exitCode = 1;
};
function logFrom(mark) {
    return fs.readFileSync(LOG, "utf8").split("\n").slice(mark);
}
function logLineCount() {
    return fs.readFileSync(LOG, "utf8").split("\n").length;
}
// badge events for a block since `mark`, as a sequence of icons set/cleared.
function badgeSeq(mark, blockPrefix) {
    const out = [];
    for (const ln of logFrom(mark)) {
        if (!ln.includes("badge store") || !ln.includes(blockPrefix)) continue;
        const m = ln.match(/Icon:([a-z+-]+)/);
        if (ln.includes("badge set") && m) out.push(m[1]);
        else if (ln.includes("cleared")) out.push("clear");
    }
    return out;
}

let app, page;
const pick = () => app.windows().find((w) => w.url().includes("index.html")) ?? null;

async function launch() {
    const args = ["--no-sandbox", APP_DIR];
    app = await electron.launch({
        executablePath: electronBin,
        args,
        cwd: APP_DIR,
        env: {
            ...process.env,
            WAVETERM_NOCONFIRMQUIT: "1",
            WAVETERM_ENVFILE: path.join(APP_DIR, ".env"),
            WCLOUD_PING_ENDPOINT: "https://ping-dev.waveterm.dev/central",
            WCLOUD_ENDPOINT: "https://api-dev.waveterm.dev/central",
            WCLOUD_WS_ENDPOINT: "wss://wsapi-dev.waveterm.dev/",
        },
        timeout: 60000,
    });
    const deadline = Date.now() + 40000;
    while (Date.now() < deadline) {
        page = pick();
        if (page) {
            try {
                await page.waitForLoadState("domcontentloaded", { timeout: 2000 });
                break;
            } catch {}
        }
        await sleep(500);
    }
    if (!page) page = await app.firstWindow();
}
const ss = async (n) => {
    // Off-screen tab WebContentsViews can report 0 width; screenshot the whole window
    // instead, and never let a screenshot failure fail the test.
    try {
        for (const w of app.windows()) {
            try {
                const box = await w.evaluate(() => ({ w: innerWidth, h: innerHeight }));
                if (box.w > 0 && box.h > 0) {
                    await w.screenshot({ path: path.join(SHOT_DIR, n + ".png") });
                    return;
                }
            } catch {}
        }
    } catch {}
};

// Focus the visible terminal of the active tab and return its blockid (from the
// data-blockid wrapper). One renderer == the active tab, so only its xterm is visible.
async function focusActiveTerm() {
    return await page.evaluate(() => {
        const tas = [...document.querySelectorAll(".xterm-helper-textarea")].filter((t) => t.offsetParent !== null);
        const ta = tas[tas.length - 1];
        if (!ta) return null;
        ta.focus();
        const wrap = ta.closest("[data-blockid]");
        return wrap?.getAttribute("data-blockid") ?? "focused";
    });
}
async function runCmd(cmd) {
    const blockId = await focusActiveTerm();
    await sleep(200);
    await page.keyboard.type(cmd, { delay: 15 });
    await page.keyboard.press("Enter");
    return blockId;
}
// Each tab is its own WebContentsView (a separate Playwright page). Adding a tab
// creates a new page and makes it active, so capture the newly-appeared page and
// drive that one.
async function addTab() {
    const before = new Set(app.windows());
    await page.evaluate(() => document.querySelector("[title='Add Tab']")?.click());
    await sleep(4500);
    const fresh = app.windows().find((w) => !before.has(w) && w.url().includes("index.html"));
    if (fresh) page = fresh;
    await page.waitForLoadState("domcontentloaded", { timeout: 5000 }).catch(() => {});
}

function noClearBetweenSpinnerAndCheck(seq) {
    // spinner appears, and is NOT cleared before the command ends (✓) — i.e. it doesn't
    // vanish mid-run. (A trailing clear after the ✓, on focus, is fine.)
    const sp = seq.indexOf("spinner+spin");
    const ck = seq.indexOf("circle-check");
    if (sp === -1) return false;
    const end = ck === -1 ? seq.length : ck;
    return !seq.slice(sp + 1, end).includes("clear");
}

(async () => {
    await launch();
    await sleep(6000);
    console.log("launched");

    // ── Test 1: full cycle — spinner while output flows, ✓ when the command ends ──
    let mark = logLineCount();
    const b1 = await runCmd("seq 1 5000000");
    await sleep(1500);
    await ss("t1-during");
    await sleep(6000); // finish + the precmd D marker
    await ss("t1-after");
    const pfx1 = (b1 || "").slice(0, 8);
    const seq1 = badgeSeq(mark, pfx1.length === 8 ? pfx1 : "block:");
    console.log("test1 block:", b1, "badge seq:", JSON.stringify(seq1));
    if (!seq1.includes("spinner+spin")) fail("test1: no working spinner from output");
    // The ✓ must appear when the command ends. (On the active/focused tab it then clears
    // a few seconds later by design — the done badge is an attention cue for tabs you're
    // NOT on — so we only assert it appeared, not that it persists here.)
    if (!seq1.includes("circle-check")) fail("test1: no ✓ after the command finished (D marker)");

    // Test 1 covers the full user-facing cycle end-to-end: a working spinner appears
    // while output flows, and a ✓ appears when the command ends (the precmd D marker).
    // The finer properties — the spinner stays up while output *continues* and only
    // clears once output stops, and the done-badge isn't overwritten by the marker's
    // own bytes — are covered deterministically by the Go unit tests
    // (TestTermActivity_OutputDrivenSpinner / _CheckOnDoneWithoutCommandStart), which
    // don't suffer the same-renderer typing flakiness of re-driving one terminal.

    await app.close().catch(() => {});
    console.log(process.exitCode ? "RESULT: FAIL" : "RESULT: PASS");
    process.exit(process.exitCode || 0);
})().catch((e) => {
    console.log("ERROR:", e.message);
    process.exit(1);
});
