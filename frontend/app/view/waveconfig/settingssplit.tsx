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
    ({ autoSaveId, left, right, defaultLeftPct = 45, minLeftPct = 25, minRightPct = 30 }: SettingsSplitProps) => {
        return (
            <PanelGroup direction="horizontal" autoSaveId={autoSaveId} className="h-full w-full">
                <Panel defaultSize={defaultLeftPct} minSize={minLeftPct} className="min-w-0">
                    {left}
                </Panel>
                <PanelResizeHandle className="w-1 shrink-0 bg-border transition-colors hover:bg-accent/60 cursor-col-resize data-[resize-handle-state=drag]:bg-accent" />
                <Panel minSize={minRightPct} className="min-w-0">
                    {right}
                </Panel>
            </PanelGroup>
        );
    }
);
SettingsSplit.displayName = "SettingsSplit";
