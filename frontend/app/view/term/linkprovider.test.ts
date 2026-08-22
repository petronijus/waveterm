// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, vi } from "vitest";
import { computeTermLinks, type LinkBufferTerminal } from "./linkprovider";

class FakeCell {
    chars: string;
    width: number;

    constructor(chars: string, width: number) {
        this.chars = chars;
        this.width = width;
    }

    getChars() {
        return this.chars;
    }

    getWidth() {
        return this.width;
    }
}

class FakeLine {
    text: string;
    length: number;
    isWrapped: boolean;

    constructor(text: string, cols: number, isWrapped = false) {
        this.text = text;
        this.length = cols;
        this.isWrapped = isWrapped;
    }

    translateToString(trimRight?: boolean) {
        return trimRight ? this.text.replace(/\s+$/, "") : this.text;
    }

    getCell(i: number) {
        const ch = this.text[i];
        return ch == null ? new FakeCell("", 1) : new FakeCell(ch, 1);
    }
}

function makeTerm(cols: number, lineSpecs: (string | [string, boolean])[]): LinkBufferTerminal {
    const lines = lineSpecs.map((spec) =>
        typeof spec === "string" ? new FakeLine(spec, cols) : new FakeLine(spec[0], cols, spec[1])
    );
    return {
        cols,
        buffer: {
            active: {
                getLine: (y: number) => lines[y] as any,
                getNullCell: () => new FakeCell("", 1) as any,
            },
        },
    };
}

const activate = vi.fn();

describe("computeTermLinks", () => {
    it("finds a plain single-line URL", () => {
        const term = makeTerm(40, ["see https://example.com here"]);
        const links = computeTermLinks(1, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://example.com"]);
    });

    it("follows a URL across a soft wrap (stock behavior)", () => {
        const term = makeTerm(10, ["https://ab", ["c.io/x yes", true]]);
        const links = computeTermLinks(1, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://abc.io/x"]);
    });

    it("follows a URL across a hard wrap at the terminal width", () => {
        const term = makeTerm(10, ["https://ab", "c.io/x yes"]);
        const links = computeTermLinks(1, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://abc.io/x"]);
        expect(links[0].range.start).toEqual({ x: 1, y: 1 });
        expect(links[0].range.end.y).toBe(2);
    });

    it("finds the same hard-wrapped URL when scanning the continuation line", () => {
        const term = makeTerm(10, ["https://ab", "c.io/x yes"]);
        const links = computeTermLinks(2, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://abc.io/x"]);
    });

    it("spans several consecutive hard-wrapped lines", () => {
        const term = makeTerm(10, ["https://ab", "c.io/depth", "/path stop"]);
        const links = computeTermLinks(2, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://abc.io/depth/path"]);
    });

    it("does not join across a hard break when the line stops short of the width", () => {
        const term = makeTerm(12, ["https://ab", "c.io/x"]);
        const links = computeTermLinks(1, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://ab"]);
    });

    it("does not join a full-width line whose last column is a space", () => {
        const term = makeTerm(10, ["https://a ", "b.io/x yes"]);
        const links = computeTermLinks(1, term, activate);
        expect(links.map((l) => l.text)).toEqual(["https://a"]);
    });

    it("finds nothing in plain text", () => {
        const term = makeTerm(20, ["hello world"]);
        expect(computeTermLinks(1, term, activate)).toEqual([]);
    });
});
