// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import {
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

// OSC 9 is the desktop-notification escape both Gemini CLI and Codex prefer (with the
// terminal bell as a fallback). We "own" OSC 9 so always return true to consume it;
// the agent "waiting for you" state it signals is now detected on the backend (it sees
// the raw stream even for background tabs), so there's nothing to do here.
export function handleOsc9Command(data: string, blockId: string, loaded: boolean, termWrap: TermWrap): boolean {
    return true;
}

function handleShellIntegrationCommandStart(
    termWrap: TermWrap,
    blockId: string,
    cmd: { command: "C"; data: { cmd64?: string } },
    rtInfo: ObjRTInfo // this is passed by reference and modified inside of this function
): void {
    rtInfo["shell:state"] = "running-command";
    globalStore.set(termWrap.shellIntegrationStatusAtom, "running-command");
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
export function handleOsc7Command(data: string, blockId: string, loaded: boolean, termWrap?: TermWrap): boolean {
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

    // Record the shell-reported cwd synchronously so a cmd:cwd watcher can tell a real
    // shell move (this) apart from an external "go here" request (e.g. picking a project).
    if (termWrap != null) {
        termWrap.lastReportedCwd = pathPart;
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

function parseColorToRgb(color: string): { r: number; g: number; b: number } {
    if (!color) {
        return null;
    }
    const c = color.trim();
    if (c.startsWith("#")) {
        let hex = c.slice(1);
        if (hex.length === 3) {
            hex = hex
                .split("")
                .map((ch) => ch + ch)
                .join("");
        }
        if (hex.length === 8) {
            hex = hex.slice(0, 6); // drop alpha — apps can't render it anyway
        }
        if (hex.length !== 6) {
            return null;
        }
        const num = parseInt(hex, 16);
        if (Number.isNaN(num)) {
            return null;
        }
        return { r: (num >> 16) & 0xff, g: (num >> 8) & 0xff, b: num & 0xff };
    }
    const m = c.match(/^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i);
    if (m) {
        return { r: Number(m[1]), g: Number(m[2]), b: Number(m[3]) };
    }
    return null;
}

// OSC 11 is the terminal background-color query/set. Apps (Claude Code, vim, tmux, …)
// query it to paint fills that match the terminal background. We force xterm's render
// background to transparent black (#00000000) so the themed block background shows
// through (see computeTheme) — but that means xterm's default answer is solid black,
// and apps then paint solid black blocks that clash with the real panel color. So
// intercept the *query* and answer with the real panel background; let xterm handle a
// *set* (return false). We "own" the query response, so return true for it.
export function handleOsc11Command(data: string, termWrap: TermWrap): boolean {
    if (!termWrap.loaded) {
        return true; // ignore queries replayed from the scrollback cache during load
    }
    if (data !== "?") {
        return false; // a set (or unknown) — let xterm's default handler run
    }
    const rgb = parseColorToRgb(termWrap.bgColor);
    if (rgb == null) {
        return false; // unknown background format — fall back to xterm's answer
    }
    const hx = (n: number) => n.toString(16).padStart(2, "0").repeat(2); // 8-bit → 16-bit per component
    const report = `\x1b]11;rgb:${hx(rgb.r)}/${hx(rgb.g)}/${hx(rgb.b)}\x1b\\`;
    termWrap.sendDataHandler?.(report);
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
