// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { MetaKeyAtomFnType, WaveEnv, WaveEnvSubset } from "@/app/waveenv/waveenv";

export type GitEnv = WaveEnvSubset<{
    rpc: {
        RemoteGitRepoInfoCommand: WaveEnv["rpc"]["RemoteGitRepoInfoCommand"];
        RemoteGitStatusCommand: WaveEnv["rpc"]["RemoteGitStatusCommand"];
        RemoteGitBranchesCommand: WaveEnv["rpc"]["RemoteGitBranchesCommand"];
        RemoteGitLogCommand: WaveEnv["rpc"]["RemoteGitLogCommand"];
        RemoteGitCheckoutCommand: WaveEnv["rpc"]["RemoteGitCheckoutCommand"];
        RemoteGitStageCommand: WaveEnv["rpc"]["RemoteGitStageCommand"];
        RemoteGitUnstageCommand: WaveEnv["rpc"]["RemoteGitUnstageCommand"];
        RemoteGitCommitCommand: WaveEnv["rpc"]["RemoteGitCommitCommand"];
        RemoteGitDiscardCommand: WaveEnv["rpc"]["RemoteGitDiscardCommand"];
        RemoteGitSyncCommand: WaveEnv["rpc"]["RemoteGitSyncCommand"];
        RemoteGitStashCommand: WaveEnv["rpc"]["RemoteGitStashCommand"];
        RemoteGitDiffCommand: WaveEnv["rpc"]["RemoteGitDiffCommand"];
    };
    getConnStatusAtom: WaveEnv["getConnStatusAtom"];
    getBlockMetaKeyAtom: MetaKeyAtomFnType<"connection" | "git:root" | "cmd:cwd" | "file">;
}>;
