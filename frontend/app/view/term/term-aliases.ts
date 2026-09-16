// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Text expansion on the way from the keyboard to the pty: a shell alias is invisible to a TUI
// that reads the keystrokes itself (Claude Code, an editor), so the substitution happens here
// instead, where it works the same locally, inside such a TUI, and over a remote connection.

const TriggerSpace = " ";
const TriggerTab = "\t";
const Backspace = "\x7f";
const PasteStart = "\x1b[200~";
const PasteEnd = "\x1b[201~";
export const BracketedPasteMode = 2004;
export const MaxWrappedRows = 32;
// enough of a gap that the chunks land as separate reads (5ms already sufficed in testing)
export const AliasChunkDelayMs = 15;

export type AliasMatch = { alias: string; text: string };

export function isAliasTrigger(data: string): boolean {
    return data == TriggerSpace || data == TriggerTab;
}

export function matchAlias(textBeforeCursor: string, aliases: { [key: string]: string }): AliasMatch {
    if (aliases == null || !textBeforeCursor) {
        return null;
    }
    const word = textBeforeCursor.match(/(\S+)$/)?.[1];
    if (!word) {
        return null;
    }
    const text = aliases[word];
    if (!text) {
        return null;
    }
    return { alias: word, text };
}

// An expansion goes out as separate writes, never as one string. A chunk that mixes the erase
// with the bracketed paste, or carries anything after the paste's end marker, arrives at the
// program as a single read -- a TUI parsing raw stdin (Claude Code) then drops the paste
// entirely. Written separately, it reads exactly like a human pasting and then typing.
export function buildAliasChunks(match: AliasMatch, trigger: string, bracketedPaste: boolean): string[] {
    // an expansion never presses Enter for you, so a trailing newline in the value is dropped
    // rather than handed to the shell as "run this"
    const text = match.text.replace(/[\r\n]+$/, "");
    if (!text) {
        return null;
    }
    const chunks = [
        Backspace.repeat([...match.alias].length),
        bracketedPaste ? `${PasteStart}${text}${PasteEnd}` : text,
    ];
    // the space that triggered the expansion is kept, so typing carries on where it left off;
    // a tab is swallowed instead -- passing it on would fire the shell's completion
    if (trigger == TriggerSpace) {
        chunks.push(TriggerSpace);
    }
    return chunks;
}

// the cursor's row holds only the tail of a long input line, so walk back over the rows it
// wrapped from -- an alias typed across a wrap still has to match
export function joinWrappedRows(rows: { text: string; wrapped: boolean }[]): string {
    let text = "";
    for (let i = rows.length - 1; i >= 0 && rows.length - i <= MaxWrappedRows; i--) {
        text = rows[i].text + text;
        if (!rows[i].wrapped) {
            break;
        }
    }
    return text;
}
