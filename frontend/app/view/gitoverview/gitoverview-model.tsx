// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { createBlock } from "@/app/store/global";
import { globalStore } from "@/app/store/jotaiStore";
import * as WOS from "@/app/store/wos";
import { waveEventSubscribeSingle } from "@/app/store/wps";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { MetaKeyAtomFnType, WaveEnv, WaveEnvSubset } from "@/app/waveenv/waveenv";
import { fireAndForget } from "@/util/util";
import * as jotai from "jotai";
import { syncTargets } from "./gitoverview-util";
import { GitOverviewView } from "./gitoverview-view";

export type GitOverviewEnv = WaveEnvSubset<{
    rpc: {
        GitOverviewStartCommand: WaveEnv["rpc"]["GitOverviewStartCommand"];
        GitOverviewSyncCommand: WaveEnv["rpc"]["GitOverviewSyncCommand"];
        GitOverviewRefreshCommand: WaveEnv["rpc"]["GitOverviewRefreshCommand"];
        SetMetaCommand: WaveEnv["rpc"]["SetMetaCommand"];
    };
    getBlockMetaKeyAtom: MetaKeyAtomFnType<
        "gitoverview:roots" | "gitoverview:maxdepth" | "gitoverview:ignore" | "gitoverview:fetchminutes"
    >;
}>;

type OverviewConfig = {
    roots: string[];
    maxdepth: number;
    ignore: string[];
    fetchminutes: number;
};

// The engine runs in wavesrv (pkg/gitoverview), one per block: it watches the
// file system, so this model only renders what the engine publishes and
// forwards clicks. Nothing here polls.
export class GitOverviewViewModel implements ViewModel {
    viewType: string;
    blockId: string;
    env: GitOverviewEnv;

    viewIcon = jotai.atom<string>("code-compare");
    viewName = jotai.atom<string>("Repos");
    noPadding = jotai.atom<boolean>(true);

    dataAtom: jotai.PrimitiveAtom<GitOverviewData>;
    errorAtom: jotai.PrimitiveAtom<string>;
    showInSyncAtom: jotai.PrimitiveAtom<boolean>;
    settingsOpenAtom: jotai.PrimitiveAtom<boolean>;
    // the pushes awaiting confirmation; null when no confirmation is shown
    confirmAtom: jotai.PrimitiveAtom<GitOverviewSyncTarget[]>;
    configAtom: jotai.Atom<OverviewConfig>;
    endIconButtons: jotai.Atom<IconButtonDecl[]>;

    disposed = false;
    configUnsub: (() => void) | null = null;
    eventUnsub: (() => void) | null = null;
    lastConfigSig: string = null;

    constructor({ blockId, waveEnv }: ViewModelInitType) {
        this.viewType = "gitoverview";
        this.blockId = blockId;
        this.env = waveEnv;

        this.dataAtom = jotai.atom<GitOverviewData>(null) as jotai.PrimitiveAtom<GitOverviewData>;
        this.errorAtom = jotai.atom<string>(null) as jotai.PrimitiveAtom<string>;
        this.showInSyncAtom = jotai.atom<boolean>(false);
        this.settingsOpenAtom = jotai.atom<boolean>(false);
        this.confirmAtom = jotai.atom<GitOverviewSyncTarget[]>(null) as jotai.PrimitiveAtom<GitOverviewSyncTarget[]>;
        this.configAtom = jotai.atom((get) => ({
            roots: get(this.env.getBlockMetaKeyAtom(blockId, "gitoverview:roots")) ?? [],
            maxdepth: get(this.env.getBlockMetaKeyAtom(blockId, "gitoverview:maxdepth")) ?? 0,
            ignore: get(this.env.getBlockMetaKeyAtom(blockId, "gitoverview:ignore")) ?? [],
            fetchminutes: get(this.env.getBlockMetaKeyAtom(blockId, "gitoverview:fetchminutes")) ?? 0,
        }));
        this.endIconButtons = jotai.atom((get) => {
            const data = get(this.dataAtom);
            const roots = get(this.configAtom).roots;
            const buttons: IconButtonDecl[] = [
                {
                    elemtype: "iconbutton",
                    icon: "folder-open",
                    title: "Folders to watch",
                    click: () => globalStore.set(this.settingsOpenAtom, !globalStore.get(this.settingsOpenAtom)),
                },
            ];
            if (roots.length > 0) {
                buttons.push({
                    elemtype: "iconbutton",
                    icon: "arrows-rotate",
                    title: "Rescan and fetch every repo now",
                    disabled: !!data?.fetching,
                    click: () => this.refresh(),
                });
            }
            return buttons;
        });

        this.eventUnsub = waveEventSubscribeSingle({
            eventType: "gitoverview",
            scope: WOS.makeORef("block", blockId),
            handler: (event) => {
                if (!this.disposed) {
                    globalStore.set(this.dataAtom, event.data);
                }
            },
        });
        this.configUnsub = globalStore.sub(this.configAtom, () => this.start());
        this.start();
    }

    get viewComponent(): ViewComponent {
        return GitOverviewView;
    }

    // start (re)configures the block's engine; an unchanged config is not
    // sent twice. Without roots nothing runs and the view asks for them.
    start() {
        if (this.disposed) {
            return;
        }
        const cfg = globalStore.get(this.configAtom);
        const sig = JSON.stringify(cfg);
        if (sig === this.lastConfigSig) {
            return;
        }
        this.lastConfigSig = sig;
        if (cfg.roots.length === 0) {
            globalStore.set(this.settingsOpenAtom, true);
            return;
        }
        fireAndForget(async () => {
            try {
                const data = await this.env.rpc.GitOverviewStartCommand(TabRpcClient, {
                    blockid: this.blockId,
                    roots: cfg.roots,
                    maxdepth: cfg.maxdepth || undefined,
                    ignore: cfg.ignore.length > 0 ? cfg.ignore : undefined,
                    fetchminutes: cfg.fetchminutes || undefined,
                });
                if (!this.disposed) {
                    globalStore.set(this.dataAtom, data);
                    globalStore.set(this.errorAtom, null);
                }
            } catch (e) {
                this.lastConfigSig = null;
                globalStore.set(this.errorAtom, String(e));
            }
        });
    }

    sync(targets: GitOverviewSyncTarget[]) {
        if (targets.length === 0) {
            return;
        }
        fireAndForget(() => this.env.rpc.GitOverviewSyncCommand(TabRpcClient, { blockid: this.blockId, targets }));
    }

    syncRow(repo: GitOverviewRepo) {
        if (repo.action === "pull" || repo.action === "push") {
            this.sync([{ path: repo.path, action: repo.action }]);
        }
    }

    // syncAll acts on the rows as rendered; anything that would push waits for
    // an explicit confirmation listing those repos.
    syncAll() {
        const { targets, pushes } = syncTargets(globalStore.get(this.dataAtom)?.repos);
        if (pushes.length > 0) {
            globalStore.set(this.confirmAtom, targets);
            return;
        }
        this.sync(targets);
    }

    confirmSyncAll(ok: boolean) {
        const targets = globalStore.get(this.confirmAtom);
        globalStore.set(this.confirmAtom, null);
        if (ok && targets) {
            this.sync(targets);
        }
    }

    refresh() {
        fireAndForget(() => this.env.rpc.GitOverviewRefreshCommand(TabRpcClient, { blockid: this.blockId }));
    }

    // openRepo opens the repo in the Git view next to this one.
    openRepo(repo: GitOverviewRepo) {
        fireAndForget(() => createBlock({ meta: { view: "git", "git:root": repo.path } }));
    }

    saveSettings(roots: string[], ignore: string[]) {
        fireAndForget(async () => {
            await this.env.rpc.SetMetaCommand(TabRpcClient, {
                oref: WOS.makeORef("block", this.blockId),
                meta: {
                    "gitoverview:roots": roots.length > 0 ? roots : null,
                    "gitoverview:ignore": ignore.length > 0 ? ignore : null,
                },
            });
            globalStore.set(this.settingsOpenAtom, roots.length === 0);
        });
    }

    dispose() {
        this.disposed = true;
        this.configUnsub?.();
        this.configUnsub = null;
        this.eventUnsub?.();
        this.eventUnsub = null;
    }
}
