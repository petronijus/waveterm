// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Pure helpers of the git overview view (pkg/gitoverview is the engine).

export type RepoStateStyle = { dot: string; label: string };

// One entry per state the engine reports (pkg/gitoverview/model.go).
export const RepoStateStyles: Record<string, RepoStateStyle> = {
    error: { dot: "bg-error", label: "error" },
    diverged: { dot: "bg-error", label: "diverged" },
    behind: { dot: "bg-warning", label: "behind" },
    ahead: { dot: "bg-warning", label: "ahead" },
    dirty: { dot: "bg-sky-500", label: "changes" },
    "upstream-gone": { dot: "bg-zinc-500", label: "upstream gone" },
    "no-upstream": { dot: "bg-zinc-500", label: "no upstream" },
    detached: { dot: "bg-zinc-500", label: "detached" },
    "no-commits": { dot: "bg-zinc-500", label: "no commits" },
    unknown: { dot: "bg-zinc-700", label: "scanning" },
    "in-sync": { dot: "bg-success", label: "in sync" },
};

export function repoNeedsAttention(r: GitOverviewRepo): boolean {
    return r.state !== "in-sync" || !!r.err;
}

// splitRepos keeps the engine's order (most urgent first) and separates the
// rows that need attention from the ones in sync.
export function splitRepos(repos: GitOverviewRepo[]): { attention: GitOverviewRepo[]; inSync: GitOverviewRepo[] } {
    const attention: GitOverviewRepo[] = [];
    const inSync: GitOverviewRepo[] = [];
    for (const r of repos ?? []) {
        (repoNeedsAttention(r) ? attention : inSync).push(r);
    }
    return { attention, inSync };
}

// syncTargets are the rows "Sync all" acts on, each with the action its row
// shows right now; the engine performs nothing else.
export function syncTargets(repos: GitOverviewRepo[]): {
    targets: GitOverviewSyncTarget[];
    pulls: string[];
    pushes: string[];
} {
    const targets: GitOverviewSyncTarget[] = [];
    const pulls: string[] = [];
    const pushes: string[] = [];
    for (const r of repos ?? []) {
        if (r.busy || (r.action !== "pull" && r.action !== "push")) {
            continue;
        }
        targets.push({ path: r.path, action: r.action });
        (r.action === "pull" ? pulls : pushes).push(r.rel);
    }
    pulls.sort();
    pushes.sort();
    return { targets, pulls, pushes };
}

// parsePathList turns the settings field (one path per line, or commas) into
// a clean list.
export function parsePathList(text: string): string[] {
    return (text ?? "")
        .split(/[\n,]/)
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
}

export function fmtClock(unixMs: number): string {
    if (!unixMs) {
        return "—";
    }
    const d = new Date(unixMs);
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
