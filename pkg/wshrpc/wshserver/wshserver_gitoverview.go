// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshserver

import (
	"context"
	"fmt"

	"github.com/wavetermdev/waveterm/pkg/gitoverview"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

func (ws *WshServer) GitOverviewStartCommand(ctx context.Context, data wshrpc.CommandGitOverviewStartData) (wshrpc.GitOverviewData, error) {
	if data.BlockId == "" {
		return wshrpc.GitOverviewData{}, fmt.Errorf("blockid is required")
	}
	return gitoverview.Start(data), nil
}

func (ws *WshServer) GitOverviewSyncCommand(ctx context.Context, data wshrpc.CommandGitOverviewSyncData) error {
	gitoverview.Sync(data)
	return nil
}

func (ws *WshServer) GitOverviewRefreshCommand(ctx context.Context, data wshrpc.CommandGitOverviewBlockData) error {
	gitoverview.Refresh(data.BlockId)
	return nil
}
