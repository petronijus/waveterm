// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { ChangelogModalV } from "@/app/modals/changelog";

const PreviewResult: ChangelogResult = {
    fetchedat: Date.now(),
    stale: false,
    entries: [
        {
            tag: "v0.14.5-pj.27",
            name: "Wave (pj) 0.14.5-pj.27",
            publishedat: "2026-09-16T07:41:25Z",
            url: "https://github.com/petronijus/waveterm/releases/tag/v0.14.5-pj.27",
            prerelease: false,
            body: "Second sweep of still-open upstream community PRs.\n\n### OSC 8 hyperlinks open again\nLinks now go through Wave's own `openLink`, on the same **⌘/Ctrl-click** gesture the fork already uses for detected URLs.\n\n### Also\n- Regression smoke suite gained an `osc8-link` check.",
        },
        {
            tag: "v0.14.5-pj.26",
            name: "v0.14.5-pj.26",
            publishedat: "2026-09-12T19:57:17Z",
            url: "https://github.com/petronijus/waveterm/releases/tag/v0.14.5-pj.26",
            prerelease: false,
            body: "Maintenance release.",
        },
        {
            tag: "v0.14.5-pj.25",
            name: "v0.14.5-pj.25 — undo close tab",
            publishedat: "2026-09-11T20:37:19Z",
            url: "https://github.com/petronijus/waveterm/releases/tag/v0.14.5-pj.25",
            prerelease: false,
            body: "Closing a tab by accident is recoverable again.",
        },
    ],
};

export function ChangelogModalPreview() {
    return (
        <ChangelogModalV
            result={PreviewResult}
            loading={false}
            version="0.14.5-pj.27"
            versionString="0.14.5-pj.27 (1758000000)"
            onRefresh={() => console.log("refresh")}
            onShowAbout={() => console.log("about")}
            onClose={() => console.log("close")}
        />
    );
}
