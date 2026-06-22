// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { globalStore } from "@/app/store/jotaiStore";
import * as WOS from "@/app/store/wos";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { fireAndForget, isBlank, makeConnRoute } from "@/util/util";
import * as jotai from "jotai";
import { GitView } from "./git-view";
import { GitEnv } from "./gitenv";

const StatusPollIntervalMs = 4000;
const LogPageSize = 50;

export type GitActionStatus = {
    message: string;
    isError: boolean;
};

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

    connection: jotai.Atom<string>;
    connStatus: jotai.Atom<ConnStatus>;
    cwdSource: jotai.Atom<string>;
    rootOverrideAtom: jotai.PrimitiveAtom<string>;
    openPickerAtom: jotai.PrimitiveAtom<boolean>;
    viewText: jotai.Atom<HeaderElem[]>;
    endIconButtons: jotai.Atom<IconButtonDecl[]>;

    disposed = false;
    cancelPoll: (() => void) | null = null;
    fetchEpoch = 0;

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
        this.rootOverrideAtom = jotai.atom<string>(null) as jotai.PrimitiveAtom<string>;
        this.openPickerAtom = jotai.atom<boolean>(false);
        this.cwdSource = jotai.atom((get) => {
            // a path the user picked this session takes precedence immediately, before
            // the git:root meta write round-trips back through the object store
            const override = get(this.rootOverrideAtom);
            if (!isBlank(override)) {
                return override;
            }
            const gitRoot = get(this.env.getBlockMetaKeyAtom(blockId, "git:root"));
            if (!isBlank(gitRoot)) {
                return gitRoot;
            }
            const cmdCwd = get(this.env.getBlockMetaKeyAtom(blockId, "cmd:cwd"));
            if (!isBlank(cmdCwd)) {
                return cmdCwd;
            }
            const file = get(this.env.getBlockMetaKeyAtom(blockId, "file"));
            if (!isBlank(file)) {
                return file;
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

        this.endIconButtons = jotai.atom((get) => {
            const repoInfo = get(this.repoInfoAtom);
            const busy = get(this.actionBusyAtom);
            const buttons: IconButtonDecl[] = [];
            if (!repoInfo?.isrepo) {
                return buttons;
            }
            buttons.push({
                elemtype: "iconbutton",
                icon: "arrows-rotate",
                title: "Refresh",
                disabled: busy,
                click: () => this.refreshAll(),
            });
            return buttons;
        });

        this.startPolling();
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
        globalStore.set(this.loadingAtom, false);
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
                    await this.refreshStatus();
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
        globalStore.set(this.rootOverrideAtom, null);
        globalStore.set(this.loadingAtom, true);
        globalStore.set(this.errorAtom, null);
        this.startPolling();
    }

    // Point the git view at a new path (chosen via the in-app path picker) and
    // re-check. A repo there gets picked up; otherwise the (centered) "No git
    // repository" shows.
    async setRoot(picked: string) {
        globalStore.set(this.openPickerAtom, false);
        if (isBlank(picked)) {
            return;
        }
        globalStore.set(this.rootOverrideAtom, picked);
        // persist the choice on the block so it survives reloads
        fireAndForget(() =>
            this.env.rpc.SetMetaCommand(TabRpcClient, {
                oref: WOS.makeORef("block", this.blockId),
                meta: { "git:root": picked },
            })
        );
        // drop the previous repo's state so nothing stale lingers while we re-check
        globalStore.set(this.repoInfoAtom, null);
        globalStore.set(this.statusAtom, null);
        globalStore.set(this.branchesAtom, null);
        globalStore.set(this.logAtom, []);
        globalStore.set(this.gitRootAtom, null);
        globalStore.set(this.errorAtom, null);
        await this.refreshAll();
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
        try {
            const diff = await this.env.rpc.RemoteGitDiffCommand(
                TabRpcClient,
                { gitroot: root, path: file.path, staged, fullcontext: true, untracked: !!file.untracked },
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
        if (this.cancelPoll) {
            this.cancelPoll();
            this.cancelPoll = null;
        }
    }
}
