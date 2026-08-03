// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { getApi, getSettingsKeyAtom, globalStore } from "@/store/global";

// Shared diagnostic logger for the renderer half of the tab-activity pipeline (the
// notification half lives in the main process — emain/emain-term-notify.ts — and logs
// its decisions to waveapp.log directly).
// Gated by the term:activitydebug setting (default off; on in the dev-channel build).
// When on, lines are routed to the electron main process via the fe-log bridge so they
// land in waveapp.log next to the backend "[tabactivity]" lines — one file, one grep.

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
