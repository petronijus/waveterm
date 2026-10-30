// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { globalStore } from "@/app/store/jotaiStore";
import { cn } from "@/util/util";
import * as jotai from "jotai";
import * as React from "react";
import type { GitOverviewViewModel } from "./gitoverview-model";
import { fmtClock, parsePathList, RepoStateStyles, splitRepos, syncTargets } from "./gitoverview-util";

const ActionTitles: Record<string, string> = {
    pull: "Fetch, then fast-forward to the upstream (never overwrites ignored files)",
    push: "Fetch, then push this branch to its upstream branch (never forced)",
};

function Badge({ text, className, title }: { text: string; className: string; title: string }) {
    return (
        <span
            className={cn("px-1.5 rounded text-[10px] font-mono whitespace-nowrap bg-white/10", className)}
            title={title}
        >
            {text}
        </span>
    );
}

const RepoRow = React.memo(function RepoRow({ model, repo }: { model: GitOverviewViewModel; repo: GitOverviewRepo }) {
    const style = RepoStateStyles[repo.state] ?? RepoStateStyles.unknown;
    const branch = repo.upstream ? `${repo.branch} → ${repo.upstream}` : repo.branch;
    let title = repo.path;
    if (repo.activity) {
        title += `\nlast activity ${new Date(repo.activity).toLocaleString()}`;
    }
    title += "\nclick to open in the Git view";

    let action: React.ReactNode = null;
    if (repo.busy) {
        action = <span className="text-xs text-secondary italic">{repo.busy}…</span>;
    } else if (repo.action === "pull" || repo.action === "push") {
        action = (
            <button
                className="px-2 py-0.5 rounded text-xs font-medium bg-accent/30 hover:bg-accent/50 text-primary cursor-pointer"
                title={ActionTitles[repo.action]}
                onClick={(e) => {
                    e.stopPropagation();
                    model.syncRow(repo);
                }}
            >
                {repo.action === "pull" ? "Pull" : "Push"}
            </button>
        );
    } else if (repo.state !== "in-sync" && repo.state !== "unknown" && repo.reason) {
        action = (
            <span className="text-xs text-muted truncate" title={repo.reason}>
                {repo.reason}
            </span>
        );
    }

    return (
        <div
            className="flex flex-col px-3 py-1.5 border-b border-border hover:bg-hoverbg cursor-pointer"
            onClick={() => model.openRepo(repo)}
        >
            <div className="flex items-center gap-2 min-w-0">
                <span className={cn("w-2 h-2 rounded-full shrink-0", style.dot)} title={style.label} />
                <span className="text-sm text-primary truncate" title={title}>
                    {repo.rel}
                </span>
                {branch && <span className="text-xs text-muted font-mono truncate">{branch}</span>}
                {repo.ahead > 0 && <Badge text={`↑${repo.ahead}`} className="text-accent" title="commits to push" />}
                {repo.behind > 0 && <Badge text={`↓${repo.behind}`} className="text-warning" title="commits to pull" />}
                {repo.changed > 0 && (
                    <Badge text={`●${repo.changed}`} className="text-sky-400" title="changed or untracked files" />
                )}
                <span className="ml-auto flex items-center min-w-0 shrink">{action}</span>
            </div>
            {repo.err && (
                <div className="text-xs text-error pl-4 truncate" title={repo.err}>
                    {repo.err}
                </div>
            )}
            {!repo.err && repo.fetcherr && (
                <div className="text-[10px] text-muted pl-4 truncate" title={repo.fetcherr}>
                    fetch failed: {repo.fetcherr}
                </div>
            )}
        </div>
    );
});

const SettingsPanel = React.memo(function SettingsPanel({ model }: { model: GitOverviewViewModel }) {
    const cfg = jotai.useAtomValue(model.configAtom);
    const [roots, setRoots] = React.useState(cfg.roots.join("\n"));
    const [ignore, setIgnore] = React.useState(cfg.ignore.join("\n"));
    const save = () => model.saveSettings(parsePathList(roots), parsePathList(ignore));
    return (
        <div className="flex flex-col gap-2 px-3 py-3 border-b border-border bg-white/5">
            <label className="text-xs text-secondary">
                Folders to scan for repos (one per line, ~ allowed; nested repos and worktrees up to 4 levels deep)
            </label>
            <textarea
                className="w-full min-h-16 p-2 rounded bg-black/30 border border-border text-sm font-mono text-primary"
                value={roots}
                placeholder="~/Documents/Dev"
                onChange={(e) => setRoots(e.target.value)}
            />
            <label className="text-xs text-secondary">
                Repos to hide, relative to their folder (one per line; <code>dir/**</code> hides everything below dir)
            </label>
            <textarea
                className="w-full min-h-10 p-2 rounded bg-black/30 border border-border text-sm font-mono text-primary"
                value={ignore}
                onChange={(e) => setIgnore(e.target.value)}
            />
            <div className="flex gap-2 justify-end">
                {cfg.roots.length > 0 && (
                    <button
                        className="px-2.5 py-1 rounded text-xs bg-white/10 hover:bg-white/20 cursor-pointer"
                        onClick={() => globalStore.set(model.settingsOpenAtom, false)}
                    >
                        Cancel
                    </button>
                )}
                <button
                    className="px-2.5 py-1 rounded text-xs font-medium bg-accent/40 hover:bg-accent/60 cursor-pointer"
                    onClick={save}
                >
                    Save
                </button>
            </div>
        </div>
    );
});

const ConfirmBar = React.memo(function ConfirmBar({
    model,
    targets,
}: {
    model: GitOverviewViewModel;
    targets: GitOverviewSyncTarget[];
}) {
    const data = jotai.useAtomValue(model.dataAtom);
    const relOf = new Map((data?.repos ?? []).map((r) => [r.path, r.rel]));
    const pushes = targets.filter((t) => t.action === "push").map((t) => relOf.get(t.path) ?? t.path);
    const pulls = targets.length - pushes.length;
    return (
        <div className="flex flex-col gap-1.5 px-3 py-2 border-b border-border bg-warning/15">
            <div className="text-sm">
                Push {pushes.length} and pull {pulls} {targets.length === 1 ? "repo" : "repos"}?
            </div>
            <div className="text-xs text-secondary break-words">Push: {pushes.join(", ")}</div>
            <div className="flex gap-2 justify-end">
                <button
                    className="px-2.5 py-1 rounded text-xs bg-white/10 hover:bg-white/20 cursor-pointer"
                    onClick={() => model.confirmSyncAll(false)}
                >
                    Cancel
                </button>
                <button
                    className="px-2.5 py-1 rounded text-xs font-medium bg-accent/40 hover:bg-accent/60 cursor-pointer"
                    onClick={() => model.confirmSyncAll(true)}
                >
                    Sync all
                </button>
            </div>
        </div>
    );
});

export const GitOverviewView: React.FC<ViewComponentProps<GitOverviewViewModel>> = React.memo(function GitOverviewView({
    model,
}) {
    const data = jotai.useAtomValue(model.dataAtom);
    const error = jotai.useAtomValue(model.errorAtom);
    const settingsOpen = jotai.useAtomValue(model.settingsOpenAtom);
    const confirm = jotai.useAtomValue(model.confirmAtom);
    const [showInSync, setShowInSync] = jotai.useAtom(model.showInSyncAtom);
    const cfg = jotai.useAtomValue(model.configAtom);

    const { attention, inSync } = splitRepos(data?.repos);
    const { targets } = syncTargets(data?.repos);
    const hasRoots = cfg.roots.length > 0;

    return (
        <div className="flex flex-col h-full w-full overflow-hidden text-primary">
            {hasRoots && (
                <div className="flex flex-wrap items-center gap-2 px-3 py-2 border-b border-border">
                    <span className="text-xs text-secondary">
                        {attention.length} need attention · {inSync.length} in sync
                    </span>
                    <button
                        className={cn(
                            "ml-auto px-2.5 py-1 rounded text-xs font-medium",
                            targets.length > 0
                                ? "bg-accent/40 hover:bg-accent/60 cursor-pointer"
                                : "bg-white/5 text-muted cursor-default"
                        )}
                        disabled={targets.length === 0}
                        title="Fast-forward every repo that is only behind, push every repo that is only ahead"
                        onClick={() => model.syncAll()}
                    >
                        Sync all ({targets.length})
                    </button>
                </div>
            )}
            {settingsOpen && <SettingsPanel model={model} />}
            {confirm && <ConfirmBar model={model} targets={confirm} />}
            {error && <div className="px-3 py-1 text-xs bg-error/30">{error}</div>}
            {data?.scanerr && <div className="px-3 py-1 text-xs bg-error/30">{data.scanerr}</div>}
            <div className="flex-1 overflow-y-auto">
                {hasRoots && !data && <div className="p-6 text-center text-muted text-sm">Scanning…</div>}
                {attention.map((r) => (
                    <RepoRow key={r.path} model={model} repo={r} />
                ))}
                {inSync.length > 0 && (
                    <button
                        className="w-full text-left px-3 py-1.5 text-xs text-secondary hover:bg-hoverbg cursor-pointer"
                        onClick={() => setShowInSync(!showInSync)}
                    >
                        {showInSync ? "▾" : "▸"} {inSync.length} in sync
                    </button>
                )}
                {showInSync && inSync.map((r) => <RepoRow key={r.path} model={model} repo={r} />)}
            </div>
            {data && (
                <div className="px-3 py-1 text-[10px] text-muted border-t border-border">
                    changes via {data.watching} · fetched {fmtClock(data.lastfetch)} · next {fmtClock(data.nextfetch)}
                    {data.fetching ? " · fetching…" : ""}
                </div>
            )}
        </div>
    );
});
