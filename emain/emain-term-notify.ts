// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { waveEventSubscribeSingle } from "@/app/store/wps";
import { RpcApi } from "@/app/store/wshclientapi";
import { fireAndForget } from "@/util/util";
import { getWaveWindowByWorkspaceId, WaveBrowserWindow } from "./emain-window";
import { ElectronWshClient, showWaveNotification } from "./emain-wsh";

// OS notifications for terminal activity (command finished / agent waiting), driven by
// the backend's Event_TermActivity stream. This lives in the electron MAIN process — not
// in the tab renderers — because a background tab's renderer is hidden and throttled, or
// may not exist at all (evicted from the tab cache, workspace not shown in any window).
// The main process always runs, owns the authoritative OS focus state of every window,
// and already owns notification display (showWaveNotification). The backend enriches
// each event with tabid/tabname/workspaceid (see enrichActivityRouting in
// termactivity.go), so no renderer-side object store is needed for routing.
//
// Command-done is coalesced per window so a burst of finishes becomes one summary
// notification; agent-waiting fires immediately. Focusing a window drops its queued
// burst — the user is back, and the tab "done" badges already show what finished.

const NotifyDefaultThresholdMs = 30000;
const CoalesceWindowMs = 5000; // collect finishes for this long, then fire one notification

type DoneEvent = {
    tabName: string;
    message: string;
    windowId: string;
    tabId: string;
};

type PendingBucket = {
    events: DoneEvent[];
    timer: NodeJS.Timeout;
};

// keyed by windowId; "" when the block's workspace is not shown in any open window
const pendingByWindow = new Map<string, PendingBucket>();

function windowForEvent(data: TermActivityData): WaveBrowserWindow {
    if (!data.workspaceid) {
        return null;
    }
    return getWaveWindowByWorkspaceId(data.workspaceid) ?? null;
}

function isWindowFocused(ww: WaveBrowserWindow): boolean {
    return ww != null && !ww.isDestroyed() && ww.isFocused();
}

function flushWindow(windowKey: string) {
    const bucket = pendingByWindow.get(windowKey);
    pendingByWindow.delete(windowKey);
    if (bucket == null || bucket.events.length === 0) {
        return;
    }
    const last = bucket.events[bucket.events.length - 1];
    const single = bucket.events.length === 1;
    const title = single ? last.tabName : `${bucket.events.length} commands finished`;
    const body = single ? last.message : bucket.events.map((e) => `${e.tabName}: ${e.message}`).join("\n");
    console.log(`[term-notify] fire done (${bucket.events.length} event(s)) title=${JSON.stringify(title)}`);
    showWaveNotification({
        title,
        body,
        silent: false,
        windowid: last.windowId || undefined,
        tabid: last.tabId || undefined,
    });
}

async function handleDone(data: TermActivityData): Promise<void> {
    const blk = data.blockid.substring(0, 8);
    const fullConfig = await RpcApi.GetFullConfigCommand(ElectronWshClient);
    if (!fullConfig?.settings?.["notify:commanddone"]) {
        return;
    }
    const thresholdMs = fullConfig?.settings?.["notify:commanddonethresholdms"] ?? NotifyDefaultThresholdMs;
    if ((data.durationms ?? 0) < thresholdMs) {
        return;
    }
    const ww = windowForEvent(data);
    if (isWindowFocused(ww)) {
        console.log(`[term-notify] done blk=${blk} SKIP (window focused)`);
        return;
    }
    const windowKey = ww?.waveWindowId ?? "";
    const event: DoneEvent = {
        tabName: data.tabname || "Terminal",
        message: data.command || "Command finished",
        windowId: ww?.waveWindowId ?? "",
        tabId: data.tabid ?? "",
    };
    let bucket = pendingByWindow.get(windowKey);
    if (bucket == null) {
        bucket = { events: [], timer: setTimeout(() => flushWindow(windowKey), CoalesceWindowMs) };
        pendingByWindow.set(windowKey, bucket);
    }
    bucket.events.push(event);
    console.log(`[term-notify] done blk=${blk} QUEUED (durationms=${data.durationms ?? 0} pending=${bucket.events.length})`);
}

function handleWaiting(data: TermActivityData): void {
    const blk = data.blockid.substring(0, 8);
    const ww = windowForEvent(data);
    if (isWindowFocused(ww)) {
        console.log(`[term-notify] waiting blk=${blk} SKIP (window focused)`);
        return;
    }
    const label = data.agentkind ? data.agentkind[0].toUpperCase() + data.agentkind.slice(1) : "Agent";
    console.log(`[term-notify] waiting blk=${blk} FIRE (label=${label} tab=${data.tabname ?? ""})`);
    showWaveNotification({
        title: `${label} is waiting for you`,
        body: data.tabname || "Terminal",
        silent: false,
        windowid: ww?.waveWindowId,
        tabid: data.tabid,
    });
}

// Called from the WaveBrowserWindow "focus" handler: drop that window's queued burst —
// the user no longer needs to be told what finished.
export function termNotifyWindowFocused(windowId: string): void {
    const bucket = pendingByWindow.get(windowId);
    if (bucket == null) {
        return;
    }
    clearTimeout(bucket.timer);
    pendingByWindow.delete(windowId);
    console.log(`[term-notify] pending cleared (window focused, dropped ${bucket.events.length})`);
}

let initialized = false;

// Must run after initElectronWshrpc: the wps subscription rides the electron wsh
// client's websocket, and wpsReconnectHandler (registered there) re-arms it on reconnect.
export function initTermActivityNotify(): void {
    if (initialized) {
        return;
    }
    initialized = true;
    waveEventSubscribeSingle({
        eventType: "block:termactivity",
        handler: (event) => {
            const data: TermActivityData = event.data;
            if (data?.blockid == null) {
                return;
            }
            if (data.state === "waiting") {
                handleWaiting(data);
            } else if (data.state === "done") {
                fireAndForget(() => handleDone(data));
            }
        },
    });
}
