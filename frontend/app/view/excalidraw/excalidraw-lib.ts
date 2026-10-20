// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// @excalidraw/excalidraw is ~5 MB of JS. The block registry imports the model eagerly in every
// tab renderer, so the model must not import the library statically — it loads it here, once
// per renderer, when an Excalidraw block actually opens.
export type ExcalidrawLib = typeof import("@excalidraw/excalidraw");

let libPromise: Promise<ExcalidrawLib> = null;

export function loadExcalidrawLib(): Promise<ExcalidrawLib> {
    libPromise ??= import("@excalidraw/excalidraw");
    return libPromise;
}
