// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Input } from "@/app/element/input";
import { Toggle } from "@/app/element/toggle";
import { getSettingsKeyAtom } from "@/app/store/global";
import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { cn, fireAndForget } from "@/util/util";
import { useAtomValue } from "jotai";
import { memo, useEffect, useState } from "react";
import type { WaveConfigViewModel } from "./waveconfig-model";

const DefaultThresholdMs = 30000;

// A labeled text/number input bound to a single settings key, committing on blur or
// Enter (so we don't write the config on every keystroke). Optional transforms let
// a key stored in one unit (e.g. ms) be edited in another (e.g. seconds).
type TextRowProps = {
    label: string;
    settingKey: keyof SettingsType;
    placeholder?: string;
    isNumber?: boolean;
    toDisplay?: (v: any) => string;
    fromDisplay?: (s: string) => any;
    onCommit: (key: keyof SettingsType, value: any) => void;
};

const TextRow = memo(({ label, settingKey, placeholder, isNumber, toDisplay, fromDisplay, onCommit }: TextRowProps) => {
    const value = useAtomValue(getSettingsKeyAtom(settingKey));
    const display = toDisplay ? toDisplay(value) : value == null ? "" : String(value);
    const [draft, setDraft] = useState(display);
    useEffect(() => setDraft(display), [display]);

    const commit = () => {
        onCommit(settingKey, fromDisplay ? fromDisplay(draft) : draft);
    };

    return (
        <div className="flex items-center gap-2">
            <span className="text-sm w-36 shrink-0">{label}</span>
            <Input
                className="flex-1"
                value={draft}
                isNumber={isNumber}
                placeholder={placeholder}
                onChange={setDraft}
                onBlur={commit}
                onKeyDown={(e) => {
                    if (e.key === "Enter") {
                        commit();
                    }
                }}
            />
        </div>
    );
});
TextRow.displayName = "TextRow";

function formatSyncTime(ts: number): string {
    if (!ts) {
        return "never";
    }
    const secs = Math.round((Date.now() - ts) / 1000);
    if (secs < 60) {
        return `${secs}s ago`;
    }
    if (secs < 3600) {
        return `${Math.round(secs / 60)}m ago`;
    }
    return `${Math.round(secs / 3600)}h ago`;
}

// Visual editor for the General settings (settings.json). Each control writes its
// key immediately via SetConfigCommand, then refreshes the raw JSON shown alongside
// it so the file view reflects the change live.
export const GeneralSettingsView = memo(({ model }: { model: WaveConfigViewModel }) => {
    const notifyEnabled = useAtomValue(getSettingsKeyAtom("notify:commanddone")) ?? false;
    const thresholdMs = useAtomValue(getSettingsKeyAtom("notify:commanddonethresholdms")) ?? DefaultThresholdMs;
    const syncEnabled = useAtomValue(getSettingsKeyAtom("sync:enabled")) ?? false;
    const syncFolderPath = useAtomValue(getSettingsKeyAtom("sync:folderpath")) ?? "";

    const [syncMode, setSyncMode] = useState<"folder" | "webdav">(syncFolderPath ? "folder" : "webdav");

    const [secondsDraft, setSecondsDraft] = useState(String(Math.round(thresholdMs / 1000)));
    useEffect(() => {
        setSecondsDraft(String(Math.round(thresholdMs / 1000)));
    }, [thresholdMs]);

    const [syncStatus, setSyncStatus] = useState<SyncStatusData>(null);
    const [syncing, setSyncing] = useState(false);
    useEffect(() => {
        fireAndForget(async () => {
            const st = await RpcApi.SyncStatusCommand(TabRpcClient);
            setSyncStatus(st);
        });
    }, []);

    const setConfigKey = <K extends keyof SettingsType>(key: K, value: SettingsType[K]) => {
        fireAndForget(async () => {
            await RpcApi.SetConfigCommand(TabRpcClient, { [key]: value } as SettingsType);
            await model.refreshContentFromDisk();
        });
    };

    const commitThreshold = () => {
        const secs = parseInt(secondsDraft, 10);
        if (isNaN(secs) || secs <= 0) {
            setSecondsDraft(String(Math.round(thresholdMs / 1000)));
            return;
        }
        setConfigKey("notify:commanddonethresholdms", secs * 1000);
    };

    const onSyncNow = () => {
        setSyncing(true);
        fireAndForget(async () => {
            try {
                const st = await RpcApi.SyncNowCommand(TabRpcClient);
                setSyncStatus(st);
            } finally {
                setSyncing(false);
            }
        });
    };

    // Switching to WebDAV clears the folder path, since a non-empty path makes the
    // backend prefer the local-folder transport regardless of the WebDAV fields.
    const selectSyncMode = (mode: "folder" | "webdav") => {
        setSyncMode(mode);
        if (mode === "webdav" && syncFolderPath) {
            setConfigKey("sync:folderpath", "");
        }
    };

    return (
        <div className="flex flex-col gap-6 p-6">
            <section className="flex flex-col gap-2">
                <h2 className="text-base font-semibold">Notifications</h2>
                <Toggle
                    id="notify-commanddone"
                    checked={notifyEnabled}
                    onChange={(v) => setConfigKey("notify:commanddone", v)}
                    label="Notify when a command finishes"
                />
                <p className="text-xs text-muted-foreground ml-0.5">
                    Sends a system notification when a foreground command finishes while the Wave
                    window is unfocused. Click it to jump back to that tab.
                </p>
                <div
                    className={cn(
                        "flex items-center gap-2 ml-0.5 mt-1",
                        !notifyEnabled && "opacity-50 pointer-events-none"
                    )}
                >
                    <span className="text-sm">Minimum duration</span>
                    <Input
                        className="w-20"
                        value={secondsDraft}
                        isNumber={true}
                        disabled={!notifyEnabled}
                        onChange={setSecondsDraft}
                        onBlur={commitThreshold}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") {
                                commitThreshold();
                            }
                        }}
                    />
                    <span className="text-sm text-muted-foreground">seconds</span>
                </div>
            </section>

            <section className="flex flex-col gap-2">
                <h2 className="text-base font-semibold">Sync</h2>
                <Toggle
                    id="sync-enabled"
                    checked={syncEnabled}
                    onChange={(v) => setConfigKey("sync:enabled", v)}
                    label="Sync settings & workspaces between machines"
                />
                <p className="text-xs text-muted-foreground ml-0.5">
                    Syncs config, workspaces, tabs and layout across your machines.
                </p>
                <div
                    className={cn(
                        "flex flex-col gap-2 mt-1",
                        !syncEnabled && "opacity-50 pointer-events-none"
                    )}
                >
                    <div className="flex items-center gap-1 p-0.5 rounded bg-hoverbg w-fit">
                        <button
                            onClick={() => selectSyncMode("folder")}
                            className={cn(
                                "px-3 py-1 rounded text-sm transition-colors cursor-pointer",
                                syncMode === "folder"
                                    ? "bg-accent/80 text-primary"
                                    : "text-muted-foreground hover:text-primary"
                            )}
                        >
                            Cloud folder
                        </button>
                        <button
                            onClick={() => selectSyncMode("webdav")}
                            className={cn(
                                "px-3 py-1 rounded text-sm transition-colors cursor-pointer",
                                syncMode === "webdav"
                                    ? "bg-accent/80 text-primary"
                                    : "text-muted-foreground hover:text-primary"
                            )}
                        >
                            WebDAV
                        </button>
                    </div>

                    {syncMode === "folder" ? (
                        <>
                            <p className="text-xs text-muted-foreground ml-0.5">
                                Point Wave at a folder inside a Nextcloud / Dropbox / Drive
                                desktop-client sync root — that client moves the files between
                                machines, so no account or password is needed here.
                            </p>
                            <TextRow
                                label="Folder path"
                                settingKey="sync:folderpath"
                                placeholder="~/Nextcloud/waveterm-sync"
                                onCommit={setConfigKey}
                            />
                        </>
                    ) : (
                        <>
                            <p className="text-xs text-muted-foreground ml-0.5">
                                Talk to a WebDAV server (e.g. Nextcloud) directly over HTTPS — no
                                desktop client required. Set the app-password in Secrets as{" "}
                                <code>sync:webdavpassword</code>.
                            </p>
                            <TextRow
                                label="WebDAV URL"
                                settingKey="sync:webdavurl"
                                placeholder="https://host/remote.php/dav/files/<user>"
                                onCommit={setConfigKey}
                            />
                            <TextRow label="WebDAV user" settingKey="sync:webdavuser" onCommit={setConfigKey} />
                            <TextRow
                                label="Folder"
                                settingKey="sync:folder"
                                placeholder="waveterm-sync"
                                onCommit={setConfigKey}
                            />
                        </>
                    )}
                    <TextRow
                        label="Interval"
                        settingKey="sync:intervalms"
                        isNumber={true}
                        placeholder="60"
                        toDisplay={(v) => (v ? String(Math.round(v / 1000)) : "")}
                        fromDisplay={(s) => {
                            const secs = parseInt(s, 10);
                            return isNaN(secs) ? null : secs * 1000;
                        }}
                        onCommit={setConfigKey}
                    />
                    <div className="flex items-center gap-3 mt-1">
                        <button
                            onClick={onSyncNow}
                            disabled={syncing}
                            className={cn(
                                "px-3 py-1 rounded text-sm transition-colors",
                                syncing
                                    ? "border border-border text-muted-foreground opacity-50"
                                    : "bg-accent/80 text-primary hover:bg-accent cursor-pointer"
                            )}
                        >
                            {syncing ? "Syncing…" : "Sync now"}
                        </button>
                        {syncStatus != null && (
                            <span className="text-xs text-muted-foreground">
                                {syncStatus.lasterror ? (
                                    <span className="text-error">Error: {syncStatus.lasterror}</span>
                                ) : (
                                    `Last synced ${formatSyncTime(syncStatus.lastsyncts)}`
                                )}
                            </span>
                        )}
                    </div>
                </div>
            </section>
        </div>
    );
});

GeneralSettingsView.displayName = "GeneralSettingsView";
