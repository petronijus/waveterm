// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { atoms, globalStore } from "@/store/global";
import { fireAndForget } from "@/util/util";

// Native OS notifications for terminal activity, driven by the global
// term-activity subscriber (which derives state from backend Event_TermActivity).
// Command-done is coalesced so a burst of finishes becomes one summary notification;
// agent-waiting fires immediately. All gating (settings, duration, focus) happens in
// the caller — this module just queues/fires. Per-renderer state == per-window.

const CoalesceWindowMs = 5000; // collect finishes for this long, then fire one notification

type NotifyTarget = {
    windowId: string;
    tabId: string;
    tabName: string;
};

type DoneEvent = NotifyTarget & {
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

// Queue a finished long-running command for a coalesced OS notification. The caller
// has already decided this finish qualifies (feature on, window unfocused, long enough).
export function queueCommandDoneNotification(event: DoneEvent): void {
    ensureFocusCancel();
    pending.push(event);
    if (flushTimeout == null) {
        flushTimeout = setTimeout(flush, CoalesceWindowMs);
    }
}

// Fire an immediate "agent is waiting for you" notification. The caller has already
// confirmed the window is unfocused. Clicking it jumps to the tab.
export function fireAgentWaitingNotification(target: NotifyTarget, agentKind: string): void {
    const label = agentKind ? agentKind[0].toUpperCase() + agentKind.slice(1) : "Agent";
    fireAndForget(() =>
        RpcApi.NotifyCommand(
            TabRpcClient,
            {
                title: `${label} is waiting for you`,
                body: target.tabName,
                silent: false,
                windowid: target.windowId,
                tabid: target.tabId,
            },
            { route: "electron" }
        )
    );
}
