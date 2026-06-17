// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// App-wide UI theme applier. Reads the selected UI theme (settings key
// "app:theme") from the full config and applies it to the :root CSS variables
// at runtime, deriving the alpha-blended variants from the theme's solid colors.
// This is the UI-color analog of TermThemeUpdater (which only themes xterm).

import { atoms } from "@/store/global";
import { atom, useAtomValue } from "jotai";
import { useEffect } from "react";

const DefaultUITheme = "dracula";

// Live-preview override: while the theme editor is open and editing, it sets this
// to the draft theme. Everything that themes off the active UI theme (the CSS-var
// applier AND the terminal via computeTheme) reads the override first, so edits
// preview instantly across the whole app — including xterm — before saving.
export const uiThemeOverrideAtom = atom(null as UIThemeType | null);

// Cache the last-applied (saved) theme so we can re-apply it synchronously on the
// next launch, before the config arrives over the websocket — eliminates the
// flash-of-default-theme (FOUC) when a non-default theme is selected.
const CACHE_KEY = "waveterm:uitheme";

// Variables we set at runtime so we can cleanly remove them when no theme is active.
const MANAGED_VARS = [
    "--main-bg-color",
    "--block-bg-solid-color",
    "--block-bg-color",
    "--panel-bg-color",
    "--main-text-color",
    "--secondary-text-color",
    "--grey-text-color",
    "--accent-color",
    "--tab-green",
    "--color-accent",
    "--color-accent-300",
    "--color-accent-400",
    "--color-accent-500",
    "--color-accent-600",
    "--color-accenthover",
    "--color-accentbg",
    "--button-green-border-color",
    "--form-element-primary-color",
    "--link-color",
    "--border-color",
    "--modal-border-color",
    "--form-element-border-color",
    "--hover-bg-color",
    "--highlight-bg-color",
    "--modal-bg-color",
    "--error-color",
    "--warning-color",
    "--success-color",
    "--keybinding-color",
    "--keybinding-bg-color",
    "--keybinding-border-color",
    "--scrollbar-thumb-color",
];

// Parse "#rgb" / "#rrggbb" (or pass through rgb()/rgba()) into an rgba() string.
function withAlpha(color: string, alpha: number): string {
    if (color == null || color === "") {
        return color;
    }
    const hex = color.trim();
    if (hex.startsWith("#")) {
        let r: number, g: number, b: number;
        const body = hex.slice(1);
        if (body.length === 3) {
            r = parseInt(body[0] + body[0], 16);
            g = parseInt(body[1] + body[1], 16);
            b = parseInt(body[2] + body[2], 16);
        } else if (body.length === 6) {
            r = parseInt(body.slice(0, 2), 16);
            g = parseInt(body.slice(2, 4), 16);
            b = parseInt(body.slice(4, 6), 16);
        } else {
            return color;
        }
        return `rgba(${r}, ${g}, ${b}, ${alpha})`;
    }
    // rgb(r,g,b) -> rgba(r,g,b,alpha); rgba(...) already has alpha, leave as-is.
    const m = hex.match(/^rgb\(([^)]+)\)$/i);
    if (m) {
        return `rgba(${m[1]}, ${alpha})`;
    }
    return color;
}

function parseRgb(color: string): { r: number; g: number; b: number } | null {
    if (color == null) return null;
    const c = color.trim();
    if (c.startsWith("#")) {
        const body = c.slice(1);
        if (body.length === 3) {
            return {
                r: parseInt(body[0] + body[0], 16),
                g: parseInt(body[1] + body[1], 16),
                b: parseInt(body[2] + body[2], 16),
            };
        }
        if (body.length === 6) {
            return {
                r: parseInt(body.slice(0, 2), 16),
                g: parseInt(body.slice(2, 4), 16),
                b: parseInt(body.slice(4, 6), 16),
            };
        }
        return null;
    }
    const m = c.match(/^rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/i);
    if (m) return { r: +m[1], g: +m[2], b: +m[3] };
    return null;
}

// Derive the panel/block background from the app background: keep them decoupled
// so panels read as a distinct surface — a bit LIGHTER than a dark background and
// a bit DARKER than a light one (subtle elevation, like VS Code editor vs sidebar).
function elevate(color: string, amount = 0.06): string {
    const rgb = parseRgb(color);
    if (rgb == null) return color;
    const luminance = (0.299 * rgb.r + 0.587 * rgb.g + 0.114 * rgb.b) / 255;
    const isDark = luminance < 0.5;
    const target = isDark ? 255 : 0; // lighten dark themes, darken light themes
    const mix = (ch: number) => Math.round(ch + (target - ch) * amount);
    return `rgb(${mix(rgb.r)}, ${mix(rgb.g)}, ${mix(rgb.b)})`;
}

export function applyUITheme(theme: UIThemeType | null) {
    const root = document.documentElement;
    if (theme == null) {
        for (const v of MANAGED_VARS) {
            root.style.removeProperty(v);
        }
        return;
    }
    const set = (name: string, value: string) => {
        if (value != null && value !== "") {
            root.style.setProperty(name, value);
        } else {
            root.style.removeProperty(name);
        }
    };
    const accent = theme.accent;
    const border = theme.border;
    const bg = theme.background;

    // Panels/blocks are decoupled from the app background: always derived a touch
    // lighter (dark themes) or darker (light themes) so they read as a distinct
    // surface and track the background automatically as it's edited.
    const panel = elevate(bg);
    set("--main-bg-color", bg);
    set("--block-bg-solid-color", panel);
    set("--block-bg-color", withAlpha(panel, 0.85));
    set("--panel-bg-color", panel);

    set("--main-text-color", theme.foreground);
    set("--secondary-text-color", theme.secondaryText);
    set("--grey-text-color", theme.greyText);

    set("--accent-color", accent);
    set("--tab-green", accent);
    set("--color-accent", accent);
    set("--color-accent-300", accent);
    set("--color-accent-400", accent);
    set("--color-accent-500", accent);
    set("--color-accent-600", accent);
    set("--color-accenthover", accent);
    set("--color-accentbg", withAlpha(accent, 0.5));
    set("--button-green-border-color", accent);
    set("--form-element-primary-color", accent);

    set("--link-color", theme.link);
    set("--border-color", withAlpha(border, 0.15));
    set("--modal-border-color", withAlpha(border, 0.12));
    set("--form-element-border-color", withAlpha(border, 0.15));
    set("--hover-bg-color", withAlpha(border, 0.08));
    set("--highlight-bg-color", withAlpha(theme.highlightBg ?? accent, 0.3));
    set("--keybinding-color", theme.foreground);
    set("--keybinding-bg-color", withAlpha(border, 0.1));
    set("--keybinding-border-color", withAlpha(border, 0.18));
    set("--scrollbar-thumb-color", withAlpha(border, 0.15));

    set("--modal-bg-color", theme.modalBg ?? bg);
    set("--error-color", theme.error);
    set("--warning-color", theme.warning);
    set("--success-color", theme.success);
}

// Resolve the active UI theme from the full config (settings key app:theme).
export function getActiveUITheme(fullConfig: FullConfigType): UIThemeType | null {
    const themes = fullConfig?.uithemes ?? {};
    const name = fullConfig?.settings?.["app:theme"] || DefaultUITheme;
    return themes[name] ?? themes[DefaultUITheme] ?? null;
}

// UIThemeUpdater: side-effect-only component, mount once near the app root.
export const UIThemeUpdater = () => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const override = useAtomValue(uiThemeOverrideAtom);
    const saved = getActiveUITheme(fullConfig);
    const theme = override ?? saved;
    useEffect(() => {
        applyUITheme(theme);
        // cache only the *saved* theme (not a live-edit preview) for a synchronous
        // re-apply on next launch (no FOUC)
        try {
            if (override == null && saved != null) {
                localStorage.setItem(CACHE_KEY, JSON.stringify(saved));
            }
        } catch {
            // ignore (private mode / quota)
        }
    }, [theme]);
    return null;
};

// Synchronously apply the cached theme at module load — runs before React mounts
// and before the config websocket arrives, so the window paints with the correct
// theme instead of the bundled default.
(function applyCachedThemeEarly() {
    try {
        const raw = localStorage.getItem(CACHE_KEY);
        if (raw) {
            applyUITheme(JSON.parse(raw) as UIThemeType);
        }
    } catch {
        // ignore
    }
})();
