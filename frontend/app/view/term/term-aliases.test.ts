// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import { buildAliasChunks, isAliasTrigger, joinWrappedRows, matchAlias } from "./term-aliases";

const Aliases = { AtoRun: "docker compose up -d", gco: "git checkout ", multi: "line one\nline two" };

describe("isAliasTrigger", () => {
    it("fires on a space or a tab only", () => {
        expect(isAliasTrigger(" ")).toBe(true);
        expect(isAliasTrigger("\t")).toBe(true);
        expect(isAliasTrigger("\r")).toBe(false);
        expect(isAliasTrigger("a")).toBe(false);
        expect(isAliasTrigger("  ")).toBe(false);
    });
});

describe("matchAlias", () => {
    it("matches the word in front of the cursor", () => {
        expect(matchAlias("AtoRun", Aliases)).toEqual({ alias: "AtoRun", text: "docker compose up -d" });
        expect(matchAlias("petr@pc:~$ AtoRun", Aliases)).toEqual({
            alias: "AtoRun",
            text: "docker compose up -d",
        });
    });

    it("does not match a word it is only the tail of", () => {
        expect(matchAlias("notAtoRun", Aliases)).toBe(null);
    });

    it("is case sensitive and ignores unknown or empty words", () => {
        expect(matchAlias("atorun", Aliases)).toBe(null);
        expect(matchAlias("git status ", Aliases)).toBe(null);
        expect(matchAlias("", Aliases)).toBe(null);
        expect(matchAlias("AtoRun", null)).toBe(null);
    });
});

describe("buildAliasChunks", () => {
    const match = { alias: "AtoRun", text: "docker compose up -d" };

    it("erases the alias, pastes the expansion and keeps the space, one write each", () => {
        expect(buildAliasChunks(match, " ", true)).toEqual([
            "\x7f".repeat(6),
            "\x1b[200~docker compose up -d\x1b[201~",
            " ",
        ]);
    });

    it("swallows the tab so the shell does not complete on it", () => {
        expect(buildAliasChunks(match, "\t", true)).toEqual([
            "\x7f".repeat(6),
            "\x1b[200~docker compose up -d\x1b[201~",
        ]);
    });

    it("sends the text plain when bracketed paste is off", () => {
        expect(buildAliasChunks(match, " ", false)).toEqual(["\x7f".repeat(6), "docker compose up -d", " "]);
    });

    // Claude Code drops a paste whose read carries anything after the end marker, which is how
    // the first version of this silently expanded to nothing inside it
    it("never puts anything after the end of a bracketed paste in the same write", () => {
        for (const trigger of [" ", "\t"]) {
            for (const chunk of buildAliasChunks(match, trigger, true)) {
                expect(chunk.includes("\x1b[201~") ? chunk.endsWith("\x1b[201~") : true).toBe(true);
            }
        }
    });

    it("never presses enter for you", () => {
        expect(buildAliasChunks({ alias: "r", text: "make run\n" }, " ", false)).toEqual(["\x7f", "make run", " "]);
        expect(buildAliasChunks({ alias: "r", text: "\n" }, " ", false)).toBe(null);
    });
});

describe("joinWrappedRows", () => {
    it("joins the rows a long line wrapped from", () => {
        const rows = [
            { text: "echo ", wrapped: false },
            { text: "some very long argument ", wrapped: true },
            { text: "AtoRun", wrapped: true },
        ];
        expect(joinWrappedRows(rows)).toBe("echo some very long argument AtoRun");
    });

    it("stops at the row that started the line", () => {
        const rows = [
            { text: "an earlier command", wrapped: false },
            { text: "petr@pc:~$ AtoRun", wrapped: false },
        ];
        expect(joinWrappedRows(rows)).toBe("petr@pc:~$ AtoRun");
    });
});
