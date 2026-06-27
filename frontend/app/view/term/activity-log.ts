// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { getApi, getSettingsKeyAtom, globalStore } from "@/store/global";

// Shared diagnostic logger for the tab-activity / command-done-notification pipeline.
// Gated by the term:activitydebug setting (default off; on in the dev-channel build).
// When on, lines are routed to the electron main process via the fe-log bridge so they
// land in waveapp.log next to the backend "[tabactivity]" lines — one file, one grep.
// Lives in its own module so both term-activity.ts and notify-commanddone.ts can import
// it without forming an import cycle.

export function activityDebug(): boolean {
    return !!globalStore.get(getSettingsKeyAtom("term:activitydebug"));
}

export function activityLog(msg: string): void {
    if (!activityDebug()) {
        return;
    }
    try {
        getApi().sendLog("[tabactivity][fe] " + msg);
    } catch {
        // best-effort diagnostic; never let logging throw into the event/notify path
    }
}

export function shortBlk(blockId: string): string {
    return blockId == null ? "?" : blockId.slice(0, 8);
}
