// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Markdown } from "@/element/markdown";
import { getBlockUserInputAtom } from "@/store/global";
import * as keyutil from "@/util/keyutil";
import { fireAndForget } from "@/util/util";
import { useAtom } from "jotai";
import * as React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { UserInputService } from "../store/services";

// A non-blocking, per-block password / confirm prompt. Unlike the global
// UserInputModal it renders inside the block whose connection raised the prompt,
// so the rest of the UI (other tabs/blocks) stays interactive.
const UserInputPromptOverlayComp = ({ blockId }: { blockId: string }) => {
    const promptAtom = useMemo(() => getBlockUserInputAtom(blockId), [blockId]);
    const [req, setReq] = useAtom(promptAtom);
    const [responseText, setResponseText] = useState("");
    const [countdown, setCountdown] = useState(0);
    const checkboxRef = useRef<HTMLInputElement>(null);

    const requestId = req?.requestid;
    useEffect(() => {
        setResponseText("");
        setCountdown(req?.timeoutms ? Math.floor(req.timeoutms / 1000) : 0);
    }, [requestId]);

    const sendResponse = useCallback(
        (resp: Partial<UserInputResponse>) => {
            if (req == null) {
                return;
            }
            fireAndForget(() =>
                UserInputService.SendUserInputResponse({
                    type: "userinputresp",
                    requestid: req.requestid,
                    checkboxstat: checkboxRef?.current?.checked ?? false,
                    ...resp,
                })
            );
            setReq(null);
        },
        [req, setReq]
    );

    const handleSubmit = useCallback(() => {
        if (req == null) {
            return;
        }
        if (req.responsetype === "confirm") {
            sendResponse({ confirm: true });
        } else {
            sendResponse({ text: responseText });
        }
    }, [req, responseText, sendResponse]);

    const handleCancel = useCallback(() => {
        if (req == null) {
            return;
        }
        if (req.responsetype === "confirm") {
            sendResponse({ confirm: false });
        } else {
            sendResponse({ errormsg: "Canceled by the user" });
        }
    }, [req, sendResponse]);

    const handleKeyDown = useCallback(
        (waveEvent: WaveKeyboardEvent): boolean => {
            if (keyutil.checkKeyPressed(waveEvent, "Escape")) {
                handleCancel();
                return true;
            }
            if (keyutil.checkKeyPressed(waveEvent, "Enter")) {
                handleSubmit();
                return true;
            }
            return false;
        },
        [handleCancel, handleSubmit]
    );

    useEffect(() => {
        if (req == null) {
            return undefined;
        }
        let timeout: ReturnType<typeof setTimeout>;
        if (countdown <= 0) {
            timeout = setTimeout(() => handleCancel(), 300);
        } else {
            timeout = setTimeout(() => setCountdown(countdown - 1), 1000);
        }
        return () => clearTimeout(timeout);
    }, [countdown, req, handleCancel]);

    const queryText = useMemo(() => {
        if (req == null) {
            return null;
        }
        if (req.markdown) {
            return <Markdown text={req.querytext} />;
        }
        return <span>{req.querytext}</span>;
    }, [req?.markdown, req?.querytext]);

    if (req == null) {
        return null;
    }

    return (
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/40 p-3">
            <div className="w-full max-w-[500px] rounded-lg border border-border bg-panel p-5 shadow-2xl">
                <div className="mb-2.5 font-bold text-primary">
                    {req.title} ({countdown}s)
                </div>
                <div className="mb-4 flex flex-col justify-between gap-4 font-mono text-primary">
                    {queryText}
                    {req.responsetype !== "confirm" && (
                        <input
                            type={req.publictext ? "text" : "password"}
                            onChange={(e) => setResponseText(e.target.value)}
                            value={responseText}
                            maxLength={400}
                            className="min-h-[30px] cursor-text resize-none rounded-md border border-border bg-background py-1.5 pl-4 text-inherit focus:outline-none focus:ring-2 focus:ring-accent"
                            autoFocus={true}
                            onKeyDown={(e) => keyutil.keydownWrapper(handleKeyDown)(e)}
                        />
                    )}
                    {req.checkboxmsg ? (
                        <div className="flex items-center gap-1.5">
                            <input
                                type="checkbox"
                                id={`uiprompt-${req.requestid}`}
                                className="accent-accent cursor-pointer"
                                ref={checkboxRef}
                            />
                            <label htmlFor={`uiprompt-${req.requestid}`} className="cursor-pointer">
                                {req.checkboxmsg}
                            </label>
                        </div>
                    ) : null}
                </div>
                <div className="flex justify-end gap-2">
                    <button
                        className="cursor-pointer rounded px-2 py-1 text-secondary hover:text-primary"
                        onClick={handleCancel}
                    >
                        {req.cancellabel || "Cancel"}
                    </button>
                    <button
                        className="cursor-pointer rounded bg-accent/80 px-2 py-1 text-primary transition-colors hover:bg-accent"
                        onClick={handleSubmit}
                    >
                        {req.oklabel || (req.responsetype === "confirm" ? "OK" : "Submit")}
                    </button>
                </div>
            </div>
        </div>
    );
};

export const UserInputPromptOverlay = React.memo(UserInputPromptOverlayComp);
UserInputPromptOverlay.displayName = "UserInputPromptOverlay";
