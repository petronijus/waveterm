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

// Visual editor for the General settings (settings.json). Each control writes its
// key immediately via SetConfigCommand, then refreshes the raw JSON shown alongside
// it so the file view reflects the change live.
export const GeneralSettingsView = memo(({ model }: { model: WaveConfigViewModel }) => {
    const notifyEnabled = useAtomValue(getSettingsKeyAtom("notify:commanddone")) ?? false;
    const thresholdMs = useAtomValue(getSettingsKeyAtom("notify:commanddonethresholdms")) ?? DefaultThresholdMs;

    const setConfigKey = <K extends keyof SettingsType>(key: K, value: SettingsType[K]) => {
        fireAndForget(async () => {
            await RpcApi.SetConfigCommand(TabRpcClient, { [key]: value } as SettingsType);
            await model.refreshContentFromDisk();
        });
    };

    const [secondsDraft, setSecondsDraft] = useState(String(Math.round(thresholdMs / 1000)));
    useEffect(() => {
        setSecondsDraft(String(Math.round(thresholdMs / 1000)));
    }, [thresholdMs]);

    const commitThreshold = () => {
        const secs = parseInt(secondsDraft, 10);
        if (isNaN(secs) || secs <= 0) {
            setSecondsDraft(String(Math.round(thresholdMs / 1000))); // revert to last good value
            return;
        }
        setConfigKey("notify:commanddonethresholdms", secs * 1000);
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
        </div>
    );
});

GeneralSettingsView.displayName = "GeneralSettingsView";
