// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { atoms, getApi, globalStore } from "@/store/global";
import { fireAndForget, isBlank, makeConnRoute } from "@/util/util";
import { atom, Getter } from "jotai";

// Shared "project" (folder bookmark) logic for every panel that can create one —
// the terminal star, the file-preview star and the connections panel "Add" button.
// Each of those sees the folder path in a different format (OSC 7 reports absolute
// paths, the file backend already tildifies, the native picker returns absolute),
// so all writes/compares go through canonical ~-form here to keep projects.json
// consistent and the star toggle working across panels.

// Remote $HOME per connection, filled lazily by primeConnHomeDir. A jotai atom so
// the star-state atoms recompute once the answer arrives.
const remoteHomeDirsAtom = atom<Record<string, string>>({});
const remoteHomeDirFetches = new Set<string>();

export function projectNameFromPath(p: string): string {
    if (p === "~" || isBlank(p)) {
        return "home";
    }
    const parts = p.replace(/[/\\]+$/, "").split(/[/\\]/);
    return parts[parts.length - 1] || p;
}

export function uniqueProjectName(base: string, projects: { [key: string]: ProjectConfigType }): string {
    if (projects[base] == null) {
        return base;
    }
    let i = 2;
    while (projects[`${base} (${i})`] != null) {
        i++;
    }
    return `${base} (${i})`;
}

let cachedLocalHomeDir: string = null;
export function localHomeDir(): string {
    if (cachedLocalHomeDir == null) {
        try {
            cachedLocalHomeDir = getApi().getHomeDir() || "";
        } catch {
            cachedLocalHomeDir = "";
        }
    }
    return cachedLocalHomeDir;
}

// Rewrite an absolute path under the given home dir into ~-form. Separators are
// normalized to "/" so Windows paths tildify the same way the backend
// (wavebase.ReplaceHomeDir) writes them.
export function tildifyPath(p: string, home: string): string {
    if (isBlank(p) || isBlank(home) || p.startsWith("~")) {
        return p;
    }
    const normPath = p.replace(/\\/g, "/").replace(/\/+$/, "");
    const normHome = home.replace(/\\/g, "/").replace(/\/+$/, "");
    if (normPath === normHome) {
        return "~";
    }
    if (normPath.startsWith(normHome + "/")) {
        return "~" + normPath.slice(normHome.length);
    }
    return p;
}

function homeDirForConn(conn: string): string {
    if (isBlank(conn) || conn === "local") {
        return localHomeDir();
    }
    return globalStore.get(remoteHomeDirsAtom)[conn] ?? "";
}

// Fetch (once per connection) the remote $HOME so remote project paths canonicalize
// into ~-form just like local ones. A failure clears the in-flight guard so a later
// call retries — e.g. after the connection comes up.
export function primeConnHomeDir(conn: string) {
    if (isBlank(conn) || conn === "local" || remoteHomeDirFetches.has(conn)) {
        return;
    }
    if (globalStore.get(remoteHomeDirsAtom)[conn] != null) {
        return;
    }
    remoteHomeDirFetches.add(conn);
    fireAndForget(async () => {
        try {
            const info = await RpcApi.RemoteGetInfoCommand(TabRpcClient, { route: makeConnRoute(conn) });
            if (!isBlank(info?.homedir)) {
                globalStore.set(remoteHomeDirsAtom, (prev) => ({ ...prev, [conn]: info.homedir }));
                return;
            }
        } catch {
            // connection down — retry on a later prime
        }
        remoteHomeDirFetches.delete(conn);
    });
}

function canonicalPathsEqual(a: string, b: string, home: string): boolean {
    if (isBlank(a) || isBlank(b)) {
        return false;
    }
    const norm = (p: string) => tildifyPath(p, home).replace(/\\/g, "/").replace(/\/+$/, "") || "/";
    return norm(a) === norm(b);
}

function findProjectEntry(
    projects: { [key: string]: ProjectConfigType },
    path: string,
    conn: string,
    home: string
): [string, ProjectConfigType] {
    const connKey = conn || "local";
    return Object.entries(projects).find(
        ([, p]) => p != null && (p.connection || "local") === connKey && canonicalPathsEqual(p.path, path, home)
    );
}

// For the star-button state atoms. Compares in canonical (~) space so a bookmark
// saved from another panel (absolute vs ~-form, or legacy absolute entries) still
// lights the star. Kicks off the remote-home fetch as a side effect — idempotent,
// and the atom recomputes via remoteHomeDirsAtom once the answer lands.
export function getIsPathBookmarked(get: Getter, path: string, conn: string): boolean {
    if (isBlank(path)) {
        return false;
    }
    const projects = get(atoms.fullConfigAtom)?.projects ?? {};
    let home: string;
    if (isBlank(conn) || conn === "local") {
        home = localHomeDir();
    } else {
        home = get(remoteHomeDirsAtom)[conn] ?? "";
        primeConnHomeDir(conn);
    }
    return findProjectEntry(projects, path, conn, home) != null;
}

export async function canonicalProjectPath(path: string, conn: string): Promise<string> {
    if (isBlank(path) || path.startsWith("~")) {
        return path;
    }
    if (isBlank(conn) || conn === "local") {
        return tildifyPath(path, localHomeDir());
    }
    let home = globalStore.get(remoteHomeDirsAtom)[conn];
    if (home == null) {
        try {
            const info = await RpcApi.RemoteGetInfoCommand(TabRpcClient, { route: makeConnRoute(conn) });
            if (!isBlank(info?.homedir)) {
                home = info.homedir;
                globalStore.set(remoteHomeDirsAtom, (prev) => ({ ...prev, [conn]: home }));
            }
        } catch {
            // connection down — keep the absolute path rather than guessing
        }
    }
    return isBlank(home) ? path : tildifyPath(path, home);
}

async function writeProjectEntry(path: string, conn: string, projects: { [key: string]: ProjectConfigType }) {
    const name = uniqueProjectName(projectNameFromPath(path), projects);
    const orders = Object.values(projects).map((p) => p?.["display:order"] ?? 0);
    const nextOrder = orders.length ? Math.max(...orders) + 1 : 1;
    const meta: ProjectConfigType = { path, "display:order": nextOrder };
    if (conn && conn !== "local") {
        meta.connection = conn;
    }
    await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name, metamaptype: meta });
}

// Bookmark (or un-bookmark) a folder as a "project", so it shows up in the
// connections panel.
export async function toggleProjectBookmark(rawPath: string, conn: string): Promise<void> {
    if (isBlank(rawPath)) {
        return;
    }
    const path = await canonicalProjectPath(rawPath, conn);
    const projects = globalStore.get(atoms.fullConfigAtom)?.projects ?? {};
    const existing = findProjectEntry(projects, path, conn, homeDirForConn(conn));
    if (existing) {
        await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name: existing[0], metamaptype: null });
        return;
    }
    await writeProjectEntry(path, conn, projects);
}

// Add-only variant for the connections panel "Add" button — picking an
// already-bookmarked folder should be a no-op, not a silent remove.
export async function addProjectBookmark(rawPath: string, conn: string): Promise<void> {
    if (isBlank(rawPath)) {
        return;
    }
    const path = await canonicalProjectPath(rawPath, conn);
    const projects = globalStore.get(atoms.fullConfigAtom)?.projects ?? {};
    if (findProjectEntry(projects, path, conn, homeDirForConn(conn)) != null) {
        return;
    }
    await writeProjectEntry(path, conn, projects);
}
