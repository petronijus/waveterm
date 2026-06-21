// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { clearBadgeById, setBadge } from "@/app/store/badge";
import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { v7 as uuidv7 } from "uuid";
import {
    atoms,
    getApi,
    getBlockMetaKeyAtom,
    getBlockTermDurableAtom,
    getOverrideConfigAtom,
    globalStore,
    recordTEvent,
    WOS,
} from "@/store/global";
import { base64ToString, fireAndForget, isSshConnName, isWslConnName } from "@/util/util";
import debug from "debug";
import { maybeNotifyAgentWaiting, maybeNotifyCommandDone } from "./notify-commanddone";
import type { TermWrap } from "./termwrap";

const dlog = debug("wave:termwrap");

const Osc52MaxDecodedSize = 75 * 1024; // max clipboard size for OSC 52 (matches common terminal implementations)
const Osc52MaxRawLength = 128 * 1024; // includes selector + base64 + whitespace (rough check)

// OSC 16162 - Shell Integration Commands
// See aiprompts/wave-osc-16162.md for full documentation
export type ShellIntegrationStatus = "ready" | "running-command";

const ClaudeCodeRegex = /^claude\b/;

// Interactive AI coding agents whose turn-done "your turn" signal (terminal bell or
// an OSC 9 notification) we surface as a distinct tab "waiting for you" state. Each
// maps a command prefix to the kind shown in the notification title.
const AgentCommandRegexes: { kind: string; re: RegExp }[] = [
    { kind: "claude", re: ClaudeCodeRegex },
    { kind: "gemini", re: /^gemini\b/ },
    { kind: "codex", re: /^codex\b/ },
];

type Osc16162Command =
    | { command: "A"; data: Record<string, never> }
    | { command: "C"; data: { cmd64?: string } }
    | {
          command: "M";
          data: {
              shell?: string;
              shellversion?: string;
              uname?: string;
              integration?: boolean;
              omz?: boolean;
              comp?: string;
          };
      }
    | { command: "D"; data: { exitcode?: number } }
    | { command: "I"; data: { inputempty?: boolean } }
    | { command: "R"; data: Record<string, never> };

function normalizeCmd(decodedCmd: string): string {
    let normalizedCmd = decodedCmd.trim();
    normalizedCmd = normalizedCmd.replace(/^env\s+/, "");
    normalizedCmd = normalizedCmd.replace(/^(?:\w+=(?:"[^"]*"|'[^']*'|\S+)\s+)*/, "");
    return normalizedCmd;
}

function checkCommandForTelemetry(decodedCmd: string) {
    if (!decodedCmd) {
        return;
    }

    const normalizedCmd = normalizeCmd(decodedCmd);

    if (normalizedCmd.startsWith("ssh ")) {
        recordTEvent("conn:connect", { "conn:conntype": "ssh-manual" });
        return;
    }

    const editorsRegex = /^(vim|vi|nano|nvim)\b/;
    if (editorsRegex.test(normalizedCmd)) {
        recordTEvent("action:term", { "action:type": "cli-edit" });
        return;
    }

    const tailFollowRegex = /(^|\|\s*)tail\s+-[fF]\b/;
    if (tailFollowRegex.test(normalizedCmd)) {
        recordTEvent("action:term", { "action:type": "cli-tailf" });
        return;
    }

    if (ClaudeCodeRegex.test(normalizedCmd)) {
        recordTEvent("action:term", { "action:type": "claude" });
        return;
    }

    const opencodeRegex = /^opencode\b/;
    if (opencodeRegex.test(normalizedCmd)) {
        recordTEvent("action:term", { "action:type": "opencode" });
        return;
    }
}

export function isClaudeCodeCommand(decodedCmd: string): boolean {
    if (!decodedCmd) {
        return false;
    }
    return ClaudeCodeRegex.test(normalizeCmd(decodedCmd));
}

// Returns which AI agent a command launches ("claude" | "gemini" | "codex"), or null.
export function agentKindForCommand(decodedCmd: string): string {
    if (!decodedCmd) {
        return null;
    }
    const normalized = normalizeCmd(decodedCmd);
    for (const { kind, re } of AgentCommandRegexes) {
        if (re.test(normalized)) {
            return kind;
        }
    }
    return null;
}

// Tab "working / done" activity badge, driven by shell-integration command
// lifecycle (OSC 16162 C → D/A) PLUS live PTY output. Generic — works for any
// command, not just one tool. The spinner shows only while a command is running
// AND actively producing output, so a long-running foreground app that's idle
// (e.g. Claude waiting for input) stops spinning until it works again.
const CmdActivityDelayMs = 1200; // ignore the first burst so quick commands don't flash
const CmdActivityIdleMs = 2500; // output quiet this long ⇒ "done thinking" / idle
const CmdActivitySustainMs = 700; // output must flow this long continuously before we call it "working"
const CmdActivityGapMs = 1000; // a quiet gap longer than this ends the continuous stretch
const CmdActivityWorkBytes = 512; // after a "waiting" bell, a stretch must carry at least this many bytes to count as real work again (so the idle TUI's cursor/box repaints don't re-trigger the spinner; real claude output streams far more)
const CmdActivityPriority = 5;

function ensureActivityBadgeId(termWrap: TermWrap): string {
    if (termWrap.cmdActivityBadgeId == null) {
        termWrap.cmdActivityBadgeId = uuidv7();
    }
    return termWrap.cmdActivityBadgeId;
}

// Is the user currently looking at this terminal's tab? (active tab + window focused)
function isTermTabActive(termWrap: TermWrap): boolean {
    return globalStore.get(atoms.staticTabId) === termWrap.tabId && !!globalStore.get(atoms.documentHasFocus);
}

// Transition our activity badge: clear our previous state (shares one badgeid) then set the new one.
function setActivityBadge(termWrap: TermWrap, blockId: string, icon: string, color: string, pidlinked: boolean): void {
    const id = ensureActivityBadgeId(termWrap);
    clearBadgeById(blockId, id);
    setBadge(blockId, { badgeid: id, icon, color, priority: CmdActivityPriority, pidlinked });
}

function hideActivityBadge(termWrap: TermWrap, blockId: string): void {
    if (termWrap.cmdActivityBadgeId != null) {
        clearBadgeById(blockId, termWrap.cmdActivityBadgeId);
    }
}

// "done — come look" badge: green check on success, red x on failure. pidlinked=false
// so it clears as soon as you focus the tab.
function showDoneBadge(termWrap: TermWrap, blockId: string, exitcode: number | null): void {
    const ok = exitcode == null || exitcode === 0;
    setActivityBadge(
        termWrap,
        blockId,
        ok ? "circle-check" : "circle-xmark",
        ok ? "var(--success-color)" : "var(--error-color)",
        false
    );
}

function startCommandActivity(termWrap: TermWrap, blockId: string): void {
    hideActivityBadge(termWrap, blockId); // drop any leftover done/pending from the previous command
    if (termWrap.cmdActivityIdleTimeout != null) {
        clearTimeout(termWrap.cmdActivityIdleTimeout);
        termWrap.cmdActivityIdleTimeout = null;
    }
    termWrap.cmdActivityRunning = true;
    termWrap.cmdActivityStartTs = Date.now();
    termWrap.cmdActivityVisible = false;
    termWrap.cmdActivityEverShown = false;
    termWrap.cmdActivityActiveSince = 0;
    termWrap.cmdActivityLastOutputTs = 0;
    termWrap.cmdActivityWaiting = false;
    termWrap.cmdActivityStretchBytes = 0;
}

// Called on each live PTY output chunk. We only call it "working" once output has
// flowed CONTINUOUSLY for CmdActivitySustainMs — a one-off redraw burst (terminal
// resize on tab-switch, a TUI repaint) is a single cluster and never qualifies, so
// it no longer produces a phantom spinner/checkmark. When sustained output then
// goes quiet, the command is "done thinking": away ⇒ pending "done" badge, watching ⇒ just stop.
function markCommandActivity(termWrap: TermWrap, blockId: string, outputLen: number = 0): void {
    if (!termWrap.cmdActivityRunning) {
        return;
    }
    // Only badge tabs you're NOT looking at. On the active+focused tab you can see
    // the terminal directly, so no spinner is needed there — and crucially, terminal
    // resizes (the repaint bursts behind the tab-switch "blink") only happen to the
    // visible tab, so never spinning the active tab removes the blink at the source.
    if (isTermTabActive(termWrap)) {
        if (termWrap.cmdActivityVisible) {
            termWrap.cmdActivityVisible = false;
            hideActivityBadge(termWrap, blockId);
        }
        if (termWrap.cmdActivityIdleTimeout != null) {
            clearTimeout(termWrap.cmdActivityIdleTimeout);
            termWrap.cmdActivityIdleTimeout = null;
        }
        termWrap.cmdActivityActiveSince = 0;
        return;
    }
    const now = Date.now();
    if (now < termWrap.cmdActivitySuppressUntil) {
        return; // output right after a resize is a repaint, not command activity
    }
    if (now - termWrap.cmdActivityStartTs < CmdActivityDelayMs) {
        return; // quick command / first burst — don't flash
    }
    // start a new continuous stretch if this is the first chunk or there was a quiet gap
    if (termWrap.cmdActivityActiveSince === 0 || now - termWrap.cmdActivityLastOutputTs > CmdActivityGapMs) {
        termWrap.cmdActivityActiveSince = now;
        termWrap.cmdActivityStretchBytes = 0;
    }
    termWrap.cmdActivityLastOutputTs = now;
    termWrap.cmdActivityStretchBytes += outputLen;
    // While we're in the bell-driven "waiting for you" state, a claude TUI keeps
    // repainting its idle prompt — small bursts that must NOT look like work. Only a
    // stretch carrying real volume (CmdActivityWorkBytes) counts as work resuming and
    // flips us back to the spinner. Outside the waiting state this gate is a no-op.
    const workVolumeOk = !termWrap.cmdActivityWaiting || termWrap.cmdActivityStretchBytes >= CmdActivityWorkBytes;
    if (!termWrap.cmdActivityVisible && workVolumeOk && now - termWrap.cmdActivityActiveSince >= CmdActivitySustainMs) {
        termWrap.cmdActivityWaiting = false;
        termWrap.cmdActivityVisible = true;
        termWrap.cmdActivityEverShown = true;
        setActivityBadge(termWrap, blockId, "spinner+spin", "var(--accent-color)", true);
    }
    // Only run the idle timer while the spinner is actually showing. Once we've gone
    // idle/"done", stray repaint bursts must NOT re-arm it (that re-decided show/hide
    // on every tab-switch and made the checkmark flicker). The spinner only comes back
    // when sustained output resumes (the block above), which is real new work.
    if (termWrap.cmdActivityVisible) {
        if (termWrap.cmdActivityIdleTimeout != null) {
            clearTimeout(termWrap.cmdActivityIdleTimeout);
        }
        termWrap.cmdActivityIdleTimeout = setTimeout(() => {
            termWrap.cmdActivityIdleTimeout = null;
            termWrap.cmdActivityActiveSince = 0; // stretch ended
            termWrap.cmdActivityVisible = false;
            if (isTermTabActive(termWrap)) {
                hideActivityBadge(termWrap, blockId); // you're watching — just stop spinning
            } else {
                showDoneBadge(termWrap, blockId, null); // away — "done, come look"
            }
        }, CmdActivityIdleMs);
    }
}

function finishCommandActivity(termWrap: TermWrap, blockId: string, exitcode: number | null): void {
    if (!termWrap.cmdActivityRunning) {
        return; // already finalized (e.g. D fired, then A)
    }
    termWrap.cmdActivityRunning = false;
    if (termWrap.cmdActivityIdleTimeout != null) {
        clearTimeout(termWrap.cmdActivityIdleTimeout);
        termWrap.cmdActivityIdleTimeout = null;
    }
    if (termWrap.cmdActivityWaiting) {
        termWrap.cmdActivityWaiting = false;
        hideActivityBadge(termWrap, blockId); // the "waiting for you" state ended with the command
    }
    // Notification is duration-gated, not output-gated, so a long *silent* command
    // (e.g. `sleep 60`) still notifies even though it never showed a spinner.
    maybeNotifyCommandDone(termWrap);
    if (!termWrap.cmdActivityEverShown) {
        return; // never worked visibly — quick/silent command, no badge
    }
    termWrap.cmdActivityVisible = false;
    termWrap.cmdActivityEverShown = false;
    if (isTermTabActive(termWrap)) {
        hideActivityBadge(termWrap, blockId); // you watched it finish
    } else {
        showDoneBadge(termWrap, blockId, exitcode); // away — leave the result
    }
}

function cancelCommandActivity(termWrap: TermWrap, blockId: string): void {
    if (termWrap.cmdActivityIdleTimeout != null) {
        clearTimeout(termWrap.cmdActivityIdleTimeout);
        termWrap.cmdActivityIdleTimeout = null;
    }
    hideActivityBadge(termWrap, blockId);
    termWrap.cmdActivityRunning = false;
    termWrap.cmdActivityVisible = false;
    termWrap.cmdActivityEverShown = false;
    termWrap.cmdActivityWaiting = false;
    termWrap.cmdActivityStretchBytes = 0;
}

// Called on a terminal BEL or an OSC 9 notification. Interactive AI agents (Claude
// Code, Gemini CLI, Codex) signal "your turn" this way when a turn finishes — but
// their TUIs keep repainting, so the output-idle heuristic never sees them stop and
// the tab would keep "working". While an agent command is active, treat that signal
// as a distinct "waiting for you" state: drop the spinner, show a calm badge on
// background tabs, and optionally fire an OS notification. The spinner only returns
// when real output volume resumes (see markCommandActivity).
function markCommandWaiting(termWrap: TermWrap, blockId: string): void {
    if (!termWrap.cmdActivityRunning) {
        return; // no command active — a bare-shell bell, not an agent waiting
    }
    if (globalStore.get(termWrap.agentKindAtom) == null) {
        return; // scoped to AI agents' "your turn" signal, not arbitrary program bells
    }
    if (termWrap.cmdActivityIdleTimeout != null) {
        clearTimeout(termWrap.cmdActivityIdleTimeout);
        termWrap.cmdActivityIdleTimeout = null;
    }
    const wasWaiting = termWrap.cmdActivityWaiting;
    termWrap.cmdActivityWaiting = true;
    termWrap.cmdActivityVisible = false;
    termWrap.cmdActivityActiveSince = 0;
    termWrap.cmdActivityStretchBytes = 0;
    if (isTermTabActive(termWrap)) {
        hideActivityBadge(termWrap, blockId); // you're looking at it — no badge needed
    } else {
        setActivityBadge(termWrap, blockId, "comment-dots", "#fbbf24", false);
    }
    if (!wasWaiting) {
        maybeNotifyAgentWaiting(termWrap); // only on entering the state, not on every repeated bell
    }
}

// OSC 9 is the desktop-notification escape both Gemini CLI and Codex prefer (with the
// terminal bell as a fallback), so handling it makes "waiting for you" fire even when
// a tool never emits BEL. OSC 9;4 is the unrelated ConEmu/Windows-Terminal progress
// protocol, so it's ignored. We "own" OSC 9, so always return true.
export function handleOsc9Command(data: string, blockId: string, loaded: boolean, termWrap: TermWrap): boolean {
    if (!loaded || !data) {
        return true;
    }
    if (data === "4" || data.startsWith("4;")) {
        return true; // progress protocol, not a notification
    }
    markCommandWaiting(termWrap, blockId);
    return true;
}

export { markCommandActivity, markCommandWaiting };

function handleShellIntegrationCommandStart(
    termWrap: TermWrap,
    blockId: string,
    cmd: { command: "C"; data: { cmd64?: string } },
    rtInfo: ObjRTInfo // this is passed by reference and modified inside of this function
): void {
    rtInfo["shell:state"] = "running-command";
    globalStore.set(termWrap.shellIntegrationStatusAtom, "running-command");
    startCommandActivity(termWrap, blockId);
    const connName = globalStore.get(getBlockMetaKeyAtom(blockId, "connection")) ?? "";
    const isRemote = isSshConnName(connName);
    const isWsl = isWslConnName(connName);
    const isDurable = globalStore.get(getBlockTermDurableAtom(blockId)) ?? false;
    getApi().incrementTermCommands({ isRemote, isWsl, isDurable });
    if (cmd.data.cmd64) {
        const decodedLen = Math.ceil(cmd.data.cmd64.length * 0.75);
        if (decodedLen > 8192) {
            rtInfo["shell:lastcmd"] = `# command too large (${decodedLen} bytes)`;
            globalStore.set(termWrap.lastCommandAtom, rtInfo["shell:lastcmd"]);
        } else {
            try {
                const decodedCmd = base64ToString(cmd.data.cmd64);
                rtInfo["shell:lastcmd"] = decodedCmd;
                globalStore.set(termWrap.lastCommandAtom, decodedCmd);
                const isCC = isClaudeCodeCommand(decodedCmd);
                globalStore.set(termWrap.claudeCodeActiveAtom, isCC);
                globalStore.set(termWrap.agentKindAtom, agentKindForCommand(decodedCmd));
                checkCommandForTelemetry(decodedCmd);
            } catch (e) {
                console.error("Error decoding cmd64:", e);
                rtInfo["shell:lastcmd"] = null;
                globalStore.set(termWrap.lastCommandAtom, null);
                globalStore.set(termWrap.claudeCodeActiveAtom, false);
                globalStore.set(termWrap.agentKindAtom, null);
            }
        }
    } else {
        rtInfo["shell:lastcmd"] = null;
        globalStore.set(termWrap.lastCommandAtom, null);
        globalStore.set(termWrap.claudeCodeActiveAtom, false);
        globalStore.set(termWrap.agentKindAtom, null);
    }
    rtInfo["shell:lastcmdexitcode"] = null;
}

// for xterm OSC handlers, we return true always because we "own" the OSC number.
// even if data is invalid we don't want to propagate to other handlers.
export function handleOsc52Command(data: string, blockId: string, loaded: boolean, termWrap: TermWrap): boolean {
    if (!loaded) {
        return true;
    }
    const osc52Mode = globalStore.get(getOverrideConfigAtom(blockId, "term:osc52")) ?? "always";
    if (osc52Mode === "focus") {
        const isBlockFocused = termWrap.nodeModel ? globalStore.get(termWrap.nodeModel.isFocused) : false;
        if (!document.hasFocus() || !isBlockFocused) {
            console.log("OSC 52: rejected, window or block not focused");
            return true;
        }
    }
    if (!data || data.length === 0) {
        console.log("OSC 52: empty data received");
        return true;
    }
    if (data.length > Osc52MaxRawLength) {
        console.log("OSC 52: raw data too large", data.length);
        return true;
    }

    const semicolonIndex = data.indexOf(";");
    if (semicolonIndex === -1) {
        console.log("OSC 52: invalid format (no semicolon)", data.substring(0, 50));
        return true;
    }

    const clipboardSelection = data.substring(0, semicolonIndex);
    const base64Data = data.substring(semicolonIndex + 1);

    // clipboard query ("?") is not supported for security (prevents clipboard theft)
    if (base64Data === "?") {
        console.log("OSC 52: clipboard query not supported");
        return true;
    }

    if (base64Data.length === 0) {
        return true;
    }

    if (clipboardSelection.length > 10) {
        console.log("OSC 52: clipboard selection too long", clipboardSelection);
        return true;
    }

    const estimatedDecodedSize = Math.ceil(base64Data.length * 0.75);
    if (estimatedDecodedSize > Osc52MaxDecodedSize) {
        console.log("OSC 52: data too large", estimatedDecodedSize, "bytes");
        return true;
    }

    try {
        // strip whitespace from base64 data (some terminals chunk with newlines per RFC 4648)
        const cleanBase64Data = base64Data.replace(/\s+/g, "");
        const decodedText = base64ToString(cleanBase64Data);

        // validate actual decoded size (base64 estimate can be off for multi-byte UTF-8)
        const actualByteSize = new TextEncoder().encode(decodedText).length;
        if (actualByteSize > Osc52MaxDecodedSize) {
            console.log("OSC 52: decoded text too large", actualByteSize, "bytes");
            return true;
        }

        fireAndForget(async () => {
            try {
                await navigator.clipboard.writeText(decodedText);
                dlog("OSC 52: copied", decodedText.length, "characters to clipboard");
            } catch (err) {
                console.error("OSC 52: clipboard write failed:", err);
            }
        });
    } catch (e) {
        console.error("OSC 52: base64 decode error:", e);
    }

    return true;
}

// for xterm handlers, we return true always because we "own" OSC 7.
// even if it is invalid we dont want to propagate to other handlers
export function handleOsc7Command(data: string, blockId: string, loaded: boolean): boolean {
    if (!loaded) {
        return true;
    }
    if (data == null || data.length == 0) {
        console.log("Invalid OSC 7 command received (empty)");
        return true;
    }
    if (data.length > 1024) {
        console.log("Invalid OSC 7, data length too long", data.length);
        return true;
    }

    let pathPart: string;
    try {
        const url = new URL(data);
        if (url.protocol !== "file:") {
            console.log("Invalid OSC 7 command received (non-file protocol)", data);
            return true;
        }
        pathPart = decodeURIComponent(url.pathname);

        // Normalize double slashes at the beginning to single slash
        if (pathPart.startsWith("//")) {
            pathPart = pathPart.substring(1);
        }

        // Handle Windows paths (e.g., /C:/... or /D:\...)
        if (/^\/[a-zA-Z]:[\\/]/.test(pathPart)) {
            // Strip leading slash and normalize to forward slashes
            pathPart = pathPart.substring(1).replace(/\\/g, "/");
        }

        // Handle UNC paths (e.g., /\\server\share)
        if (pathPart.startsWith("/\\\\")) {
            // Strip leading slash but keep backslashes for UNC
            pathPart = pathPart.substring(1);
        }
    } catch (e) {
        console.log("Invalid OSC 7 command received (parse error)", data, e);
        return true;
    }

    setTimeout(() => {
        fireAndForget(async () => {
            await RpcApi.SetMetaCommand(TabRpcClient, {
                oref: WOS.makeORef("block", blockId),
                meta: { "cmd:cwd": pathPart },
            });

            const rtInfo = { "shell:hascurcwd": true };
            const rtInfoData: CommandSetRTInfoData = {
                oref: WOS.makeORef("block", blockId),
                data: rtInfo,
            };
            await RpcApi.SetRTInfoCommand(TabRpcClient, rtInfoData).catch((e) =>
                console.log("error setting RT info", e)
            );
        });
    }, 0);
    return true;
}

export function handleOsc16162Command(data: string, blockId: string, loaded: boolean, termWrap: TermWrap): boolean {
    const terminal = termWrap.terminal;
    if (!loaded) {
        return true;
    }
    if (!data || data.length === 0) {
        return true;
    }

    const parts = data.split(";");
    const commandStr = parts[0];
    const jsonDataStr = parts.length > 1 ? parts.slice(1).join(";") : null;
    let parsedData: Record<string, any> = {};
    if (jsonDataStr) {
        try {
            parsedData = JSON.parse(jsonDataStr);
        } catch (e) {
            console.error("Error parsing OSC 16162 JSON data:", e);
        }
    }

    const cmd: Osc16162Command = { command: commandStr, data: parsedData } as Osc16162Command;
    const rtInfo: ObjRTInfo = {};
    switch (cmd.command) {
        case "A": {
            rtInfo["shell:state"] = "ready";
            globalStore.set(termWrap.shellIntegrationStatusAtom, "ready");
            globalStore.set(termWrap.claudeCodeActiveAtom, false);
            globalStore.set(termWrap.agentKindAtom, null);
            // finalize activity badge if "D" didn't fire (no-op if it already did)
            finishCommandActivity(termWrap, blockId, null);
            const marker = terminal.registerMarker(0);
            if (marker) {
                termWrap.promptMarkers.push(marker);
                // addTestMarkerDecoration(terminal, marker, termWrap);
                marker.onDispose(() => {
                    const idx = termWrap.promptMarkers.indexOf(marker);
                    if (idx !== -1) {
                        termWrap.promptMarkers.splice(idx, 1);
                    }
                });
            }
            break;
        }
        case "C":
            handleShellIntegrationCommandStart(termWrap, blockId, cmd, rtInfo);
            break;
        case "M":
            if (cmd.data.shell) {
                rtInfo["shell:type"] = cmd.data.shell;
            }
            if (cmd.data.shellversion) {
                rtInfo["shell:version"] = cmd.data.shellversion;
            }
            if (cmd.data.uname) {
                rtInfo["shell:uname"] = cmd.data.uname;
            }
            if (cmd.data.integration != null) {
                rtInfo["shell:integration"] = cmd.data.integration;
            }
            if (cmd.data.omz != null) {
                rtInfo["shell:omz"] = cmd.data.omz;
            }
            if (cmd.data.comp != null) {
                rtInfo["shell:comp"] = cmd.data.comp;
            }
            break;
        case "D":
            globalStore.set(termWrap.claudeCodeActiveAtom, false);
            globalStore.set(termWrap.agentKindAtom, null);
            finishCommandActivity(termWrap, blockId, cmd.data.exitcode ?? null);
            if (cmd.data.exitcode != null) {
                rtInfo["shell:lastcmdexitcode"] = cmd.data.exitcode;
            } else {
                rtInfo["shell:lastcmdexitcode"] = null;
            }
            break;
        case "I":
            if (cmd.data.inputempty != null) {
                rtInfo["shell:inputempty"] = cmd.data.inputempty;
            }
            break;
        case "R":
            globalStore.set(termWrap.shellIntegrationStatusAtom, null);
            globalStore.set(termWrap.claudeCodeActiveAtom, false);
            globalStore.set(termWrap.agentKindAtom, null);
            cancelCommandActivity(termWrap, blockId);
            if (terminal.buffer.active.type === "alternate") {
                terminal.write("\x1b[?1049l");
            }
            break;
    }

    if (Object.keys(rtInfo).length > 0) {
        setTimeout(() => {
            fireAndForget(async () => {
                const rtInfoData: CommandSetRTInfoData = {
                    oref: WOS.makeORef("block", blockId),
                    data: rtInfo,
                };
                await RpcApi.SetRTInfoCommand(TabRpcClient, rtInfoData).catch((e) =>
                    console.log("error setting RT info (OSC 16162)", e)
                );
            });
        }, 0);
    }

    return true;
}
