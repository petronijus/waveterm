// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { atoms, getSettingsKeyAtom, globalStore, WOS } from "@/store/global";
import { fireAndForget } from "@/util/util";
import type { TermWrap } from "./termwrap";

// Native OS notification when a long-running foreground command finishes while
// the window is unfocused. Opt-in (notify:commanddone), duration-gated
// (notify:commanddonethresholdms), and coalesced so a burst of finishes becomes
// one summary notification instead of a pile. Per-renderer state == per-window.

const DefaultThresholdMs = 30000;
const CoalesceWindowMs = 5000; // collect finishes for this long, then fire one notification

type DoneEvent = {
    windowId: string;
    tabId: string;
    tabName: string;
    message: string;
};

let pending: DoneEvent[] = [];
let flushTimeout: ReturnType<typeof setTimeout> | null = null;
let focusCancelSubscribed = false;

// Drop a queued burst the moment the user comes back to the window — they no
// longer need to be told, and the tab "done" badges already show what finished.
function ensureFocusCancel(): void {
    if (focusCancelSubscribed) {
        return;
    }
    focusCancelSubscribed = true;
    globalStore.sub(atoms.documentHasFocus, () => {
        if (globalStore.get(atoms.documentHasFocus)) {
            clearPending();
        }
    });
}

function clearPending(): void {
    if (flushTimeout != null) {
        clearTimeout(flushTimeout);
        flushTimeout = null;
    }
    pending = [];
}

function flush(): void {
    flushTimeout = null;
    const events = pending;
    pending = [];
    if (events.length === 0) {
        return;
    }
    // refocused during the coalesce window — they're back, don't interrupt.
    if (globalStore.get(atoms.documentHasFocus)) {
        return;
    }
    const last = events[events.length - 1];
    const single = events.length === 1;
    const title = single ? last.tabName : `${events.length} commands finished`;
    const body = single ? last.message : events.map((e) => `${e.tabName}: ${e.message}`).join("\n");
    // route to the electron main process (where handle_notify lives) — the
    // default route is wavesrv, which has no "notify" handler.
    fireAndForget(() =>
        RpcApi.NotifyCommand(
            TabRpcClient,
            {
                title,
                body,
                silent: false,
                windowid: last.windowId,
                tabid: last.tabId,
            },
            { route: "electron" }
        )
    );
}

function queue(event: DoneEvent): void {
    ensureFocusCancel();
    pending.push(event);
    if (flushTimeout == null) {
        flushTimeout = setTimeout(flush, CoalesceWindowMs);
    }
}

// Called from finishCommandActivity. Decides whether this finish qualifies for a
// notification and, if so, queues it. Cheap no-op when the feature is off.
export function maybeNotifyCommandDone(termWrap: TermWrap): void {
    if (!globalStore.get(getSettingsKeyAtom("notify:commanddone"))) {
        return;
    }
    if (globalStore.get(atoms.documentHasFocus)) {
        return; // window focused — the user is here, no notification
    }
    if (termWrap.cmdActivityStartTs <= 0) {
        return; // never actually started (no shell-integration markers)
    }
    const thresholdMs = globalStore.get(getSettingsKeyAtom("notify:commanddonethresholdms")) ?? DefaultThresholdMs;
    if (Date.now() - termWrap.cmdActivityStartTs < thresholdMs) {
        return; // too quick to bother announcing
    }
    const windowId = globalStore.get(atoms.uiContext)?.windowid;
    if (!windowId) {
        return;
    }
    const tab = WOS.getObjectValue<Tab>(WOS.makeORef("tab", termWrap.tabId));
    const tabName = tab?.name || "Terminal";
    const message = globalStore.get(termWrap.lastCommandAtom) || "Command finished";
    queue({ windowId, tabId: termWrap.tabId, tabName, message });
}

// Called from markCommandWaiting when an AI agent (Claude / Gemini / Codex) signals
// "your turn" (terminal bell or OSC 9) while the window is unfocused. Always on (no
// setting) — it's a high-signal, low-frequency event. Unlike the command-done path
// this is not a command finishing, so it isn't coalesced or duration-gated;
// markCommandWaiting already fires it only on the transition into the waiting state.
// Clicking it jumps to the tab.
export function maybeNotifyAgentWaiting(termWrap: TermWrap): void {
    if (globalStore.get(atoms.documentHasFocus)) {
        return; // window focused — you're already here
    }
    const windowId = globalStore.get(atoms.uiContext)?.windowid;
    if (!windowId) {
        return;
    }
    const tab = WOS.getObjectValue<Tab>(WOS.makeORef("tab", termWrap.tabId));
    const tabName = tab?.name || "Terminal";
    const kind = globalStore.get(termWrap.agentKindAtom);
    const label = kind ? kind[0].toUpperCase() + kind.slice(1) : "Agent";
    fireAndForget(() =>
        RpcApi.NotifyCommand(
            TabRpcClient,
            {
                title: `${label} is waiting for you`,
                body: tabName,
                silent: false,
                windowid: windowId,
                tabid: termWrap.tabId,
            },
            { route: "electron" }
        )
    );
}
