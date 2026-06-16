// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { BlockNodeModel } from "@/app/block/blocktypes";
import { atoms } from "@/store/global";
import { RpcApi } from "@/store/wshclientapi";
import { TabRpcClient } from "@/store/wshrpcutil";
import { fireAndForget } from "@/util/util";
import { atom, useAtomValue } from "jotai";
import type { Atom } from "jotai";
import { useEffect, useMemo, useState } from "react";
import { applyUITheme } from "../../uitheme";
import "./themeeditor.scss";

const DefaultUITheme = "dracula";

// Editable color fields (UIThemeType keys) with display labels, in editor order.
const FIELDS: { key: keyof UIThemeType; label: string }[] = [
    { key: "background", label: "Background" },
    { key: "foreground", label: "Foreground" },
    { key: "accent", label: "Accent" },
    { key: "secondaryText", label: "Secondary Text" },
    { key: "greyText", label: "Muted Text" },
    { key: "border", label: "Border" },
    { key: "panelBg", label: "Panel Background" },
    { key: "modalBg", label: "Modal Background" },
    { key: "highlightBg", label: "Highlight" },
    { key: "link", label: "Link" },
    { key: "error", label: "Error" },
    { key: "warning", label: "Warning" },
    { key: "success", label: "Success" },
];

class ThemeEditorViewModel implements ViewModel {
    viewType: string;
    blockId: string;
    nodeModel: BlockNodeModel;
    viewIcon: Atom<string>;
    viewName: Atom<string>;
    noPadding: Atom<boolean>;

    constructor(initOpts: ViewModelInitType) {
        this.viewType = "themeeditor";
        this.blockId = initOpts.blockId;
        this.nodeModel = initOpts.nodeModel;
        this.viewIcon = atom("palette");
        this.viewName = atom("Themes");
        this.noPadding = atom(true);
    }

    get viewComponent(): ViewComponent {
        return ThemeEditorView;
    }
}

function setActiveTheme(name: string) {
    fireAndForget(() => RpcApi.SetConfigCommand(TabRpcClient, { "app:theme": name } as SettingsType));
}

function saveTheme(name: string, theme: UIThemeType) {
    fireAndForget(() =>
        RpcApi.SetUIThemeCommand(TabRpcClient, {
            themename: name,
            metamaptype: theme as any,
        })
    );
}

// A small color row: native swatch + hex text input. Editing fires onChange live.
function ColorRow({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
    const hex = /^#[0-9a-fA-F]{6}$/.test(value ?? "") ? value : "#000000";
    return (
        <div className="theme-color-row">
            <label className="theme-color-label">{label}</label>
            <input
                type="color"
                className="theme-color-swatch"
                value={hex}
                onChange={(e) => onChange(e.target.value)}
            />
            <input
                type="text"
                className="theme-color-hex"
                spellCheck={false}
                value={value ?? ""}
                onChange={(e) => onChange(e.target.value)}
            />
        </div>
    );
}

// Mini preview swatch strip for a theme card.
function ThemeSwatches({ theme }: { theme: UIThemeType }) {
    const keys: (keyof UIThemeType)[] = ["background", "accent", "foreground", "link", "error", "success"];
    return (
        <div className="theme-card-swatches">
            {keys.map((k) => (
                <span key={k} className="theme-card-swatch" style={{ backgroundColor: theme[k] as string }} />
            ))}
        </div>
    );
}

function ThemeEditorView() {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const uiThemes = (fullConfig?.uithemes ?? {}) as { [k: string]: UIThemeType };
    const activeName = (fullConfig?.settings?.["app:theme"] as string) || DefaultUITheme;

    const sortedKeys = useMemo(
        () =>
            Object.keys(uiThemes).sort(
                (a, b) => (uiThemes[a]["display:order"] ?? 0) - (uiThemes[b]["display:order"] ?? 0)
            ),
        [uiThemes]
    );

    // Draft = the theme currently being edited (always the active theme).
    const activeTheme = uiThemes[activeName];
    const [draft, setDraft] = useState<UIThemeType | null>(activeTheme ?? null);
    const [dirty, setDirty] = useState(false);

    // When the active theme (or its saved config) changes and we have no pending
    // edits, sync the draft from config so the editor follows the selection.
    useEffect(() => {
        if (!dirty) {
            setDraft(activeTheme ?? null);
        }
    }, [activeName, activeTheme]);

    const updateField = (key: keyof UIThemeType, value: string) => {
        const next = { ...(draft as UIThemeType), [key]: value };
        setDraft(next);
        setDirty(true);
        applyUITheme(next); // live preview
    };

    const revert = () => {
        setDraft(activeTheme ?? null);
        setDirty(false);
        applyUITheme(activeTheme ?? null);
    };

    const save = () => {
        if (draft == null) return;
        saveTheme(activeName, draft);
        setDirty(false);
    };

    const saveAsNew = () => {
        if (draft == null) return;
        const name = window.prompt("New theme name:", (draft["display:name"] ?? activeName) + " (custom)");
        if (!name) return;
        const id = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "custom";
        const newTheme: UIThemeType = {
            ...(draft as UIThemeType),
            "display:name": name,
            "display:order": 100,
        };
        saveTheme(id, newTheme);
        setActiveTheme(id);
        setDirty(false);
    };

    return (
        <div className="theme-editor">
            <div className="theme-gallery">
                <div className="theme-gallery-title">Themes</div>
                {sortedKeys.map((name) => {
                    const t = uiThemes[name];
                    return (
                        <div
                            key={name}
                            className={"theme-card" + (name === activeName ? " active" : "")}
                            style={{ backgroundColor: t.background, color: t.foreground }}
                            onClick={() => setActiveTheme(name)}
                        >
                            <div className="theme-card-name">{t["display:name"] ?? name}</div>
                            <ThemeSwatches theme={t} />
                            {name === activeName ? <i className="fa fa-sharp fa-solid fa-check theme-card-check" /> : null}
                        </div>
                    );
                })}
            </div>
            <div className="theme-editor-pane">
                <div className="theme-editor-header">
                    <div className="theme-editor-title">{draft?.["display:name"] ?? activeName}</div>
                    <div className="theme-editor-actions">
                        <button className="theme-btn" disabled={!dirty} onClick={revert}>
                            Reset
                        </button>
                        <button className="theme-btn" disabled={!dirty} onClick={save}>
                            Save
                        </button>
                        <button className="theme-btn primary" onClick={saveAsNew}>
                            Save as New…
                        </button>
                    </div>
                </div>
                {draft == null ? (
                    <div className="theme-editor-empty">No theme selected.</div>
                ) : (
                    <div className="theme-editor-fields">
                        {FIELDS.map((f) => (
                            <ColorRow
                                key={f.key}
                                label={f.label}
                                value={draft[f.key] as string}
                                onChange={(v) => updateField(f.key, v)}
                            />
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

export { ThemeEditorViewModel };
