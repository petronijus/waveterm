// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Input } from "@/app/element/input";
import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { atoms, createBlock } from "@/store/global";
import { cn, fireAndForget } from "@/util/util";
import { useAtomValue } from "jotai";
import { memo, useEffect, useState } from "react";
import type { WaveConfigViewModel } from "./waveconfig-model";

async function setProject(name: string, data: ProjectConfigType | null) {
    await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name, metamaptype: data });
}

// Renaming changes the projects.json map key, so a rename is a delete of the old
// key followed by a write of the new one.
async function saveProject(oldName: string, newName: string, data: ProjectConfigType) {
    const trimmed = newName.trim();
    if (trimmed === "") {
        return;
    }
    if (oldName !== trimmed) {
        await setProject(oldName, null);
    }
    await setProject(trimmed, data);
}

type ProjectRowProps = {
    name: string;
    project: ProjectConfigType;
};

const ProjectRow = memo(({ name, project }: ProjectRowProps) => {
    const [nameDraft, setNameDraft] = useState(name);
    const [pathDraft, setPathDraft] = useState(project.path ?? "");
    const [connDraft, setConnDraft] = useState(project.connection ?? "");
    useEffect(() => setNameDraft(name), [name]);
    useEffect(() => setPathDraft(project.path ?? ""), [project.path]);
    useEffect(() => setConnDraft(project.connection ?? ""), [project.connection]);

    const commit = () => {
        const data: ProjectConfigType = { ...project, path: pathDraft.trim() };
        const conn = connDraft.trim();
        if (conn === "" || conn === "local") {
            delete data.connection;
        } else {
            data.connection = conn;
        }
        fireAndForget(() => saveProject(name, nameDraft, data));
    };

    const open = () => {
        fireAndForget(() =>
            createBlock({
                meta: {
                    view: "preview",
                    file: project.path,
                    connection: project.connection || null,
                },
            })
        );
    };

    const remove = () => fireAndForget(() => setProject(name, null));

    return (
        <div className="flex items-center gap-2">
            <Input className="w-40 shrink-0" value={nameDraft} onChange={setNameDraft} onBlur={commit} />
            <Input className="flex-1" value={pathDraft} onChange={setPathDraft} onBlur={commit} placeholder="/path/to/folder" />
            <Input
                className="w-28 shrink-0"
                value={connDraft}
                onChange={setConnDraft}
                onBlur={commit}
                placeholder="local"
            />
            <button
                onClick={open}
                title="Open project"
                className="px-2 py-1 rounded text-sm border border-border hover:bg-hoverbg transition-colors cursor-pointer shrink-0"
            >
                <i className="fa-sharp fa-solid fa-folder-open" />
            </button>
            <button
                onClick={remove}
                title="Remove project"
                className="px-2 py-1 rounded text-sm border border-border hover:bg-hoverbg hover:text-error transition-colors cursor-pointer shrink-0"
            >
                <i className="fa-sharp fa-solid fa-trash" />
            </button>
        </div>
    );
});
ProjectRow.displayName = "ProjectRow";

export const ConnectionsProjectsView = memo(({ model }: { model: WaveConfigViewModel }) => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const projects = fullConfig?.projects ?? {};
    const connections = fullConfig?.connections ?? {};

    const projectEntries = Object.entries(projects).sort(
        (a, b) => (a[1]?.["display:order"] ?? 0) - (b[1]?.["display:order"] ?? 0)
    );
    const connectionNames = Object.keys(connections).sort();

    return (
        <div className="flex flex-col gap-6 p-4 overflow-y-auto h-full">
            <section className="flex flex-col gap-2">
                <h2 className="text-base font-semibold">Projects</h2>
                <p className="text-xs text-muted-foreground">
                    Bookmarked folders. Add one with the <i className="fa-sharp fa-solid fa-star" /> button in a Files
                    block; pick it from the connection dropdown to jump there. Each project remembers its connection so
                    it works across machines.
                </p>
                {projectEntries.length === 0 ? (
                    <div className="text-sm text-secondary italic">No projects yet.</div>
                ) : (
                    <div className="flex flex-col gap-2">
                        {projectEntries.map(([name, project]) => (
                            <ProjectRow key={name} name={name} project={project} />
                        ))}
                    </div>
                )}
            </section>

            <section className="flex flex-col gap-2">
                <h2 className="text-base font-semibold">Connections</h2>
                <p className="text-xs text-muted-foreground">
                    SSH hosts and WSL distros. Edit them in the JSON tab (<code>connections.json</code>).
                </p>
                {connectionNames.length === 0 ? (
                    <div className="text-sm text-secondary italic">No saved connections.</div>
                ) : (
                    <div className="flex flex-col gap-1">
                        {connectionNames.map((conn) => (
                            <div key={conn} className={cn("flex items-center gap-2 text-sm")}>
                                <i className="fa-sharp fa-solid fa-arrow-right-arrow-left text-xs text-secondary" />
                                <span>{conn}</span>
                            </div>
                        ))}
                    </div>
                )}
            </section>
        </div>
    );
});
ConnectionsProjectsView.displayName = "ConnectionsProjectsView";
