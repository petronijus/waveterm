// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { BlockNodeModel } from "@/app/block/blocktypes";
import { atoms } from "@/store/global";
import { RpcApi } from "@/store/wshclientapi";
import { TabRpcClient } from "@/store/wshrpcutil";
import { fireAndForget } from "@/util/util";
import { atom, useAtomValue, useSetAtom } from "jotai";
import type { Atom } from "jotai";
import { useEffect, useMemo, useState } from "react";
import { uiThemeOverrideAtom } from "../../uitheme";
import "./themeeditor.scss";

const DefaultUITheme = "dracula";

// Editable color fields grouped into sections: text colors vs element/surface
// colors vs accent/status, so it's clear what each color drives.
type FieldDef = { key: keyof UIThemeType; label: string };
const FIELD_GROUPS: { title: string; fields: FieldDef[] }[] = [
    {
        title: "Text",
        fields: [
            { key: "foreground", label: "Text" },
            { key: "secondaryText", label: "Secondary Text" },
            { key: "greyText", label: "Muted Text" },
            { key: "link", label: "Link" },
        ],
    },
    {
        title: "Surfaces",
        fields: [
            { key: "background", label: "Background" },
            { key: "border", label: "Border" },
            { key: "modalBg", label: "Modal Background" },
        ],
    },
    {
        title: "Accent & Status",
        fields: [
            { key: "accent", label: "Accent" },
            { key: "highlightBg", label: "Highlight / Selection" },
            { key: "error", label: "Error" },
            { key: "warning", label: "Warning" },
            { key: "success", label: "Success" },
        ],
    },
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

// Self-contained editor UI; ignores props so it can render both as a block view
// (themeeditor) and as the "Themes" tab inside Wave Config settings.
export function ThemeEditorView() {
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
    const [naming, setNaming] = useState(false);
    const [newName, setNewName] = useState("");

    // When the active theme (or its saved config) changes and we have no pending
    // edits, sync the draft from config so the editor follows the selection.
    useEffect(() => {
        if (!dirty) {
            setDraft(activeTheme ?? null);
        }
    }, [activeName, activeTheme]);

    // Live preview drives the override atom: UIThemeUpdater (CSS vars) and the
    // terminal (computeTheme) both read it, so edits preview everywhere instantly.
    const setOverride = useSetAtom(uiThemeOverrideAtom);

    // Clear any unsaved live-preview override when the panel is closed, so a
    // half-finished theme doesn't stick on the app.
    useEffect(() => {
        return () => {
            setOverride(null);
        };
    }, []);

    const updateField = (key: keyof UIThemeType, value: string) => {
        const next = { ...(draft as UIThemeType), [key]: value };
        setDraft(next);
        setDirty(true);
        setOverride(next); // live preview (UI + terminal)
    };

    const revert = () => {
        setDraft(activeTheme ?? null);
        setDirty(false);
        setOverride(null);
    };

    const save = () => {
        if (draft == null) return;
        saveTheme(activeName, draft);
        setDirty(false);
        setOverride(null);
    };

    const selectTheme = (name: string) => {
        setOverride(null);
        setDirty(false);
        setActiveTheme(name);
    };

    const startNaming = () => {
        setNewName((draft?.["display:name"] ?? activeName) + " Custom");
        setNaming(true);
    };

    const confirmSaveAsNew = () => {
        const name = newName.trim();
        if (draft == null || name === "") return;
        const id = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "custom";
        const newTheme: UIThemeType = {
            ...(draft as UIThemeType),
            "display:name": name,
            "display:order": 100,
        };
        saveTheme(id, newTheme);
        setActiveTheme(id);
        setDirty(false);
        setOverride(null);
        setNaming(false);
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
                            onClick={() => selectTheme(name)}
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
                        {naming ? (
                            <>
                                <input
                                    type="text"
                                    className="theme-name-input"
                                    autoFocus
                                    placeholder="Theme name"
                                    value={newName}
                                    onChange={(e) => setNewName(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") confirmSaveAsNew();
                                        if (e.key === "Escape") setNaming(false);
                                    }}
                                />
                                <button className="theme-btn primary" onClick={confirmSaveAsNew}>
                                    Create
                                </button>
                                <button className="theme-btn" onClick={() => setNaming(false)}>
                                    Cancel
                                </button>
                            </>
                        ) : (
                            <>
                                <button className="theme-btn" disabled={!dirty} onClick={revert}>
                                    Reset
                                </button>
                                <button className="theme-btn" disabled={!dirty} onClick={save}>
                                    Save
                                </button>
                                <button className="theme-btn primary" onClick={startNaming}>
                                    Save as New…
                                </button>
                            </>
                        )}
                    </div>
                </div>
                {draft == null ? (
                    <div className="theme-editor-empty">No theme selected.</div>
                ) : (
                    <div className="theme-editor-groups">
                        {FIELD_GROUPS.map((group) => (
                            <div key={group.title} className="theme-editor-group">
                                <div className="theme-editor-group-title">{group.title}</div>
                                <div className="theme-editor-fields">
                                    {group.fields.map((f) => (
                                        <ColorRow
                                            key={f.key}
                                            label={f.label}
                                            value={draft[f.key] as string}
                                            onChange={(v) => updateField(f.key, v)}
                                        />
                                    ))}
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

export { ThemeEditorViewModel };
