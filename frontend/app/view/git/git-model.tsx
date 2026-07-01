// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { atoms } from "@/app/store/global";
import { globalStore } from "@/app/store/jotaiStore";
import * as WOS from "@/app/store/wos";
import { RpcApi } from "@/app/store/wshclientapi";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { fireAndForget, isBlank, makeConnRoute } from "@/util/util";
import * as jotai from "jotai";
import { GitView } from "./git-view";
import { GitEnv } from "./gitenv";

const StatusPollIntervalMs = 2000;
const LogPageSize = 50;

function projectNameFromPath(p: string): string {
    if (p === "~" || isBlank(p)) {
        return "home";
    }
    const parts = p.replace(/[/\\]+$/, "").split(/[/\\]/);
    return parts[parts.length - 1] || p;
}

function uniqueProjectName(base: string, projects: { [key: string]: ProjectConfigType }): string {
    if (projects[base] == null) {
        return base;
    }
    let i = 2;
    while (projects[`${base} (${i})`] != null) {
        i++;
    }
    return `${base} (${i})`;
}

export type GitActionStatus = {
    message: string;
    isError: boolean;
};

// secret-store keys must match ^[A-Za-z][A-Za-z0-9_]*$, so encode the host.
function gitSecretKey(host: string, field: string): string {
    return `git_https_${host.replace(/[^A-Za-z0-9]/g, "_")}_${field}`;
}

export class GitViewModel implements ViewModel {
    viewType: string;
    blockId: string;
    env: GitEnv;

    viewIcon = jotai.atom<string>("code-branch");
    viewName = jotai.atom<string>("Git");
    manageConnection = jotai.atom<boolean>(true);
    filterOutNowsh = jotai.atom<boolean>(true);
    noPadding = jotai.atom<boolean>(true);

    gitRootAtom: jotai.PrimitiveAtom<string>;
    repoInfoAtom: jotai.PrimitiveAtom<GitRepoInfo>;
    statusAtom: jotai.PrimitiveAtom<GitStatus>;
    branchesAtom: jotai.PrimitiveAtom<GitBranchList>;
    logAtom: jotai.PrimitiveAtom<GitCommit[]>;
    logOffsetAtom: jotai.PrimitiveAtom<number>;
    logHasMoreAtom: jotai.PrimitiveAtom<boolean>;
    loadingAtom: jotai.PrimitiveAtom<boolean>;
    errorAtom: jotai.PrimitiveAtom<string>;
    actionStatusAtom: jotai.PrimitiveAtom<GitActionStatus>;
    actionBusyAtom: jotai.PrimitiveAtom<boolean>;

    // F2+ UI state
    branchSwitcherOpenAtom: jotai.PrimitiveAtom<boolean>;
    branchFilterAtom: jotai.PrimitiveAtom<string>;
    // F3+ UI state
    commitMessageAtom: jotai.PrimitiveAtom<string>;
    commitAmendAtom: jotai.PrimitiveAtom<boolean>;
    // F5 inline diff
    diffAtom: jotai.PrimitiveAtom<GitDiff>;
    diffLoadingAtom: jotai.PrimitiveAtom<boolean>;
    diffFileAtom: jotai.PrimitiveAtom<GitFileStatus>;
    diffStagedAtom: jotai.PrimitiveAtom<boolean>;
    // multi-file review mode: step through all changed files in one diff panel
    reviewActiveAtom: jotai.PrimitiveAtom<boolean>;
    reviewFilesAtom: jotai.PrimitiveAtom<GitFileStatus[]>;
    reviewIndexAtom: jotai.PrimitiveAtom<number>;
    // push credential prompt
    authOpenAtom: jotai.PrimitiveAtom<boolean>;
    authHostAtom: jotai.PrimitiveAtom<string>;
    authUsernameAtom: jotai.PrimitiveAtom<string>;
    authErrorAtom: jotai.PrimitiveAtom<string>;
    authBusyAtom: jotai.PrimitiveAtom<boolean>;
    pendingPushUpstream = false;

    connection: jotai.Atom<string>;
    connStatus: jotai.Atom<ConnStatus>;
    cwdSource: jotai.Atom<string>;
    openPickerAtom: jotai.PrimitiveAtom<boolean>;
    isCurrentDirBookmarkedAtom: jotai.Atom<boolean>;
    viewText: jotai.Atom<HeaderElem[]>;
    endIconButtons: jotai.Atom<IconButtonDecl[]>;

    disposed = false;
    cancelPoll: (() => void) | null = null;
    cwdUnsub: (() => void) | null = null;
    connStatusUnsub: (() => void) | null = null;
    lastResolvedCwd: string = null;
    fetchEpoch = 0;
    lastBranchHeadSig: string = null;

    constructor({ blockId, waveEnv }: ViewModelInitType) {
        this.viewType = "git";
        this.blockId = blockId;
        this.env = waveEnv;

        this.gitRootAtom = jotai.atom<string>(null) as jotai.PrimitiveAtom<string>;
        this.repoInfoAtom = jotai.atom<GitRepoInfo>(null) as jotai.PrimitiveAtom<GitRepoInfo>;
        this.statusAtom = jotai.atom<GitStatus>(null) as jotai.PrimitiveAtom<GitStatus>;
        this.branchesAtom = jotai.atom<GitBranchList>(null) as jotai.PrimitiveAtom<GitBranchList>;
        this.logAtom = jotai.atom<GitCommit[]>([]) as jotai.PrimitiveAtom<GitCommit[]>;
        this.logOffsetAtom = jotai.atom<number>(0);
        this.logHasMoreAtom = jotai.atom<boolean>(false);
        this.loadingAtom = jotai.atom<boolean>(true);
        this.errorAtom = jotai.atom<string>(null) as jotai.PrimitiveAtom<string>;
        this.actionStatusAtom = jotai.atom<GitActionStatus>(null) as jotai.PrimitiveAtom<GitActionStatus>;
        this.actionBusyAtom = jotai.atom<boolean>(false);
        this.branchSwitcherOpenAtom = jotai.atom<boolean>(false);
        this.branchFilterAtom = jotai.atom<string>("");
        this.commitMessageAtom = jotai.atom<string>("");
        this.commitAmendAtom = jotai.atom<boolean>(false);
        this.diffAtom = jotai.atom<GitDiff>(null) as jotai.PrimitiveAtom<GitDiff>;
        this.diffLoadingAtom = jotai.atom<boolean>(false);
        this.diffFileAtom = jotai.atom<GitFileStatus>(null) as jotai.PrimitiveAtom<GitFileStatus>;
        this.diffStagedAtom = jotai.atom<boolean>(false);
        this.reviewActiveAtom = jotai.atom<boolean>(false);
        this.reviewFilesAtom = jotai.atom<GitFileStatus[]>([]) as jotai.PrimitiveAtom<GitFileStatus[]>;
        this.reviewIndexAtom = jotai.atom<number>(0);
        this.authOpenAtom = jotai.atom<boolean>(false);
        this.authHostAtom = jotai.atom<string>("") as jotai.PrimitiveAtom<string>;
        this.authUsernameAtom = jotai.atom<string>("") as jotai.PrimitiveAtom<string>;
        this.authErrorAtom = jotai.atom<string>(null) as jotai.PrimitiveAtom<string>;
        this.authBusyAtom = jotai.atom<boolean>(false);

        this.connection = jotai.atom((get) => {
            const connValue = get(this.env.getBlockMetaKeyAtom(blockId, "connection"));
            if (isBlank(connValue)) {
                return "local";
            }
            return connValue;
        });
        this.connStatus = jotai.atom((get) => {
            const connName = get(this.env.getBlockMetaKeyAtom(blockId, "connection"));
            const connAtom = this.env.getConnStatusAtom(connName);
            return get(connAtom);
        });
        this.openPickerAtom = jotai.atom<boolean>(false);
        this.cwdSource = jotai.atom((get) => {
            // cmd:cwd/file are what the header picker and a connections-panel project
            // set, so they take precedence; git:root is a legacy fallback (nothing sets
            // it anymore — the picker clears it).
            const cmdCwd = get(this.env.getBlockMetaKeyAtom(blockId, "cmd:cwd"));
            if (!isBlank(cmdCwd)) {
                return cmdCwd;
            }
            const file = get(this.env.getBlockMetaKeyAtom(blockId, "file"));
            if (!isBlank(file)) {
                return file;
            }
            const gitRoot = get(this.env.getBlockMetaKeyAtom(blockId, "git:root"));
            if (!isBlank(gitRoot)) {
                return gitRoot;
            }
            return "~";
        });
        this.viewText = jotai.atom((get) => {
            const path = get(this.cwdSource);
            return [
                {
                    elemtype: "text",
                    text: path,
                    className: "cursor-pointer hover:text-primary",
                    onClick: () => globalStore.set(this.openPickerAtom, true),
                },
            ] as HeaderElem[];
        });

        this.isCurrentDirBookmarkedAtom = jotai.atom<boolean>((get) => {
            const projects = get(atoms.fullConfigAtom)?.projects ?? {};
            const curPath = get(this.cwdSource);
            const curConn = get(this.connection) || "local";
            return Object.values(projects).some((p) => p?.path === curPath && (p?.connection || "local") === curConn);
        });

        this.endIconButtons = jotai.atom((get) => {
            const buttons: IconButtonDecl[] = [];
            const bookmarked = get(this.isCurrentDirBookmarkedAtom);
            buttons.push({
                elemtype: "iconbutton",
                icon: bookmarked ? "star" : "regular@star",
                title: bookmarked ? "Remove project bookmark" : "Bookmark folder as project",
                click: () => fireAndForget(() => this.toggleProjectBookmark()),
            });
            const repoInfo = get(this.repoInfoAtom);
            const busy = get(this.actionBusyAtom);
            if (repoInfo?.isrepo) {
                buttons.push({
                    elemtype: "iconbutton",
                    icon: "arrows-rotate",
                    title: "Refresh",
                    disabled: busy,
                    click: () => this.refreshAll(),
                });
            }
            return buttons;
        });

        // Re-check whenever the effective path changes — from the header picker OR a
        // project picked in the connections panel (which sets cmd:cwd) — so the git
        // view jumps to the new folder.
        this.cwdUnsub = globalStore.sub(this.cwdSource, () => this.onCwdChanged());

        // The panel can mount before its connection is `connected` (app start,
        // a reopened layout), so the initial resolveRoot finds no git root and the
        // poll would keep refreshing a status that never loads. Re-resolve the
        // moment the connection (re)connects so the panel recovers on its own.
        this.connStatusUnsub = globalStore.sub(this.connStatus, () => {
            if (this.disposed) {
                return;
            }
            const connected = globalStore.get(this.connStatus)?.connected;
            if (connected && isBlank(globalStore.get(this.gitRootAtom))) {
                fireAndForget(() => this.refreshAll());
            }
        });

        this.startPolling();
    }

    onCwdChanged() {
        if (this.disposed) {
            return;
        }
        const cwd = globalStore.get(this.cwdSource);
        if (cwd === this.lastResolvedCwd) {
            return;
        }
        this.lastResolvedCwd = cwd;
        // drop the previous repo's state so nothing stale lingers while we re-check
        globalStore.set(this.repoInfoAtom, null);
        globalStore.set(this.statusAtom, null);
        globalStore.set(this.branchesAtom, null);
        globalStore.set(this.logAtom, []);
        globalStore.set(this.gitRootAtom, null);
        globalStore.set(this.errorAtom, null);
        globalStore.set(this.loadingAtom, true);
        fireAndForget(() => this.refreshAll());
    }

    get viewComponent(): ViewComponent {
        return GitView;
    }

    getRoute(): string {
        return makeConnRoute(globalStore.get(this.connection));
    }

    async resolveRoot(): Promise<string> {
        const connStatus = globalStore.get(this.connStatus);
        if (!connStatus?.connected) {
            return null;
        }
        const cwd = globalStore.get(this.cwdSource);
        this.lastResolvedCwd = cwd;
        try {
            const info = await this.env.rpc.RemoteGitRepoInfoCommand(
                TabRpcClient,
                { path: cwd },
                { route: this.getRoute() }
            );
            globalStore.set(this.repoInfoAtom, info);
            globalStore.set(this.gitRootAtom, info?.isrepo ? info.gitroot : null);
            return info?.isrepo ? info.gitroot : null;
        } catch (e) {
            globalStore.set(this.repoInfoAtom, { isrepo: false, errormsg: String(e) });
            globalStore.set(this.gitRootAtom, null);
            return null;
        }
    }

    async refreshStatus() {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const epoch = ++this.fetchEpoch;
        try {
            const status = await this.env.rpc.RemoteGitStatusCommand(
                TabRpcClient,
                { gitroot: root },
                { route: this.getRoute() }
            );
            if (!this.disposed && this.fetchEpoch === epoch) {
                globalStore.set(this.statusAtom, status);
                globalStore.set(this.errorAtom, null);
            }
        } catch (e) {
            if (!this.disposed && this.fetchEpoch === epoch) {
                globalStore.set(this.errorAtom, String(e));
            }
        }
    }

    async refreshBranches() {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        try {
            const branches = await this.env.rpc.RemoteGitBranchesCommand(
                TabRpcClient,
                { gitroot: root },
                { route: this.getRoute() }
            );
            if (!this.disposed) {
                globalStore.set(this.branchesAtom, branches);
            }
        } catch (e) {
            // branch list failures are non-fatal
        }
    }

    async refreshLog(reset = true) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const offset = reset ? 0 : globalStore.get(this.logOffsetAtom);
        try {
            const log = await this.env.rpc.RemoteGitLogCommand(
                TabRpcClient,
                { gitroot: root, offset, limit: LogPageSize },
                { route: this.getRoute() }
            );
            if (this.disposed) {
                return;
            }
            const commits = log?.commits ?? [];
            if (reset) {
                globalStore.set(this.logAtom, commits);
            } else {
                globalStore.set(this.logAtom, [...globalStore.get(this.logAtom), ...commits]);
            }
            globalStore.set(this.logOffsetAtom, offset + commits.length);
            globalStore.set(this.logHasMoreAtom, !!log?.hasmore);
        } catch (e) {
            // log failures are non-fatal
        }
    }

    async loadMoreLog() {
        await this.refreshLog(false);
    }

    async refreshAll() {
        globalStore.set(this.loadingAtom, true);
        const root = await this.resolveRoot();
        if (isBlank(root)) {
            globalStore.set(this.loadingAtom, false);
            return;
        }
        await Promise.all([this.refreshStatus(), this.refreshBranches(), this.refreshLog(true)]);
        this.lastBranchHeadSig = this.branchHeadSig();
        globalStore.set(this.loadingAtom, false);
    }

    // Cheap signature of "which branch / which commit" we're on. The poll only
    // pulls the lightweight status each tick; when this signature changes
    // (branch switch or new/amended commit) it also refreshes the branch list
    // and log, which a plain status poll wouldn't pick up.
    branchHeadSig(): string {
        const status = globalStore.get(this.statusAtom);
        if (status == null) {
            return null;
        }
        return `${status.branch} ${status.head} ${status.detached}`;
    }

    startPolling() {
        let cancelled = false;
        this.cancelPoll = () => {
            cancelled = true;
        };
        const poll = async () => {
            await this.refreshAll();
            while (!cancelled && !this.disposed) {
                await new Promise<void>((resolve) => {
                    const timer = setTimeout(resolve, StatusPollIntervalMs);
                    this.cancelPoll = () => {
                        clearTimeout(timer);
                        cancelled = true;
                        resolve();
                    };
                });
                if (cancelled || this.disposed) {
                    break;
                }
                this.cancelPoll = () => {
                    cancelled = true;
                };
                if (!globalStore.get(this.actionBusyAtom)) {
                    if (isBlank(globalStore.get(this.gitRootAtom))) {
                        // No git root yet — the connection wasn't ready when polling
                        // started, the path wasn't a repo, or a repo just appeared
                        // (git init). Re-resolve so polling self-heals instead of
                        // forever refreshing a status that can never load.
                        const root = await this.resolveRoot();
                        if (!isBlank(root)) {
                            await Promise.all([
                                this.refreshStatus(),
                                this.refreshBranches(),
                                this.refreshLog(true),
                            ]);
                            this.lastBranchHeadSig = this.branchHeadSig();
                        }
                    } else {
                        await this.refreshStatus();
                        const sig = this.branchHeadSig();
                        if (sig != null && sig !== this.lastBranchHeadSig) {
                            this.lastBranchHeadSig = sig;
                            await Promise.all([this.refreshBranches(), this.refreshLog(true)]);
                        }
                    }
                }
            }
        };
        poll();
    }

    forceRefreshOnConnectionChange() {
        if (this.cancelPoll) {
            this.cancelPoll();
        }
        this.cancelPoll = null;
        globalStore.set(this.repoInfoAtom, null);
        globalStore.set(this.statusAtom, null);
        globalStore.set(this.branchesAtom, null);
        globalStore.set(this.logAtom, []);
        globalStore.set(this.gitRootAtom, null);
        this.lastResolvedCwd = null;
        globalStore.set(this.loadingAtom, true);
        globalStore.set(this.errorAtom, null);
        this.startPolling();
    }

    // Point the git view at a new path (chosen via the in-app path picker) and
    // re-check. A repo there gets picked up; otherwise the (centered) "No git
    // repository" shows.
    async setRoot(picked: string) {
        if (isBlank(picked)) {
            return;
        }
        // Persist on the block using the same meta keys a connections-panel project
        // uses (cmd:cwd/file), so the header picker and project picks stay consistent.
        // Clear any old git:root so it can't shadow the new path. The cwdSource
        // subscription (onCwdChanged) then clears stale state and re-checks.
        await this.env.rpc.SetMetaCommand(TabRpcClient, {
            oref: WOS.makeORef("block", this.blockId),
            meta: { "git:root": null, "cmd:cwd": picked, file: picked },
        });
    }

    // Bookmark (or un-bookmark) the current folder as a "project", so it shows up in
    // the connections panel — same as the star button in the file preview.
    async toggleProjectBookmark() {
        const path = globalStore.get(this.cwdSource);
        if (isBlank(path)) {
            return;
        }
        const conn = globalStore.get(this.connection);
        const connKey = conn || "local";
        const projects = globalStore.get(atoms.fullConfigAtom)?.projects ?? {};
        const existing = Object.entries(projects).find(
            ([, p]) => p?.path === path && (p?.connection || "local") === connKey
        );
        if (existing) {
            await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name: existing[0], metamaptype: null });
            return;
        }
        const name = uniqueProjectName(projectNameFromPath(path), projects);
        const orders = Object.values(projects).map((p) => p?.["display:order"] ?? 0);
        const nextOrder = orders.length ? Math.max(...orders) + 1 : 1;
        const meta: ProjectConfigType = { path, "display:order": nextOrder };
        if (conn && conn !== "local") {
            meta.connection = conn;
        }
        await RpcApi.SetProjectsConfigCommand(TabRpcClient, { name, metamaptype: meta });
    }

    setActionStatus(status: GitActionStatus) {
        globalStore.set(this.actionStatusAtom, status);
        if (!status.isError) {
            setTimeout(() => {
                if (globalStore.get(this.actionStatusAtom) === status) {
                    globalStore.set(this.actionStatusAtom, null);
                }
            }, 3000);
        }
    }

    clearActionStatus() {
        globalStore.set(this.actionStatusAtom, null);
    }

    async runAction(label: string, fn: () => Promise<GitActionResult>): Promise<boolean> {
        globalStore.set(this.actionBusyAtom, true);
        try {
            const res = await fn();
            if (res?.success) {
                this.setActionStatus({ message: `${label} succeeded`, isError: false });
            } else {
                this.setActionStatus({ message: `${label} failed: ${res?.output ?? "unknown error"}`, isError: true });
            }
            await this.refreshStatus();
            return !!res?.success;
        } catch (e) {
            this.setActionStatus({ message: `${label} failed: ${String(e)}`, isError: true });
            return false;
        } finally {
            globalStore.set(this.actionBusyAtom, false);
        }
    }

    // ---- branch switcher (F2) ----

    toggleBranchSwitcher() {
        const open = globalStore.get(this.branchSwitcherOpenAtom);
        globalStore.set(this.branchSwitcherOpenAtom, !open);
        if (!open) {
            globalStore.set(this.branchFilterAtom, "");
            this.refreshBranches();
        }
    }

    closeBranchSwitcher() {
        globalStore.set(this.branchSwitcherOpenAtom, false);
        globalStore.set(this.branchFilterAtom, "");
    }

    async checkout(branch: string, create = false) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        this.closeBranchSwitcher();
        const ok = await this.runAction(`Checkout ${branch}`, () =>
            this.env.rpc.RemoteGitCheckoutCommand(
                TabRpcClient,
                { gitroot: root, branch, create },
                { route: this.getRoute() }
            )
        );
        if (ok) {
            await Promise.all([this.refreshBranches(), this.refreshLog(true)]);
        }
    }

    // ---- staging & commit (F3) ----

    async stage(paths: string[]) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        await this.runAction("Stage", () =>
            this.env.rpc.RemoteGitStageCommand(TabRpcClient, { gitroot: root, paths }, { route: this.getRoute() })
        );
    }

    async unstage(paths: string[]) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        await this.runAction("Unstage", () =>
            this.env.rpc.RemoteGitUnstageCommand(TabRpcClient, { gitroot: root, paths }, { route: this.getRoute() })
        );
    }

    async commit() {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const message = globalStore.get(this.commitMessageAtom);
        const amend = globalStore.get(this.commitAmendAtom);
        if (isBlank(message) && !amend) {
            this.setActionStatus({ message: "Commit message is empty", isError: true });
            return;
        }
        const ok = await this.runAction("Commit", () =>
            this.env.rpc.RemoteGitCommitCommand(
                TabRpcClient,
                { gitroot: root, message, amend },
                { route: this.getRoute() }
            )
        );
        if (ok) {
            globalStore.set(this.commitMessageAtom, "");
            globalStore.set(this.commitAmendAtom, false);
            await this.refreshLog(true);
        }
    }

    // ---- sync & stash (F4) ----

    async sync(action: "pull" | "push" | "fetch", setUpstream = false) {
        if (action === "push") {
            return this.push(setUpstream);
        }
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const ok = await this.runAction(action, () =>
            this.env.rpc.RemoteGitSyncCommand(
                TabRpcClient,
                { gitroot: root, action, setupstream: setUpstream },
                { route: this.getRoute() }
            )
        );
        if (ok) {
            await Promise.all([this.refreshBranches(), this.refreshLog(true)]);
        }
    }

    // ---- push with credential handling ----

    async push(setUpstream = false) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const res = await this.runPush(root, setUpstream, null);
        if (res?.success || !res?.authrequired) {
            return;
        }
        // auth needed: try credentials stored for this host, else prompt
        const host = res.authhost || "";
        const stored = await this.loadStoredCreds(host);
        if (stored != null) {
            const retry = await this.runPush(root, setUpstream, stored);
            if (retry?.success) {
                return;
            }
        }
        this.pendingPushUpstream = setUpstream;
        globalStore.set(this.authHostAtom, host);
        globalStore.set(this.authUsernameAtom, stored?.username ?? "");
        globalStore.set(this.authErrorAtom, stored != null ? "Stored credentials were rejected" : null);
        globalStore.set(this.authOpenAtom, true);
    }

    async submitPushAuth(username: string, token: string, save: boolean) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        globalStore.set(this.authBusyAtom, true);
        try {
            const res = await this.runPush(root, this.pendingPushUpstream, { username, token });
            if (res?.success) {
                if (save) {
                    await this.saveStoredCreds(globalStore.get(this.authHostAtom), username, token);
                }
                this.closePushAuth();
            } else if (res?.authrequired) {
                globalStore.set(this.authErrorAtom, "Authentication failed — check your username and token");
            } else {
                globalStore.set(this.authErrorAtom, res?.output ?? "Push failed");
            }
        } finally {
            globalStore.set(this.authBusyAtom, false);
        }
    }

    closePushAuth() {
        globalStore.set(this.authOpenAtom, false);
        globalStore.set(this.authErrorAtom, null);
    }

    private async runPush(
        root: string,
        setUpstream: boolean,
        creds: { username: string; token: string }
    ): Promise<GitActionResult> {
        globalStore.set(this.actionBusyAtom, true);
        try {
            const res = await this.env.rpc.RemoteGitSyncCommand(
                TabRpcClient,
                {
                    gitroot: root,
                    action: "push",
                    setupstream: setUpstream,
                    username: creds?.username,
                    token: creds?.token,
                },
                { route: this.getRoute() }
            );
            if (res?.success) {
                this.setActionStatus({ message: "push succeeded", isError: false });
                await Promise.all([this.refreshBranches(), this.refreshLog(true)]);
            } else if (!res?.authrequired) {
                this.setActionStatus({ message: `push failed: ${res?.output ?? "unknown error"}`, isError: true });
                await this.refreshStatus();
            }
            return res;
        } catch (e) {
            this.setActionStatus({ message: `push failed: ${String(e)}`, isError: true });
            return { success: false } as GitActionResult;
        } finally {
            globalStore.set(this.actionBusyAtom, false);
        }
    }

    private async loadStoredCreds(host: string): Promise<{ username: string; token: string }> {
        if (isBlank(host)) {
            return null;
        }
        try {
            const uKey = gitSecretKey(host, "username");
            const tKey = gitSecretKey(host, "token");
            const secrets = await RpcApi.GetSecretsCommand(TabRpcClient, [uKey, tKey]);
            const username = secrets?.[uKey];
            const token = secrets?.[tKey];
            if (!isBlank(username) && !isBlank(token)) {
                return { username, token };
            }
        } catch (e) {
            console.error("git: failed to load stored credentials", e);
        }
        return null;
    }

    private async saveStoredCreds(host: string, username: string, token: string) {
        if (isBlank(host)) {
            return;
        }
        try {
            await RpcApi.SetSecretsCommand(TabRpcClient, {
                [gitSecretKey(host, "username")]: username,
                [gitSecretKey(host, "token")]: token,
            });
        } catch (e) {
            console.error("git: failed to store credentials", e);
        }
    }

    async stash(action: "push" | "pop" | "apply" | "drop", index = 0, message = "") {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        await this.runAction(`Stash ${action}`, () =>
            this.env.rpc.RemoteGitStashCommand(
                TabRpcClient,
                { gitroot: root, action, index, message },
                { route: this.getRoute() }
            )
        );
    }

    // ---- inline diff (F5) ----

    async openDiff(file: GitFileStatus) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        const staged = file.staged && !file.unstaged;
        globalStore.set(this.diffLoadingAtom, true);
        globalStore.set(this.diffAtom, { path: file.path, diff: "" });
        globalStore.set(this.diffFileAtom, file);
        globalStore.set(this.diffStagedAtom, staged);
        try {
            // normal context (not full-file) so the diff splits into real hunks that
            // can be staged/unstaged individually via RemoteGitApplyHunkCommand
            const diff = await this.env.rpc.RemoteGitDiffCommand(
                TabRpcClient,
                { gitroot: root, path: file.path, staged, fullcontext: false, untracked: !!file.untracked },
                { route: this.getRoute() }
            );
            if (!this.disposed) {
                globalStore.set(this.diffAtom, diff);
            }
        } catch (e) {
            if (!this.disposed) {
                globalStore.set(this.diffAtom, { path: file.path, diff: `Error: ${String(e)}` });
            }
        } finally {
            globalStore.set(this.diffLoadingAtom, false);
        }
    }

    closeDiff() {
        globalStore.set(this.diffAtom, null);
        globalStore.set(this.diffFileAtom, null);
        globalStore.set(this.reviewActiveAtom, false);
        globalStore.set(this.reviewFilesAtom, []);
    }

    // ---- multi-file review ----

    async openReview(files: GitFileStatus[]) {
        if (files == null || files.length === 0) {
            return;
        }
        globalStore.set(this.reviewFilesAtom, files);
        globalStore.set(this.reviewIndexAtom, 0);
        globalStore.set(this.reviewActiveAtom, true);
        await this.openDiff(files[0]);
    }

    async reviewGoto(index: number) {
        const files = globalStore.get(this.reviewFilesAtom);
        if (index < 0 || index >= files.length) {
            return;
        }
        globalStore.set(this.reviewIndexAtom, index);
        await this.openDiff(files[index]);
    }

    reviewNext() {
        return this.reviewGoto(globalStore.get(this.reviewIndexAtom) + 1);
    }

    reviewPrev() {
        return this.reviewGoto(globalStore.get(this.reviewIndexAtom) - 1);
    }

    // Stage or unstage a single hunk (by index into the currently-shown diff), then
    // reload the same diff side so the remaining hunks re-index; close it once that
    // side has no changes left.
    async applyHunk(hunkIndex: number) {
        const root = globalStore.get(this.gitRootAtom);
        const file = globalStore.get(this.diffFileAtom);
        if (isBlank(root) || file == null) {
            return;
        }
        const unstage = globalStore.get(this.diffStagedAtom);
        const ok = await this.runAction(unstage ? "Unstage hunk" : "Stage hunk", () =>
            this.env.rpc.RemoteGitApplyHunkCommand(
                TabRpcClient,
                { gitroot: root, path: file.path, hunkindex: hunkIndex, unstage },
                { route: this.getRoute() }
            )
        );
        if (ok && !this.disposed) {
            await this.reloadDiffSide();
        }
    }

    private async reloadDiffSide() {
        const root = globalStore.get(this.gitRootAtom);
        const file = globalStore.get(this.diffFileAtom);
        if (isBlank(root) || file == null) {
            return;
        }
        const staged = globalStore.get(this.diffStagedAtom);
        globalStore.set(this.diffLoadingAtom, true);
        try {
            const diff = await this.env.rpc.RemoteGitDiffCommand(
                TabRpcClient,
                { gitroot: root, path: file.path, staged, fullcontext: false, untracked: !!file.untracked },
                { route: this.getRoute() }
            );
            if (this.disposed) {
                return;
            }
            const reviewing = globalStore.get(this.reviewActiveAtom);
            if (isBlank(diff?.diff)) {
                // in review mode keep the panel (as "no changes") so the file
                // stepper stays; otherwise the diff is done, close it
                if (reviewing) {
                    globalStore.set(this.diffAtom, { path: file.path, diff: "" });
                } else {
                    this.closeDiff();
                }
            } else {
                globalStore.set(this.diffAtom, diff);
            }
        } catch (e) {
            if (!this.disposed && !globalStore.get(this.reviewActiveAtom)) {
                this.closeDiff();
            }
        } finally {
            globalStore.set(this.diffLoadingAtom, false);
        }
    }

    // ---- discard (F5) ----

    async discard(paths: string[]) {
        const root = globalStore.get(this.gitRootAtom);
        if (isBlank(root)) {
            return;
        }
        await this.runAction("Discard", () =>
            this.env.rpc.RemoteGitDiscardCommand(TabRpcClient, { gitroot: root, paths }, { route: this.getRoute() })
        );
    }

    dispose() {
        this.disposed = true;
        if (this.cwdUnsub) {
            this.cwdUnsub();
            this.cwdUnsub = null;
        }
        if (this.connStatusUnsub) {
            this.connStatusUnsub();
            this.connStatusUnsub = null;
        }
        if (this.cancelPoll) {
            this.cancelPoll();
            this.cancelPoll = null;
        }
    }
}
