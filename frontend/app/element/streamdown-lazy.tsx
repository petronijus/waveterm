// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type * as StreamdownModule from "@/app/element/streamdown";
import { cn } from "@/util/util";
import { lazy, Suspense, type ComponentProps } from "react";

// streamdown pulls in shiki, mermaid, katex and cytoscape (~15 MB of JS). Every tab is its own
// renderer, so a static import made each one parse and keep all of it even when no markdown is
// ever rendered there. Same name and props as the real component; loads on first use.
const WaveStreamdownImpl = lazy(() => import("@/app/element/streamdown").then((m) => ({ default: m.WaveStreamdown })));

type WaveStreamdownProps = ComponentProps<typeof StreamdownModule.WaveStreamdown>;

export function WaveStreamdown(props: WaveStreamdownProps) {
    // the raw text keeps the message readable for the moment the renderer takes to load
    const fallback = <div className={cn("whitespace-pre-wrap text-secondary", props.className)}>{props.text}</div>;
    return (
        <Suspense fallback={fallback}>
            <WaveStreamdownImpl {...props} />
        </Suspense>
    );
}
