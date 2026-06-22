// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { clearBadgeById, setBadge } from "@/app/store/badge";
import { waveEventSubscribeSingle } from "@/app/store/wps";
import { atoms, getSettingsKeyAtom, globalStore, WOS } from "@/store/global";
import { atom } from "jotai";
import { v7 as uuidv7 } from "uuid";
import { fireAgentWaitingNotification, queueCommandDoneNotification } from "./notify-commanddone";

// Global, app-lifetime presentation layer for terminal command-activity. The
// backend (Event_TermActivity) decides WHAT each terminal is doing — running and
// producing output ("working"), come-to-rest ("thinking"), an AI agent waiting
// ("waiting"), or finished ("done") — by scanning the PTY stream, so it works even
// when a tab is in the background. This module turns that into focus-aware tab badges
// and OS notifications.
//
// Architecture: every tab is its own renderer (a WebContentsView kept warm off-screen
// when not visible), each with its own copy of this module and an AllScopes
// subscription. So a backend event for one block reaches EVERY renderer. To avoid
// renderers fighting over the shared badge, each renderer manages only the blocks on
// ITS OWN tab (atoms.staticTabId never changes for a renderer). The owning renderer
// stays alive in the background, so it can badge its own tab even while you're looking
// at another; the badge then syncs to every renderer's tab bar via the badge store.
//
// "Watched" — the user is actually looking at this tab — is `activetabid === my tab`
// AND the window is focused. documentHasFocus alone is NOT enough: within one focused
// window every tab's renderer reports focus, so only the active-tab comparison tells a
// background renderer that it's hidden (this was the root of the original bug).

const ActivityPriority = 5;
const NotifyDefaultThresholdMs = 30000;

type Display = "none" | "spinner" | "check" | "xmark" | "dots";

type BlockActivity = {
    badgeId: string; // stable per-block activity badge id (clear-by-id + set re-uses it)
    live: boolean; // currently "working" — re-show the spinner when you look away
    pending: Display; // what to show while not watched (consumed to "none" once watched)
    displayed: Display; // what is currently rendered (so we only publish on change)
};

const blockActivity = new Map<string, BlockActivity>();

// the globally-active tab id (synced from the backend on the workspace object)
const activeTabIdAtom = atom((get) => get(atoms.workspace)?.activetabid);

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

// Is the user looking at this tab right now? Its renderer's tab is the active tab AND
// the window is focused.
function isWatched(): boolean {
    if (!globalStore.get(atoms.documentHasFocus)) {
        return false;
    }
    return globalStore.get(atoms.workspace)?.activetabid === thisTabId();
}

function ensureEntry(blockId: string): BlockActivity {
    let entry = blockActivity.get(blockId);
    if (entry == null) {
        entry = { badgeId: uuidv7(), live: false, pending: "none", displayed: "none" };
        blockActivity.set(blockId, entry);
    }
    return entry;
}

function applyDisplay(blockId: string, entry: BlockActivity, desired: Display): void {
    if (entry.displayed === desired) {
        return;
    }
    entry.displayed = desired;
    if (desired === "none") {
        clearBadgeById(blockId, entry.badgeId);
        return;
    }
    const spec: Record<Exclude<Display, "none">, { icon: string; color: string; pidlinked: boolean }> = {
        spinner: { icon: "spinner+spin", color: "var(--accent-color)", pidlinked: true },
        check: { icon: "circle-check", color: "var(--success-color)", pidlinked: false },
        xmark: { icon: "circle-xmark", color: "var(--error-color)", pidlinked: false },
        dots: { icon: "comment-dots", color: "#fbbf24", pidlinked: false },
    };
    const s = spec[desired];
    // clear-by-id first so the set is unconditional (the badge store only overwrites a
    // higher-or-equal priority; re-using the id + clearing sidesteps that).
    clearBadgeById(blockId, entry.badgeId);
    setBadge(blockId, {
        badgeid: entry.badgeId,
        icon: s.icon,
        color: s.color,
        priority: ActivityPriority,
        pidlinked: s.pidlinked,
    });
}

function doneDisplay(data: TermActivityData): Display {
    if (!data.visible) {
        return "none"; // never showed a spinner (quick/silent command) — no badge
    }
    return data.exitcode == null || data.exitcode === 0 ? "check" : "xmark";
}

function handleActivityEvent(data: TermActivityData): void {
    if (data?.blockid == null) {
        return;
    }
    const blockId = data.blockid;
    // Only the renderer that owns this block's tab manages its badge. Every other
    // renderer drops it (its tab bar still shows the badge via the badge store).
    if (!isBlockOnThisTab(blockId)) {
        return;
    }
    const entry = ensureEntry(blockId);
    const watched = isWatched();

    switch (data.state) {
        case "working":
            entry.live = true;
            entry.pending = "spinner";
            break;
        case "thinking":
            entry.live = false;
            entry.pending = "check";
            break;
        case "waiting":
            entry.live = false;
            entry.pending = "dots";
            maybeNotifyWaiting(data);
            break;
        case "done":
            entry.live = false;
            entry.pending = doneDisplay(data);
            maybeNotifyDone(data);
            break;
        case "none":
        default:
            entry.live = false;
            entry.pending = "none";
            break;
    }

    // While you're looking, show nothing. An attention badge (check/dots) that arrives
    // while watched is consumed — it must not pop up later when you leave (you already
    // saw it). A live "working" badge keeps its pending so it reappears the moment you
    // switch away.
    if (watched) {
        if (!entry.live) {
            entry.pending = "none";
        }
        applyDisplay(blockId, entry, "none");
        return;
    }
    applyDisplay(blockId, entry, entry.pending);
}

// Re-evaluate every owned block when this tab becomes (in)active or the window focus
// changes: a working terminal reveals/hides its spinner; looking at the tab consumes a
// pending attention badge.
function reevaluateAll(): void {
    const watched = isWatched();
    for (const [blockId, entry] of blockActivity) {
        if (entry.pending === "none" && entry.displayed === "none") {
            continue;
        }
        if (watched) {
            if (!entry.live) {
                entry.pending = "none";
            }
            applyDisplay(blockId, entry, "none");
            continue;
        }
        applyDisplay(blockId, entry, entry.pending);
    }
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
        return; // window focused — you're already here
    }
    const target = notifyTarget();
    if (target == null) {
        return;
    }
    fireAgentWaitingNotification(target, data.agentkind);
}

function maybeNotifyDone(data: TermActivityData): void {
    if (!globalStore.get(getSettingsKeyAtom("notify:commanddone"))) {
        return;
    }
    if (globalStore.get(atoms.documentHasFocus)) {
        return; // window focused — the user is here, no notification
    }
    const thresholdMs = globalStore.get(getSettingsKeyAtom("notify:commanddonethresholdms")) ?? NotifyDefaultThresholdMs;
    if ((data.durationms ?? 0) < thresholdMs) {
        return; // too quick to bother announcing
    }
    const target = notifyTarget();
    if (target == null) {
        return;
    }
    queueCommandDoneNotification({ ...target, message: data.command || "Command finished" });
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
    globalStore.sub(activeTabIdAtom, reevaluateAll);
    globalStore.sub(atoms.documentHasFocus, reevaluateAll);
}
