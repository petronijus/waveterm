// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { getBlockUploadStateAtom } from "@/store/global";
import { useAtomValue } from "jotai";
import * as React from "react";

const UploadOverlayComp = ({ blockId }: { blockId: string }) => {
    const uploadState = useAtomValue(getBlockUploadStateAtom(blockId));
    if (!uploadState?.active) {
        return null;
    }
    return (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-black/40">
            <div className="flex items-center gap-2 rounded bg-black/70 px-3 py-2 text-white shadow-lg">
                <i className="fa-solid fa-spinner fa-spin text-accent" />
                <span className="text-sm">
                    Uploading{uploadState.fileName ? ` ${uploadState.fileName}` : ""}…
                </span>
            </div>
        </div>
    );
};

export const UploadOverlay = React.memo(UploadOverlayComp);
UploadOverlay.displayName = "UploadOverlay";
