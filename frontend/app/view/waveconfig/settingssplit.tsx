// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { memo, type ReactNode } from "react";
import { Panel, PanelGroup, PanelResizeHandle } from "react-resizable-panels";

interface SettingsSplitProps {
    // Persists the divider position in localStorage; must be unique per split.
    autoSaveId: string;
    left: ReactNode;
    right: ReactNode;
    defaultLeftPct?: number;
    minLeftPct?: number;
    minRightPct?: number;
}

// A horizontal, user-draggable split of a GUI editor (left) and its raw-JSON editor
// (right), used by the settings views. The divider position is remembered via
// autoSaveId so it survives reopening the block.
export const SettingsSplit = memo(
    ({ autoSaveId, left, right, defaultLeftPct = 40, minLeftPct = 25, minRightPct = 30 }: SettingsSplitProps) => {
        return (
            <PanelGroup direction="horizontal" autoSaveId={autoSaveId} className="h-full w-full">
                <Panel defaultSize={defaultLeftPct} minSize={minLeftPct} className="min-w-0">
                    {left}
                </Panel>
                {/* A wide, transparent grab strip with a thin centered hairline: at rest it
                    looks like the plain 1px divider, and only highlights while hovered/dragged. */}
                <PanelResizeHandle className="group relative w-[7px] shrink-0 cursor-col-resize">
                    <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-border transition-colors group-hover:bg-accent group-hover:w-0.5 group-data-[resize-handle-state=drag]:bg-accent group-data-[resize-handle-state=drag]:w-0.5" />
                </PanelResizeHandle>
                <Panel minSize={minRightPct} className="min-w-0">
                    {right}
                </Panel>
            </PanelGroup>
        );
    }
);
SettingsSplit.displayName = "SettingsSplit";
