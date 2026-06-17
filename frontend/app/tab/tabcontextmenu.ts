// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { getOrefMetaKeyAtom, globalStore, recordTEvent } from "@/app/store/global";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { fireAndForget } from "@/util/util";
import { makeORef } from "../store/wos";
import type { TabEnv } from "./tab";

export function buildTabBarContextMenu(env: TabEnv): ContextMenuItem[] {
    const currentTabBar = globalStore.get(env.getSettingsKeyAtom("app:tabbar")) ?? "top";
    const tabBarSubmenu: ContextMenuItem[] = [
        {
            label: "Top",
            type: "checkbox",
            checked: currentTabBar === "top",
            click: () => fireAndForget(() => env.rpc.SetConfigCommand(TabRpcClient, { "app:tabbar": "top" })),
        },
        {
            label: "Left",
            type: "checkbox",
            checked: currentTabBar === "left",
            click: () => fireAndForget(() => env.rpc.SetConfigCommand(TabRpcClient, { "app:tabbar": "left" })),
        },
    ];
    return [{ label: "Tab Bar Position", type: "submenu", submenu: tabBarSubmenu }];
}

export function buildTabContextMenu(
    id: string,
    renameRef: React.RefObject<(() => void) | null>,
    onClose: (event: React.MouseEvent<HTMLButtonElement, MouseEvent> | null) => void,
    env: TabEnv
): ContextMenuItem[] {
    const menu: ContextMenuItem[] = [];
    menu.push(
        { label: "Rename Tab", click: () => renameRef.current?.() },
        {
            label: "Copy TabId",
            click: () => fireAndForget(() => navigator.clipboard.writeText(id)),
        },
        { type: "separator" }
    );
    const tabORef = makeORef("tab", id);
    const fullConfig = globalStore.get(env.atoms.fullConfigAtom);
    const currentFlag = globalStore.get(getOrefMetaKeyAtom(tabORef, "tab:flag")) ?? null;
    const flags = fullConfig?.flags ?? {};
    const flagKeys = Object.keys(flags)
        .filter((k) => flags[k] != null)
        .sort((a, b) => (flags[a]["display:order"] ?? 0) - (flags[b]["display:order"] ?? 0));
    const flagSubmenu: ContextMenuItem[] = [
        {
            label: "None",
            type: "checkbox",
            checked: currentFlag == null,
            click: () =>
                fireAndForget(() =>
                    // clear both the flag ref and the legacy literal color
                    env.rpc.SetMetaCommand(TabRpcClient, { oref: tabORef, meta: { "tab:flag": null, "tab:flagcolor": null } })
                ),
        },
        ...flagKeys.map((k) => ({
            label: flags[k].label || k,
            type: "checkbox" as const,
            checked: currentFlag === k,
            click: () =>
                fireAndForget(() =>
                    // store the flag id (so color edits propagate live) + a denormalized color
                    env.rpc.SetMetaCommand(TabRpcClient, {
                        oref: tabORef,
                        meta: { "tab:flag": k, "tab:flagcolor": flags[k].color },
                    })
                ),
        })),
    ];
    menu.push({ label: "Flag Tab", type: "submenu", submenu: flagSubmenu }, { type: "separator" });
    const backgrounds = fullConfig?.backgrounds ?? {};
    const bgKeys = Object.keys(backgrounds).filter((k) => backgrounds[k] != null);
    bgKeys.sort((a, b) => {
        const aOrder = backgrounds[a]["display:order"] ?? 0;
        const bOrder = backgrounds[b]["display:order"] ?? 0;
        return aOrder - bOrder;
    });
    if (bgKeys.length > 0) {
        const submenu: ContextMenuItem[] = [];
        const oref = makeORef("tab", id);
        submenu.push({
            label: "Default",
            click: () =>
                fireAndForget(async () => {
                    await env.rpc.SetMetaCommand(TabRpcClient, {
                        oref,
                        meta: { "bg:*": true, "tab:background": null },
                    });
                    env.rpc.ActivityCommand(TabRpcClient, { settabtheme: 1 }, { noresponse: true });
                    recordTEvent("action:settabtheme");
                }),
        });
        for (const bgKey of bgKeys) {
            const bg = backgrounds[bgKey];
            submenu.push({
                label: bg["display:name"] ?? bgKey,
                click: () =>
                    fireAndForget(async () => {
                        await env.rpc.SetMetaCommand(TabRpcClient, {
                            oref,
                            meta: { "bg:*": true, "tab:background": bgKey },
                        });
                        env.rpc.ActivityCommand(TabRpcClient, { settabtheme: 1 }, { noresponse: true });
                        recordTEvent("action:settabtheme");
                    }),
            });
        }
        menu.push({ label: "Backgrounds", type: "submenu", submenu }, { type: "separator" });
    }
    menu.push(...buildTabBarContextMenu(env), { type: "separator" });
    menu.push({ label: "Close Tab", click: () => onClose(null) });
    return menu;
}
