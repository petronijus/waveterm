// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { beforeEach, describe, expect, it, vi } from "vitest";

// termutil pulls in RPC / theme / store modules that don't load in Node.js
vi.mock("@/app/store/wshclientapi", () => ({ RpcApi: {} }));
vi.mock("@/app/store/wshrpcutil", () => ({ TabRpcClient: {} }));
vi.mock("@/app/uitheme", () => ({ elevate: vi.fn(), getActiveUITheme: vi.fn() }));

let mockSettings: Record<string, unknown> = {};
vi.mock("@/store/global", () => ({
    getSettingsKeyAtom: vi.fn((key: string) => key),
    globalStore: { get: vi.fn((key: string) => mockSettings[key as string]) },
}));

import { getTerminalCopyText, trimTerminalSelection, unwrapHardWrappedSelection } from "./termutil";

describe("unwrapHardWrappedSelection", () => {
    it("joins a line hard-wrapped exactly at the terminal width", () => {
        expect(unwrapHardWrappedSelection("https://ex\nample.com/", 10)).toBe("https://example.com/");
    });

    it("joins consecutive full-width lines", () => {
        expect(unwrapHardWrappedSelection("aaaaaaaaaa\nbbbbbbbbbb\ncc", 10)).toBe("aaaaaaaaaabbbbbbbbbbcc");
    });

    it("joins a soft-joined logical line whose length is an exact multiple of the width", () => {
        expect(unwrapHardWrappedSelection("a".repeat(20) + "\nrest", 10)).toBe("a".repeat(20) + "rest");
    });

    it("leaves short lines alone", () => {
        expect(unwrapHardWrappedSelection("foo\nbar", 10)).toBe("foo\nbar");
    });

    it("does not join a full-width line that ends with a space (padded TUI line)", () => {
        expect(unwrapHardWrappedSelection("abcdefghi \nnext", 10)).toBe("abcdefghi \nnext");
    });

    it("preserves empty lines", () => {
        expect(unwrapHardWrappedSelection("\n\nabc", 10)).toBe("\n\nabc");
    });

    it("normalizes CRLF separators", () => {
        expect(unwrapHardWrappedSelection("foo\r\nbar", 10)).toBe("foo\nbar");
        expect(unwrapHardWrappedSelection("https://ex\r\nample.com/", 10)).toBe("https://example.com/");
    });

    it("returns the text untouched for a bogus width", () => {
        expect(unwrapHardWrappedSelection("foo\nbar", 0)).toBe("foo\nbar");
        expect(unwrapHardWrappedSelection("foo\nbar", null)).toBe("foo\nbar");
    });
});

describe("trimTerminalSelection", () => {
    it("trims trailing whitespace per line", () => {
        expect(trimTerminalSelection("foo   \nbar\t\nbaz")).toBe("foo\nbar\nbaz");
    });
});

describe("getTerminalCopyText", () => {
    const makeTerminal = (selection: string, cols: number) =>
        ({ getSelection: () => selection, cols }) as any;

    beforeEach(() => {
        mockSettings = {};
    });

    it("applies unwrap and trim by default", () => {
        const term = makeTerminal("https://ex\nample.io/\ndone   ", 10);
        expect(getTerminalCopyText(term)).toBe("https://example.io/\ndone");
    });

    it("skips unwrapping when term:copyunwrap is false", () => {
        mockSettings["term:copyunwrap"] = false;
        const term = makeTerminal("https://ex\nample.com/", 10);
        expect(getTerminalCopyText(term)).toBe("https://ex\nample.com/");
    });

    it("skips trimming when term:trimtrailingwhitespace is false", () => {
        mockSettings["term:trimtrailingwhitespace"] = false;
        const term = makeTerminal("foo   \nbar", 10);
        expect(getTerminalCopyText(term)).toBe("foo   \nbar");
    });

    it("returns empty string for no selection", () => {
        expect(getTerminalCopyText(makeTerminal("", 10))).toBe("");
    });
});
