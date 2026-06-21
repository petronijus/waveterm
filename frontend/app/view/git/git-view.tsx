// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { TypeAheadModal } from "@/app/modals/typeaheadmodal";
import { cn } from "@/util/util";
import * as jotai from "jotai";
import * as React from "react";
import { GitViewModel } from "./git-model";

import "./git.scss";

function fmtRelativeTime(unixSeconds: number): string {
    if (!unixSeconds) return "";
    const diff = Math.floor(Date.now() / 1000) - unixSeconds;
    if (diff < 60) return "just now";
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    if (diff < 86400 * 7) return `${Math.floor(diff / 86400)}d ago`;
    if (diff < 86400 * 30) return `${Math.floor(diff / (86400 * 7))}w ago`;
    return new Date(unixSeconds * 1000).toLocaleDateString();
}

function statusLetter(f: GitFileStatus): { letter: string; color: string; title: string } {
    if (f.untracked) return { letter: "U", color: "text-success", title: "Untracked" };
    const code = (f.staged ? f.indexstatus : f.workstatus) || "M";
    switch (code) {
        case "M":
            return { letter: "M", color: "text-warning", title: "Modified" };
        case "A":
            return { letter: "A", color: "text-success", title: "Added" };
        case "D":
            return { letter: "D", color: "text-error", title: "Deleted" };
        case "R":
            return { letter: "R", color: "text-accent", title: "Renamed" };
        case "C":
            return { letter: "C", color: "text-accent", title: "Copied" };
        default:
            return { letter: code, color: "text-secondary", title: "Changed" };
    }
}

const ToolbarButton = React.memo(function ToolbarButton({
    icon,
    title,
    onClick,
    disabled,
    spin,
}: {
    icon: string;
    title: string;
    onClick: (e: React.MouseEvent) => void;
    disabled?: boolean;
    spin?: boolean;
}) {
    return (
        <button
            className="flex items-center justify-center w-6 h-6 rounded hover:bg-white/10 transition-colors cursor-pointer text-secondary hover:text-primary disabled:opacity-40 disabled:cursor-default"
            title={title}
            disabled={disabled}
            onClick={onClick}
        >
            <i className={cn(`fa-sharp fa-solid fa-${icon} text-xs`, spin && "fa-spin")} />
        </button>
    );
});
ToolbarButton.displayName = "ToolbarButton";

const FileRow = React.memo(function FileRow({
    file,
    staged,
    onStage,
    onUnstage,
    onDiscard,
    onDiff,
}: {
    file: GitFileStatus;
    staged: boolean;
    onStage: (p: string) => void;
    onUnstage: (p: string) => void;
    onDiscard: (f: GitFileStatus) => void;
    onDiff: (f: GitFileStatus) => void;
}) {
    const s = statusLetter(file);
    return (
        <div className="git-file-row group flex items-center gap-2 px-3 py-1 text-xs hover:bg-white/5">
            <span className={cn("w-3 text-center font-mono font-bold", s.color)} title={s.title}>
                {s.letter}
            </span>
            <span
                className="flex-1 truncate cursor-pointer hover:text-primary"
                title={file.path}
                onClick={() => onDiff(file)}
            >
                {file.origpath ? `${file.origpath} → ${file.path}` : file.path}
            </span>
            {file.binary ? (
                <span className="text-secondary font-mono">bin</span>
            ) : (
                <span className="font-mono whitespace-pre">
                    {file.added > 0 && <span className="text-success">+{file.added}</span>}
                    {file.added > 0 && file.removed > 0 && " "}
                    {file.removed > 0 && <span className="text-error">-{file.removed}</span>}
                </span>
            )}
            <span className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                {!staged && (
                    <button
                        className="w-5 h-5 rounded hover:bg-white/10 cursor-pointer text-error/70 hover:text-error"
                        title="Discard changes"
                        onClick={() => onDiscard(file)}
                    >
                        <i className="fa-sharp fa-solid fa-trash text-[10px]" />
                    </button>
                )}
                <button
                    className="w-5 h-5 rounded hover:bg-white/10 cursor-pointer text-secondary hover:text-primary"
                    title={staged ? "Unstage" : "Stage"}
                    onClick={() => (staged ? onUnstage(file.path) : onStage(file.path))}
                >
                    <i className={cn("fa-sharp fa-solid text-[10px]", staged ? "fa-minus" : "fa-plus")} />
                </button>
            </span>
        </div>
    );
});
FileRow.displayName = "FileRow";

const ChangesSection = React.memo(function ChangesSection({ model }: { model: GitViewModel }) {
    const status = jotai.useAtomValue(model.statusAtom);
    const files = status?.files ?? [];
    const staged = files.filter((f) => f.staged);
    const unstaged = files.filter((f) => f.unstaged);

    const onDiff = React.useCallback((f: GitFileStatus) => model.openDiff(f), [model]);
    const onStage = React.useCallback((p: string) => model.stage([p]), [model]);
    const onUnstage = React.useCallback((p: string) => model.unstage([p]), [model]);
    const onDiscard = React.useCallback(
        (f: GitFileStatus) => {
            if (window.confirm(`Discard changes to ${f.path}? This cannot be undone.`)) {
                model.discard([f.path]);
            }
        },
        [model]
    );

    if (files.length === 0) {
        return <div className="px-3 py-4 text-xs text-secondary italic">No changes — working tree clean</div>;
    }

    return (
        <div className="flex flex-col">
            {staged.length > 0 && (
                <>
                    <div className="git-section-header flex items-center justify-between px-3 py-1 text-[11px] uppercase tracking-wide text-secondary bg-panel">
                        <span>Staged ({staged.length})</span>
                        <button
                            className="cursor-pointer hover:text-primary normal-case"
                            onClick={() => model.unstage([])}
                        >
                            Unstage all
                        </button>
                    </div>
                    {staged.map((f) => (
                        <FileRow
                            key={"s-" + f.path}
                            file={f}
                            staged
                            onStage={onStage}
                            onUnstage={onUnstage}
                            onDiscard={onDiscard}
                            onDiff={onDiff}
                        />
                    ))}
                </>
            )}
            {unstaged.length > 0 && (
                <>
                    <div className="git-section-header flex items-center justify-between px-3 py-1 text-[11px] uppercase tracking-wide text-secondary bg-panel">
                        <span>Changes ({unstaged.length})</span>
                        <button
                            className="cursor-pointer hover:text-primary normal-case"
                            onClick={() => model.stage([])}
                        >
                            Stage all
                        </button>
                    </div>
                    {unstaged.map((f) => (
                        <FileRow
                            key={"u-" + f.path}
                            file={f}
                            staged={false}
                            onStage={onStage}
                            onUnstage={onUnstage}
                            onDiscard={onDiscard}
                            onDiff={onDiff}
                        />
                    ))}
                </>
            )}
        </div>
    );
});
ChangesSection.displayName = "ChangesSection";

const CommitBox = React.memo(function CommitBox({ model }: { model: GitViewModel }) {
    const status = jotai.useAtomValue(model.statusAtom);
    const [message, setMessage] = jotai.useAtom(model.commitMessageAtom);
    const [amend, setAmend] = jotai.useAtom(model.commitAmendAtom);
    const busy = jotai.useAtomValue(model.actionBusyAtom);
    const stagedCount = (status?.files ?? []).filter((f) => f.staged).length;
    const canCommit = !busy && (amend || (stagedCount > 0 && message.trim().length > 0));

    return (
        <div className="flex flex-col gap-1 p-2 border-t border-border">
            <textarea
                className="w-full bg-transparent border border-border rounded px-2 py-1 text-xs text-primary placeholder-secondary outline-none resize-none focus:border-accent"
                rows={2}
                placeholder="Commit message…"
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && canCommit) {
                        e.preventDefault();
                        model.commit();
                    }
                }}
            />
            <div className="flex items-center gap-2 text-xs">
                <label className="flex items-center gap-1 cursor-pointer text-secondary">
                    <input type="checkbox" checked={amend} onChange={(e) => setAmend(e.target.checked)} />
                    Amend
                </label>
                <span className="flex-1" />
                <button
                    className="bg-accent/80 text-primary rounded px-3 py-1 hover:bg-accent transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-default"
                    disabled={!canCommit}
                    onClick={() => model.commit()}
                >
                    Commit{stagedCount > 0 ? ` (${stagedCount})` : ""}
                </button>
            </div>
        </div>
    );
});
CommitBox.displayName = "CommitBox";

const HistorySection = React.memo(function HistorySection({ model }: { model: GitViewModel }) {
    const commits = jotai.useAtomValue(model.logAtom);
    const hasMore = jotai.useAtomValue(model.logHasMoreAtom);
    if (commits.length === 0) {
        return <div className="px-3 py-2 text-xs text-secondary italic">No commits</div>;
    }
    return (
        <div className="flex flex-col">
            {commits.map((c) => (
                <div key={c.fullhash} className="git-commit-row flex flex-col px-3 py-1 hover:bg-white/5">
                    <div className="flex items-center gap-2 text-xs">
                        <span className="font-mono text-accent">{c.hash}</span>
                        <span className="flex-1 truncate text-primary" title={c.subject}>
                            {c.subject}
                        </span>
                    </div>
                    <div className="flex items-center gap-2 text-[11px] text-secondary">
                        <span className="truncate">{c.author}</span>
                        <span>·</span>
                        <span className="shrink-0">{fmtRelativeTime(c.ts)}</span>
                    </div>
                </div>
            ))}
            {hasMore && (
                <button
                    className="px-3 py-1.5 text-xs text-secondary hover:text-primary cursor-pointer"
                    onClick={() => model.loadMoreLog()}
                >
                    Load more…
                </button>
            )}
        </div>
    );
});
HistorySection.displayName = "HistorySection";

const BranchSwitcher = React.memo(function BranchSwitcher({
    model,
    anchorRef,
    blockRef,
}: {
    model: GitViewModel;
    anchorRef: React.RefObject<HTMLElement>;
    blockRef: React.RefObject<HTMLDivElement>;
}) {
    const open = jotai.useAtomValue(model.branchSwitcherOpenAtom);
    const [filter, setFilter] = jotai.useAtom(model.branchFilterAtom);
    const branches = jotai.useAtomValue(model.branchesAtom);
    if (!open) return null;

    const allBranches = branches?.branches ?? [];
    const filtered = allBranches.filter((b) => b.name.toLowerCase().includes(filter.toLowerCase()));
    const exactMatch = allBranches.some((b) => b.name === filter);

    const suggestions: SuggestionsType[] = filtered.map((b) => ({
        label: b.name,
        value: b.name,
        icon: "code-branch",
        iconColor: "inherit",
        status: "connected" as ConnStatusType,
        current: b.iscurrent,
        onSelect: () => model.checkout(b.name),
    }));

    if (filter.trim() && !exactMatch) {
        suggestions.push({
            label: `Create branch "${filter.trim()}"`,
            value: filter.trim(),
            icon: "plus",
            iconColor: "inherit",
            status: "connected" as ConnStatusType,
            onSelect: () => model.checkout(filter.trim(), true),
        });
    }

    return (
        <TypeAheadModal
            anchorRef={anchorRef}
            blockRef={blockRef}
            centered
            suggestions={suggestions}
            label="Switch or create branch…"
            value={filter}
            autoFocus
            onChange={setFilter}
            onClickBackdrop={() => model.closeBranchSwitcher()}
            onKeyDown={(e) => {
                if (e.key === "Escape") {
                    model.closeBranchSwitcher();
                }
            }}
        />
    );
});
BranchSwitcher.displayName = "BranchSwitcher";

function diffLineClass(line: string): string {
    if (line.startsWith("+") && !line.startsWith("+++")) return "text-success";
    if (line.startsWith("-") && !line.startsWith("---")) return "text-error";
    if (line.startsWith("@@")) return "text-accent";
    if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("+++") || line.startsWith("---"))
        return "text-secondary";
    return "text-primary";
}

const DiffPanel = React.memo(function DiffPanel({ model }: { model: GitViewModel }) {
    const diff = jotai.useAtomValue(model.diffAtom);
    const loading = jotai.useAtomValue(model.diffLoadingAtom);
    if (diff == null) return null;
    const lines = (diff.diff ?? "").split("\n");
    return (
        <div className="absolute inset-0 z-10 flex flex-col bg-panel">
            <div className="shrink-0 flex items-center gap-2 px-3 py-1.5 border-b border-border text-xs bg-panel">
                <i className="fa-sharp fa-solid fa-file-lines text-secondary" />
                <span className="flex-1 truncate font-mono text-primary" title={diff.path}>
                    {diff.path}
                </span>
                <button
                    className="w-5 h-5 rounded hover:bg-white/10 cursor-pointer text-secondary hover:text-primary"
                    title="Close diff"
                    onClick={() => model.closeDiff()}
                >
                    <i className="fa-sharp fa-solid fa-xmark text-xs" />
                </button>
            </div>
            <div className="flex-1 overflow-auto">
                {loading ? (
                    <div className="p-3 text-xs text-secondary">Loading diff…</div>
                ) : diff.binary ? (
                    <div className="p-3 text-xs text-secondary italic">Binary file — no text diff</div>
                ) : lines.length === 0 || (lines.length === 1 && lines[0] === "") ? (
                    <div className="p-3 text-xs text-secondary italic">No diff</div>
                ) : (
                    <pre className="text-[11px] font-mono leading-relaxed p-2 whitespace-pre">
                        {lines.map((line, i) => (
                            <div key={i} className={diffLineClass(line)}>
                                {line || " "}
                            </div>
                        ))}
                    </pre>
                )}
            </div>
        </div>
    );
});
DiffPanel.displayName = "DiffPanel";

const ActionStatusBar = React.memo(function ActionStatusBar({ model }: { model: GitViewModel }) {
    const status = jotai.useAtomValue(model.actionStatusAtom);
    if (status == null) return null;
    return (
        <div
            className={cn(
                "shrink-0 flex items-center px-3 py-1 text-xs border-t border-border",
                status.isError ? "text-error" : "text-secondary"
            )}
        >
            <span className="flex-1 truncate">{status.message}</span>
            {status.isError && (
                <button
                    className="ml-2 w-4 h-4 rounded hover:bg-white/10 cursor-pointer text-secondary hover:text-primary"
                    onClick={() => model.clearActionStatus()}
                >
                    <i className="fa-sharp fa-solid fa-xmark text-[10px]" />
                </button>
            )}
        </div>
    );
});
ActionStatusBar.displayName = "ActionStatusBar";

export const GitView: React.FC<ViewComponentProps<GitViewModel>> = React.memo(function GitView({ blockRef, model }) {
    const connStatus = jotai.useAtomValue(model.connStatus);
    const connection = jotai.useAtomValue(model.connection);
    const repoInfo = jotai.useAtomValue(model.repoInfoAtom);
    const status = jotai.useAtomValue(model.statusAtom);
    const loading = jotai.useAtomValue(model.loadingAtom);
    const busy = jotai.useAtomValue(model.actionBusyAtom);
    const branchAnchorRef = React.useRef<HTMLButtonElement>(null);

    const isFirstRender = React.useRef(true);
    React.useEffect(() => {
        if (isFirstRender.current) {
            isFirstRender.current = false;
            return;
        }
        model.forceRefreshOnConnectionChange();
    }, [connection]);

    if (!connStatus?.connected) {
        return (
            <div className="flex items-center justify-center h-full text-secondary text-sm">Waiting for connection…</div>
        );
    }

    if (loading && repoInfo == null) {
        return <div className="flex items-center justify-center h-full text-secondary text-sm">Loading…</div>;
    }

    if (!repoInfo?.isrepo) {
        return (
            <div className="flex flex-col items-center justify-center h-full gap-2 text-secondary text-sm px-6 text-center">
                <i className="fa-sharp fa-solid fa-code-branch text-2xl opacity-40" />
                <div>No git repository found</div>
                {repoInfo?.errormsg && <div className="text-xs opacity-60">{repoInfo.errormsg}</div>}
                <button
                    className="mt-1 text-xs text-accent hover:underline cursor-pointer"
                    onClick={() => model.refreshAll()}
                >
                    Retry
                </button>
            </div>
        );
    }

    const branchLabel = status?.detached ? `(detached ${status?.head ?? ""})` : (status?.branch ?? "…");

    return (
        <div className="relative flex flex-col w-full h-full overflow-hidden">
            <div className="git-toolbar shrink-0 flex items-center gap-1 px-2 py-1 border-b border-border bg-panel">
                <button
                    ref={branchAnchorRef}
                    className="flex items-center gap-1.5 px-2 py-0.5 rounded border border-border text-xs text-secondary hover:text-primary hover:bg-white/5 transition-colors cursor-pointer max-w-[40%]"
                    title="Switch branch"
                    onClick={() => model.toggleBranchSwitcher()}
                >
                    <i className="fa-sharp fa-solid fa-code-branch text-[10px]" />
                    <span className="truncate">{branchLabel}</span>
                    <i className="fa-sharp fa-solid fa-caret-down text-[10px]" />
                </button>
                {(status?.ahead > 0 || status?.behind > 0) && (
                    <span className="text-xs text-secondary font-mono whitespace-pre">
                        {status?.ahead > 0 ? ` ↑${status.ahead}` : ""}
                        {status?.behind > 0 ? ` ↓${status.behind}` : ""}
                    </span>
                )}
                <span className="flex-1" />
                <ToolbarButton icon="cloud-arrow-down" title="Fetch" disabled={busy} onClick={() => model.sync("fetch")} />
                <ToolbarButton icon="arrow-down" title="Pull" disabled={busy} onClick={() => model.sync("pull")} />
                <ToolbarButton icon="arrow-up" title="Push" disabled={busy} onClick={() => model.sync("push")} />
                <ToolbarButton
                    icon="box-archive"
                    title="Stash changes"
                    disabled={busy}
                    onClick={() => model.stash("push")}
                />
                {status?.stashcount > 0 && (
                    <ToolbarButton
                        icon="box-open"
                        title={`Pop stash (${status.stashcount})`}
                        disabled={busy}
                        onClick={() => model.stash("pop")}
                    />
                )}
            </div>

            <div className="flex-1 overflow-y-auto">
                <ChangesSection model={model} />
                <div className="git-section-header px-3 py-1 text-[11px] uppercase tracking-wide text-secondary bg-panel border-t border-border">
                    History
                </div>
                <HistorySection model={model} />
            </div>

            <CommitBox model={model} />
            <ActionStatusBar model={model} />

            <BranchSwitcher model={model} anchorRef={branchAnchorRef} blockRef={blockRef} />
            <DiffPanel model={model} />
        </div>
    );
});
GitView.displayName = "GitView";
