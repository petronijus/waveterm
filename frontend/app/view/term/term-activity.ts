// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { waveEventSubscribeSingle } from "@/app/store/wps";
import { atoms, getSettingsKeyAtom, globalStore, WOS } from "@/store/global";
import { activityLog, shortBlk } from "./activity-log";
import { fireAgentWaitingNotification, queueCommandDoneNotification } from "./notify-commanddone";

// The backend (Event_TermActivity) decides what each terminal is doing — "working",
// "waiting" for an AI agent, "done", etc. — by scanning the PTY stream, and it sets
// the tab's working/done badge straight from the backend (see termactivity.go), so
// the indicator is reliable on every tab regardless of which renderer is alive.
//
// This module is the focus-aware half: it turns the same events into OS notifications
// (command finished / agent waiting). Every tab is its own renderer with an AllScopes
// subscription, so a backend event reaches EVERY renderer — we de-dupe by firing only
// from the renderer that owns the block's tab.

const NotifyDefaultThresholdMs = 30000;

function thisTabId(): string {
    return globalStore.get(atoms.staticTabId);
}

function thisTab(): Tab {
    const tabId = thisTabId();
    if (!tabId) {
        return null;
    }
    return WOS.getObjectValue<Tab>(WOS.makeORef("tab", tabId));
}

function isBlockOnThisTab(blockId: string): boolean {
    return thisTab()?.blockids?.includes(blockId) ?? false;
}

function notifyTarget(): { windowId: string; tabId: string; tabName: string } {
    const windowId = globalStore.get(atoms.uiContext)?.windowid;
    const tabId = thisTabId();
    if (!windowId || !tabId) {
        return null;
    }
    return { windowId, tabId, tabName: thisTab()?.name || "Terminal" };
}

function maybeNotifyWaiting(data: TermActivityData): void {
    if (globalStore.get(atoms.documentHasFocus)) {
        activityLog(`waiting blk=${shortBlk(data.blockid)} SKIP notify (window focused)`);
        return; // window focused — you're already here
    }
    const target = notifyTarget();
    if (target == null) {
        activityLog(`waiting blk=${shortBlk(data.blockid)} SKIP notify (no target window/tab)`);
        return;
    }
    activityLog(`waiting blk=${shortBlk(data.blockid)} FIRE notify (agent=${data.agentkind ?? ""} tab=${target.tabName})`);
    fireAgentWaitingNotification(target, data.agentkind);
}

function maybeNotifyDone(data: TermActivityData): void {
    const blk = shortBlk(data.blockid);
    if (!globalStore.get(getSettingsKeyAtom("notify:commanddone"))) {
        activityLog(`done blk=${blk} SKIP notify (notify:commanddone off)`);
        return;
    }
    if (globalStore.get(atoms.documentHasFocus)) {
        activityLog(`done blk=${blk} SKIP notify (window focused)`);
        return; // window focused — the user is here, no notification
    }
    const thresholdMs =
        globalStore.get(getSettingsKeyAtom("notify:commanddonethresholdms")) ?? NotifyDefaultThresholdMs;
    if ((data.durationms ?? 0) < thresholdMs) {
        activityLog(`done blk=${blk} SKIP notify (durationms=${data.durationms ?? 0} < threshold=${thresholdMs})`);
        return; // too quick to bother announcing
    }
    const target = notifyTarget();
    if (target == null) {
        activityLog(`done blk=${blk} SKIP notify (no target window/tab)`);
        return;
    }
    activityLog(`done blk=${blk} FIRE notify (durationms=${data.durationms ?? 0} command=${JSON.stringify((data.command || "").slice(0, 60))} tab=${target.tabName})`);
    queueCommandDoneNotification({ ...target, message: data.command || "Command finished" });
}

function handleActivityEvent(data: TermActivityData): void {
    if (data?.blockid == null) {
        return;
    }
    const onThisTab = isBlockOnThisTab(data.blockid);
    // Log every event this renderer sees, including ones for other tabs (ownThisTab=false)
    // — that's how we confirm whether the owning renderer ever received the event at all.
    activityLog(
        `event state=${data.state} blk=${shortBlk(data.blockid)} ownThisTab=${onThisTab}` +
            (data.durationms != null ? ` durationms=${data.durationms}` : "") +
            (data.exitcode != null ? ` exit=${data.exitcode}` : "") +
            (data.agentkind ? ` agent=${data.agentkind}` : "")
    );
    // Only the renderer that owns this block's tab fires the notification, so a single
    // event doesn't notify once per open tab.
    if (!onThisTab) {
        return;
    }
    if (data.state === "waiting") {
        maybeNotifyWaiting(data);
    } else if (data.state === "done") {
        maybeNotifyDone(data);
    }
}

let initialized = false;

export function initTermActivity(): void {
    if (initialized) {
        return;
    }
    initialized = true;
    waveEventSubscribeSingle({
        eventType: "block:termactivity",
        handler: (event) => {
            if (event.data == null) {
                return;
            }
            handleActivityEvent(event.data);
        },
    });
}
