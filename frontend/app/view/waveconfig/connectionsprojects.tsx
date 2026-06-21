// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { CodeEditor } from "@/app/view/codeeditor/codeeditor";
import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { atoms, createBlock, getApi } from "@/store/global";
import { base64ToString, cn, fireAndForget, stringToBase64 } from "@/util/util";
import { useAtomValue } from "jotai";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { SettingsSplit } from "./settingssplit";
import type { WaveConfigViewModel } from "./waveconfig-model";

function projectNameFromPath(p: string): string {
    if (p === "~" || p == null || p === "") {
        return "home";
    }
    const parts = p.replace(/[/\\]+$/, "").split(/[/\\]/);
    return parts[parts.length - 1] || p;
}

function uniqueProjectName(base: string, projects: { [key: string]: ProjectConfigType }): string {
    if (projects[base] == null) {
        return base;
    }
    let i = 2;
    while (projects[`${base} (${i})`] != null) {
        i++;
    }
    return `${base} (${i})`;
}

// A live JSON editor bound to a single config file (projects.json / connections.json).
// It re-reads from disk whenever the config changes elsewhere (e.g. the GUI buttons),
// except while the user is mid-edit in this pane, so the two stay in sync.
const JsonPane = memo(({ model, filePath }: { model: WaveConfigViewModel; filePath: string }) => {
    const [text, setText] = useState("");
    const [dirty, setDirty] = useState(false);
    const [saving, setSaving] = useState(false);
    const dirtyRef = useRef(false);
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const fullPath = `${model.configDir}/${filePath}`;

    const reload = useCallback(async () => {
        try {
            const fileData = await model.env.rpc.FileReadCommand(TabRpcClient, { info: { path: fullPath } });
            const content = fileData?.data64 ? base64ToString(fileData.data64) : "";
            setText(content.trim() === "" ? "{}" : content);
        } catch {
            setText("{}");
        }
    }, [fullPath, model]);

    useEffect(() => {
        if (!dirtyRef.current) {
            fireAndForget(reload);
        }
    }, [fullConfig, reload]);

    const onChange = (v: string) => {
        setText(v);
        dirtyRef.current = true;
        setDirty(true);
    };

    const save = () =>
        fireAndForget(async () => {
            setSaving(true);
            try {
                await model.env.rpc.FileWriteCommand(TabRpcClient, {
                    info: { path: fullPath },
                    data64: stringToBase64(text),
                });
                dirtyRef.current = false;
                setDirty(false);
            } finally {
                setSaving(false);
            }
        });

    return (
        <div className="flex flex-col h-full min-h-0 min-w-0 flex-1">
            <div className="flex items-center justify-end gap-2 px-2 py-1 border-b border-border">
                <span className="text-xs text-muted-foreground font-mono mr-auto">{filePath}</span>
                {dirty && <span className="text-xs text-warning">Unsaved</span>}
                <button
                    onClick={save}
                    disabled={!dirty || saving}
                    className={cn(
                        "px-2 py-0.5 rounded text-xs transition-colors",
                        !dirty || saving
                            ? "border border-border text-muted-foreground opacity-50"
                            : "bg-accent/80 text-primary hover:bg-accent cursor-pointer"
                    )}
                >
                    {saving ? "Saving…" : "Save"}
                </button>
            </div>
            <div className="flex-1 min-h-0">
                <CodeEditor
                    blockId={model.blockId}
                    text={text}
                    fileName={`WAVECONFIGPATH/${filePath}`}
                    language="json"
                    readonly={false}
                    onChange={onChange}
                />
            </div>
        </div>
    );
});
JsonPane.displayName = "JsonPane";

const ProjectsGui = memo(({ model }: { model: WaveConfigViewModel }) => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const projects = fullConfig?.projects ?? {};
    const entries = Object.entries(projects).sort(
        (a, b) => (a[1]?.["display:order"] ?? 0) - (b[1]?.["display:order"] ?? 0)
    );

    const add = () => {
        fireAndForget(async () => {
            const picked = await getApi().selectDirectory();
            if (!picked) {
                return;
            }
            const name = uniqueProjectName(projectNameFromPath(picked), projects);
            const orders = Object.values(projects).map((p) => p?.["display:order"] ?? 0);
            const nextOrder = orders.length ? Math.max(...orders) + 1 : 1;
            const meta: ProjectConfigType = { path: picked, "display:order": nextOrder };
            await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name, metamaptype: meta });
        });
    };

    const remove = (name: string) => fireAndForget(() => RpcApi.SetProjectsConfigCommand(TabRpcClient, { name, metamaptype: null }));

    const open = (project: ProjectConfigType) =>
        fireAndForget(() =>
            createBlock({ meta: { view: "preview", file: project.path, connection: project.connection || null } })
        );

    return (
        <div className="flex flex-col gap-2 p-3 h-full overflow-y-auto">
            <div className="flex items-center justify-between">
                <h2 className="text-base font-semibold">Projects</h2>
                <button
                    onClick={add}
                    className="px-2 py-1 rounded text-sm bg-accent/80 text-primary hover:bg-accent transition-colors cursor-pointer"
                >
                    <i className="fa-sharp fa-solid fa-plus mr-1" /> Add
                </button>
            </div>
            {entries.length === 0 ? (
                <div className="text-sm text-secondary italic">No projects yet. Click Add or use the ⭐ in a Files block.</div>
            ) : (
                <div className="flex flex-col gap-1">
                    {entries.map(([name, project]) => (
                        <div key={name} className="flex items-center gap-2 group">
                            <div className="flex flex-col min-w-0 flex-1">
                                <span className="text-sm truncate">{name}</span>
                                <span className="text-xs text-muted-foreground truncate font-mono">
                                    {project?.connection ? `${project.connection}: ` : ""}
                                    {project?.path}
                                </span>
                            </div>
                            <button
                                onClick={() => open(project)}
                                title="Open project"
                                className="px-2 py-1 rounded text-sm border border-border hover:bg-hoverbg transition-colors cursor-pointer shrink-0"
                            >
                                <i className="fa-sharp fa-solid fa-folder-open" />
                            </button>
                            <button
                                onClick={() => remove(name)}
                                title="Delete project"
                                className="px-2 py-1 rounded text-sm border border-border hover:bg-hoverbg hover:text-error transition-colors cursor-pointer shrink-0"
                            >
                                <i className="fa-sharp fa-solid fa-trash" />
                            </button>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
});
ProjectsGui.displayName = "ProjectsGui";

const ConnectionsGui = memo(() => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const connections = fullConfig?.connections ?? {};
    const names = Object.keys(connections).sort();

    return (
        <div className="flex flex-col gap-2 p-3 h-full overflow-y-auto">
            <h2 className="text-base font-semibold">Connections</h2>
            <p className="text-xs text-muted-foreground">SSH hosts and WSL distros. Edit details in the JSON →</p>
            {names.length === 0 ? (
                <div className="text-sm text-secondary italic">No saved connections.</div>
            ) : (
                <div className="flex flex-col gap-1">
                    {names.map((conn) => (
                        <div key={conn} className="flex items-center gap-2 text-sm">
                            <i className="fa-sharp fa-solid fa-arrow-right-arrow-left text-xs text-secondary" />
                            <span className="truncate">{conn}</span>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
});
ConnectionsGui.displayName = "ConnectionsGui";

export const ConnectionsProjectsView = memo(({ model }: { model: WaveConfigViewModel }) => {
    return (
        <div className="flex flex-col h-full min-h-0">
            <div className="flex-1 min-h-0 border-b border-border">
                <SettingsSplit
                    autoSaveId="waveconfig-projects-split"
                    left={<ProjectsGui model={model} />}
                    right={<JsonPane model={model} filePath="projects.json" />}
                />
            </div>
            <div className="flex-1 min-h-0">
                <SettingsSplit
                    autoSaveId="waveconfig-connections-split"
                    left={<ConnectionsGui />}
                    right={<JsonPane model={model} filePath="connections.json" />}
                />
            </div>
        </div>
    );
});
ConnectionsProjectsView.displayName = "ConnectionsProjectsView";
