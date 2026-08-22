// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Adapted from @xterm/addon-web-links (Copyright (c) 2019 The xterm.js authors, MIT).
// The stock addon only follows URLs across xterm's own soft wraps (isWrapped lines).
// TUI programs (Claude Code, tmux, less, ...) re-wrap output themselves and print real
// newlines at the terminal width, so a URL chopped by them spans "hard" line breaks the
// addon refuses to cross. This provider additionally joins a line with the next one when
// the line runs right through its last column without a soft-wrap continuation — the
// signature of a hard wrap at the terminal width.

import type { IBufferCell, IBufferLine, ILink, ILinkProvider, IViewportRange } from "@xterm/xterm";

const UrlRegex = /(https?|HTTPS?):[/]{2}[^\s"'!*(){}|\\^<>`]*[^\s"':,.!?{}|\\^~[\]`()<>]/;
const MaxLinkExpansionChars = 2048;

// structural subset of Terminal so tests can supply a fake buffer
export type LinkBufferTerminal = {
    cols: number;
    buffer: {
        active: {
            getLine(y: number): IBufferLine | undefined;
            getNullCell(): IBufferCell;
        };
    };
};

type LinkBuffer = LinkBufferTerminal["buffer"]["active"];

export type LinkHoverHandlers = {
    hover?: (event: MouseEvent, text: string, location: IViewportRange) => void;
    leave?: (event: MouseEvent, text: string) => void;
};

function isUrl(urlString: string): boolean {
    try {
        const url = new URL(urlString);
        const parsedBase =
            url.password && url.username
                ? `${url.protocol}//${url.username}:${url.password}@${url.host}`
                : url.username
                  ? `${url.protocol}//${url.username}@${url.host}`
                  : `${url.protocol}//${url.host}`;
        return urlString.toLocaleLowerCase().startsWith(parsedBase.toLocaleLowerCase());
    } catch (e) {
        return false;
    }
}

// "hard-full": content occupies the last column and the following line is NOT a soft-wrap
// continuation — i.e. the program printed a real newline exactly at the terminal width.
function isHardFullLine(line: IBufferLine, cell: IBufferCell): boolean {
    if (line.length === 0) {
        return false;
    }
    const last = line.getCell(line.length - 1, cell);
    if (last == null) {
        return false;
    }
    if (last.getWidth() === 0) {
        // trailing half of a wide char — the last column is occupied
        return true;
    }
    const chars = last.getChars();
    return chars !== "" && chars !== " ";
}

function joinsWithPrevious(buf: LinkBuffer, y: number, cell: IBufferCell): boolean {
    const line = buf.getLine(y);
    if (line == null) {
        return false;
    }
    if (line.isWrapped) {
        return true;
    }
    const prev = buf.getLine(y - 1);
    return prev != null && isHardFullLine(prev, cell);
}

/**
 * Collect the line strings joined around lineIndex (soft wraps AND hard-full wraps).
 * Expansion stops at whitespace or beyond MaxLinkExpansionChars, mirroring the stock
 * addon. Returns the line strings and the top line index.
 *
 * Lines are pulled with trimRight=true; every non-terminal line is fully occupied
 * (soft-wrap source or hard-full), so nothing is trimmed there and string indices map
 * 1:1 back to cells — except the early-wrapped wide char case corrected in mapStrIdx.
 */
function getWindowedLineStrings(lineIndex: number, term: LinkBufferTerminal): [string[], number] {
    const buf = term.buffer.active;
    const cell = buf.getNullCell();
    const startLine = buf.getLine(lineIndex);
    if (startLine == null) {
        return [[], lineIndex];
    }
    const lines: string[] = [];
    const currentContent = startLine.translateToString(true);
    let topIdx = lineIndex;

    // a URL crossing into this line from above must occupy its first cell
    if (currentContent[0] !== " ") {
        let length = 0;
        const upper: string[] = [];
        while (length < MaxLinkExpansionChars && joinsWithPrevious(buf, topIdx, cell)) {
            const upLine = buf.getLine(--topIdx);
            const content = upLine.translateToString(true);
            length += content.length;
            upper.push(content);
            if (content.indexOf(" ") !== -1) {
                break;
            }
        }
        upper.reverse();
        lines.push(...upper);
    }

    lines.push(currentContent);

    let bottomIdx = lineIndex;
    let length = 0;
    while (length < MaxLinkExpansionChars && joinsWithPrevious(buf, bottomIdx + 1, cell)) {
        const downLine = buf.getLine(++bottomIdx);
        const content = downLine.translateToString(true);
        length += content.length;
        lines.push(content);
        if (content.indexOf(" ") !== -1) {
            break;
        }
    }
    return [lines, topIdx];
}

/**
 * Map a string index back to buffer positions.
 * Returns buffer position as [lineIndex, columnIndex] 0-based,
 * or [-1, -1] in case the lookup ran into a non-existing line.
 */
function mapStrIdx(term: LinkBufferTerminal, lineIndex: number, rowIndex: number, stringIndex: number): [number, number] {
    const buf = term.buffer.active;
    const nullCell = buf.getNullCell();
    let start = rowIndex;
    while (stringIndex) {
        const line = buf.getLine(lineIndex);
        if (!line) {
            return [-1, -1];
        }
        for (let i = start; i < line.length; ++i) {
            const cell = line.getCell(i, nullCell);
            const chars = cell.getChars();
            const width = cell.getWidth();
            if (width) {
                stringIndex -= chars.length || 1;

                // correct stringIndex for early wrapped wide chars:
                // - currently only happens at last cell
                // - cells to the right are reset with chars='' and width=1 in InputHandler.print
                // - follow-up line must be wrapped and contain wide char at first cell
                // --> if all these conditions are met, correct stringIndex by +1
                if (i === line.length - 1 && chars === "") {
                    const nextLine = buf.getLine(lineIndex + 1);
                    if (nextLine && nextLine.isWrapped) {
                        const firstCell = nextLine.getCell(0, nullCell);
                        if (firstCell.getWidth() === 2) {
                            stringIndex += 1;
                        }
                    }
                }
            }
            if (stringIndex < 0) {
                return [lineIndex, i];
            }
        }
        lineIndex++;
        start = 0;
    }
    return [lineIndex, start];
}

export function computeTermLinks(
    y: number,
    term: LinkBufferTerminal,
    activate: (event: MouseEvent, uri: string) => void
): ILink[] {
    const rex = new RegExp(UrlRegex.source, "g");
    const [lines, startLineIndex] = getWindowedLineStrings(y - 1, term);
    const line = lines.join("");

    let match: RegExpExecArray;
    const result: ILink[] = [];

    while ((match = rex.exec(line))) {
        const text = match[0];
        if (!isUrl(text)) {
            continue;
        }

        // map string positions back to buffer positions
        // values are 0-based right side excluding
        const [startY, startX] = mapStrIdx(term, startLineIndex, 0, match.index);
        const [endY, endX] = mapStrIdx(term, startY, startX, text.length);

        if (startY === -1 || startX === -1 || endY === -1 || endX === -1) {
            continue;
        }

        // range expects values 1-based right side including, thus +1 except for endX
        const range = {
            start: { x: startX + 1, y: startY + 1 },
            end: { x: endX, y: endY + 1 },
        };

        result.push({ range, text, activate });
    }

    return result;
}

export class WaveLinkProvider implements ILinkProvider {
    terminal: LinkBufferTerminal;
    handler: (event: MouseEvent, uri: string) => void;
    hoverHandlers: LinkHoverHandlers;

    constructor(
        terminal: LinkBufferTerminal,
        handler: (event: MouseEvent, uri: string) => void,
        hoverHandlers: LinkHoverHandlers = {}
    ) {
        this.terminal = terminal;
        this.handler = handler;
        this.hoverHandlers = hoverHandlers;
    }

    provideLinks(y: number, callback: (links: ILink[] | undefined) => void): void {
        const links = computeTermLinks(y, this.terminal, this.handler);
        for (const link of links) {
            link.leave = this.hoverHandlers.leave;
            link.hover = (event: MouseEvent, uri: string) => {
                this.hoverHandlers.hover?.(event, uri, link.range);
            };
        }
        callback(links);
    }
}
