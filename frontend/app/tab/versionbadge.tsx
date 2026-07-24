// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Tooltip } from "@/app/element/tooltip";
import { atoms } from "@/app/store/global";
import { modalsModel } from "@/app/store/modalmodel";
import { useAtomValue } from "jotai";
import { memo } from "react";

const VersionBadge = memo(() => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const version = fullConfig?.version ?? "";
    if (!version) {
        return null;
    }
    const pjMatch = version.match(/-pj\.(\d+)$/);
    const label = pjMatch ? `pj.${pjMatch[1]}` : `v${version}`;
    return (
        <Tooltip
            content={`Wave ${version}`}
            placement="bottom"
            hideOnClick
            divClassName="flex h-[22px] px-2 mb-1 items-center rounded-md box-border cursor-pointer hover:bg-hoverbg transition-colors text-[11px] text-secondary hover:text-primary select-none whitespace-nowrap"
            divStyle={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
            divOnClick={() => modalsModel.pushModal("AboutModal")}
        >
            {label}
        </Tooltip>
    );
});
VersionBadge.displayName = "VersionBadge";

export { VersionBadge };
