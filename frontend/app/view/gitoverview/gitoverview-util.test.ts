// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import { fmtClock, parsePathList, RepoStateStyles, splitRepos, syncTargets } from "./gitoverview-util";

function repo(over: Partial<GitOverviewRepo>): GitOverviewRepo {
    return { path: "/dev/" + (over.rel ?? "r"), rel: "r", state: "in-sync", ...over };
}

describe("splitRepos", () => {
    it("keeps the engine's order and separates rows that need attention", () => {
        const { attention, inSync } = splitRepos([
            repo({ rel: "a", state: "behind" }),
            repo({ rel: "b" }),
            repo({ rel: "c", err: "push: rejected" }),
        ]);
        expect(attention.map((r) => r.rel)).toEqual(["a", "c"]);
        expect(inSync.map((r) => r.rel)).toEqual(["b"]);
    });
});

describe("syncTargets", () => {
    it("carries each row's own action and skips busy rows and rows without one", () => {
        const { targets, pulls, pushes } = syncTargets([
            repo({ rel: "b", state: "behind", action: "pull" }),
            repo({ rel: "a", state: "behind", action: "pull" }),
            repo({ rel: "c", state: "ahead", action: "push" }),
            repo({ rel: "d", state: "ahead", action: "push", busy: "pushing" }),
            repo({ rel: "e", state: "diverged" }),
        ]);
        expect(targets).toEqual([
            { path: "/dev/b", action: "pull" },
            { path: "/dev/a", action: "pull" },
            { path: "/dev/c", action: "push" },
        ]);
        expect(pulls).toEqual(["a", "b"]);
        expect(pushes).toEqual(["c"]);
    });
});

describe("parsePathList", () => {
    it("accepts lines and commas and drops blanks", () => {
        expect(parsePathList(" ~/Dev ,\n/work\n\n,")).toEqual(["~/Dev", "/work"]);
        expect(parsePathList(undefined)).toEqual([]);
    });
});

describe("RepoStateStyles", () => {
    it("has a style for every state the engine reports", () => {
        for (const s of [
            "error",
            "diverged",
            "behind",
            "ahead",
            "dirty",
            "upstream-gone",
            "no-upstream",
            "detached",
            "no-commits",
            "unknown",
            "in-sync",
        ]) {
            expect(RepoStateStyles[s], s).toBeDefined();
        }
    });
});

describe("fmtClock", () => {
    it("shows a dash for no time", () => {
        expect(fmtClock(0)).toBe("—");
        expect(fmtClock(new Date(2026, 9, 30, 9, 5).getTime())).toBe("09:05");
    });
});
