// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type * as MonacoReact from "@/app/monaco/monaco-react";
import { lazy, Suspense, type ComponentProps } from "react";

// Monaco is ~8 MB of JS plus its language contributions. Every tab is its own renderer, so a
// static import made each one parse and keep it on the heap even when it only shows
// terminals. These wrappers keep the same names and props as monaco-react and load it on the
// first editor a tab actually opens.
const MonacoCodeEditorImpl = lazy(() =>
    import("@/app/monaco/monaco-react").then((m) => ({ default: m.MonacoCodeEditor }))
);
const MonacoDiffViewerImpl = lazy(() =>
    import("@/app/monaco/monaco-react").then((m) => ({ default: m.MonacoDiffViewer }))
);

function EditorLoadingFallback() {
    return <div className="h-full w-full" />;
}

export function MonacoCodeEditor(props: ComponentProps<typeof MonacoReact.MonacoCodeEditor>) {
    return (
        <Suspense fallback={<EditorLoadingFallback />}>
            <MonacoCodeEditorImpl {...props} />
        </Suspense>
    );
}

export function MonacoDiffViewer(props: ComponentProps<typeof MonacoReact.MonacoDiffViewer>) {
    return (
        <Suspense fallback={<EditorLoadingFallback />}>
            <MonacoDiffViewerImpl {...props} />
        </Suspense>
    );
}
