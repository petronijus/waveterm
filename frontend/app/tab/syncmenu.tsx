// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import {
    ExpandableMenu,
    ExpandableMenuItem,
    ExpandableMenuItemGroupTitle,
    ExpandableMenuItemLeftElement,
    ExpandableMenuItemRightElement,
} from "@/element/expandablemenu";
import { Popover, PopoverButton, PopoverContent } from "@/element/popover";
import { cn, fireAndForget } from "@/util/util";
import { memo, useCallback, useState } from "react";
import "./syncmenu.scss";

type SyncMenuProps = {
    tabId: string;
};

const SyncMenu = memo(({ tabId }: SyncMenuProps) => {
    const [layouts, setLayouts] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);
    const [naming, setNaming] = useState(false);
    const [newName, setNewName] = useState("");
    const [status, setStatus] = useState<{ msg: string; error: boolean }>(null);

    const refresh = useCallback(() => {
        fireAndForget(async () => {
            const list = await RpcApi.ListLayoutsCommand(TabRpcClient);
            setLayouts(list ?? []);
        });
    }, []);

    const run = useCallback(
        (fn: () => Promise<void>, okMsg?: string) => {
            if (busy) {
                return;
            }
            setBusy(true);
            setStatus(null);
            fireAndForget(async () => {
                try {
                    await fn();
                    if (okMsg) {
                        setStatus({ msg: okMsg, error: false });
                    }
                } catch (e) {
                    setStatus({ msg: e?.message ?? String(e), error: true });
                } finally {
                    setBusy(false);
                }
            });
        },
        [busy]
    );

    const saveSettings = () => run(() => RpcApi.SaveSettingsCommand(TabRpcClient), "Settings saved");
    const loadSettings = () => run(() => RpcApi.LoadSettingsCommand(TabRpcClient), "Settings loaded");
    const loadLayout = (name: string) => run(() => RpcApi.LoadLayoutCommand(TabRpcClient, { tabid: tabId, name }));
    const resaveLayout = (name: string) =>
        run(() => RpcApi.SaveLayoutCommand(TabRpcClient, { tabid: tabId, name }), `Layout "${name}" re-saved`);
    const deleteLayout = (name: string) =>
        run(async () => {
            await RpcApi.DeleteLayoutCommand(TabRpcClient, name);
            const list = await RpcApi.ListLayoutsCommand(TabRpcClient);
            setLayouts(list ?? []);
        });

    const commitName = () => {
        const name = newName.trim();
        if (name === "") {
            return;
        }
        run(async () => {
            await RpcApi.SaveLayoutCommand(TabRpcClient, { tabid: tabId, name });
            const list = await RpcApi.ListLayoutsCommand(TabRpcClient);
            setLayouts(list ?? []);
            setNewName("");
            setNaming(false);
        }, `Layout "${name}" saved`);
    };

    return (
        <Popover className="sync-menu-popover" placement="bottom-end" onDismiss={() => setNaming(false)}>
            <PopoverButton className="sync-button" as="div" title="Sync settings & layouts" onClick={refresh}>
                <i className={cn("fa", busy ? "fa-spinner fa-spin" : "fa-cloud")} />
            </PopoverButton>
            <PopoverContent className="sync-menu-content">
                <ExpandableMenu noIndent>
                    <ExpandableMenuItemGroupTitle>Settings</ExpandableMenuItemGroupTitle>
                    <ExpandableMenuItem onClick={saveSettings}>
                        <ExpandableMenuItemLeftElement>
                            <i className="fa fa-cloud-arrow-up" />
                        </ExpandableMenuItemLeftElement>
                        <div className="content">Save settings</div>
                    </ExpandableMenuItem>
                    <ExpandableMenuItem onClick={loadSettings}>
                        <ExpandableMenuItemLeftElement>
                            <i className="fa fa-cloud-arrow-down" />
                        </ExpandableMenuItemLeftElement>
                        <div className="content">Load settings</div>
                    </ExpandableMenuItem>

                    <ExpandableMenuItemGroupTitle>Layouts</ExpandableMenuItemGroupTitle>
                    {layouts.map((name) => (
                        <ExpandableMenuItem key={name} onClick={() => loadLayout(name)}>
                            <ExpandableMenuItemLeftElement>
                                <i className="fa fa-table-cells-large" />
                            </ExpandableMenuItemLeftElement>
                            <div className="content">{name}</div>
                            <ExpandableMenuItemRightElement>
                                <div className="flex items-center gap-2">
                                    <i
                                        className="fa fa-arrows-rotate text-secondary hover:text-accent cursor-pointer"
                                        title="Re-save this layout (overwrite with the current layout)"
                                        onClick={(e) => {
                                            e.stopPropagation();
                                            resaveLayout(name);
                                        }}
                                    />
                                    <i
                                        className="fa fa-trash text-secondary hover:text-error cursor-pointer"
                                        title="Delete layout"
                                        onClick={(e) => {
                                            e.stopPropagation();
                                            deleteLayout(name);
                                        }}
                                    />
                                </div>
                            </ExpandableMenuItemRightElement>
                        </ExpandableMenuItem>
                    ))}
                    {layouts.length === 0 && !naming && <div className="sync-menu-empty">No saved layouts</div>}

                    {naming ? (
                        <div className="sync-menu-name-row">
                            <input
                                className="sync-menu-name-input"
                                autoFocus
                                placeholder="Layout name…"
                                value={newName}
                                onChange={(e) => setNewName(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        commitName();
                                    } else if (e.key === "Escape") {
                                        setNaming(false);
                                        setNewName("");
                                    }
                                }}
                            />
                        </div>
                    ) : (
                        <ExpandableMenuItem onClick={() => setNaming(true)}>
                            <ExpandableMenuItemLeftElement>
                                <i className="fa fa-plus" />
                            </ExpandableMenuItemLeftElement>
                            <div className="content">Save layout…</div>
                        </ExpandableMenuItem>
                    )}
                </ExpandableMenu>
                {status && <div className={cn("sync-menu-status", status.error && "is-error")}>{status.msg}</div>}
            </PopoverContent>
        </Popover>
    );
});
SyncMenu.displayName = "SyncMenu";

export { SyncMenu };
