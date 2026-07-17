// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { uiThemeOverrideAtom } from "@/app/uitheme";
import type { TermViewModel } from "@/app/view/term/term-model";
import { computeTheme } from "@/app/view/term/termutil";
import { TermWrap } from "@/app/view/term/termwrap";
import { atoms } from "@/store/global";
import { useAtomValue } from "jotai";
import { useEffect } from "react";

interface TermThemeProps {
    blockId: string;
    termRef: React.RefObject<TermWrap>;
    model: TermViewModel;
}

function termThemesEqual(a: TermThemeType, b: TermThemeType): boolean {
    if (a === b) {
        return true;
    }
    if (a == null || b == null) {
        return false;
    }
    const aKeys = Object.keys(a);
    const bKeys = Object.keys(b);
    if (aKeys.length !== bKeys.length) {
        return false;
    }
    return aKeys.every((key) => a[key] === b[key]);
}

const TermThemeUpdater = ({ blockId, model, termRef }: TermThemeProps) => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const blockTermTheme = useAtomValue(model.termThemeNameAtom);
    const transparency = useAtomValue(model.termTransparencyAtom);
    const uiOverride = useAtomValue(uiThemeOverrideAtom);
    const [theme, _] = computeTheme(fullConfig, blockTermTheme, transparency, uiOverride);
    useEffect(() => {
        const terminal = termRef.current?.terminal;
        if (!terminal) {
            return;
        }
        // computeTheme returns a fresh object every render, and xterm treats every
        // options.theme assignment as a color change — with DECSET 2031 active it then
        // reports CSI ?997;n to the shell, which shows up as literal "997;1n" garbage
        // at the prompt. Only reassign when the colors actually changed.
        if (termThemesEqual(terminal.options.theme as TermThemeType, theme)) {
            return;
        }
        terminal.options.theme = theme;
    }, [theme]);
    return null;
};

export { TermThemeUpdater };
