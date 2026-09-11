// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wcore

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/wavetermdev/waveterm/pkg/filestore"
	"github.com/wavetermdev/waveterm/pkg/panichandler"
	"github.com/wavetermdev/waveterm/pkg/util/utilfn"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

const TabTrashZoneDeleteTimeout = 30 * time.Second

// UndoCloseTab restores a tab from the tab trash. An empty tabId restores the most
// recently closed tab of the workspace. Returns nil when there is nothing to restore.
func UndoCloseTab(ctx context.Context, workspaceId string, tabId string) (*waveobj.Tab, error) {
	ws, err := wstore.DBMustGet[*waveobj.Workspace](ctx, workspaceId)
	if err != nil {
		return nil, fmt.Errorf("workspace not found %q: %w", workspaceId, err)
	}
	snapshot, err := wstore.TabTrashTake(ctx, workspaceId, tabId)
	if err != nil {
		return nil, fmt.Errorf("error reading the tab trash: %w", err)
	}
	if snapshot == nil || snapshot.Tab == nil {
		return nil, nil
	}
	restoredId := snapshot.Tab.OID
	if utilfn.FindStringInSlice(ws.TabIds, restoredId) != -1 {
		return nil, fmt.Errorf("tab %s is already open in workspace %s", restoredId, workspaceId)
	}
	// one transaction: a half-restored tab would leave block rows nothing points at
	err = wstore.WithTx(ctx, func(tx *wstore.TxWrap) error {
		if snapshot.Layout != nil {
			err := wstore.DBInsert(tx.Context(), snapshot.Layout)
			if err != nil {
				return fmt.Errorf("error restoring layout for tab %s: %w", restoredId, err)
			}
		}
		for _, block := range snapshot.Blocks {
			err := wstore.DBInsert(tx.Context(), block)
			if err != nil {
				return fmt.Errorf("error restoring block %s: %w", block.OID, err)
			}
		}
		err := wstore.DBInsert(tx.Context(), snapshot.Tab)
		if err != nil {
			return fmt.Errorf("error restoring tab %s: %w", restoredId, err)
		}
		ws.TabIds = slices.Insert(ws.TabIds, min(max(snapshot.TabIdx, 0), len(ws.TabIds)), restoredId)
		ws.ActiveTabId = restoredId
		err = wstore.DBUpdate(tx.Context(), ws)
		if err != nil {
			return fmt.Errorf("error updating workspace %s: %w", workspaceId, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("UndoCloseTab: restored tab %s (%q) with %d blocks\n", restoredId, snapshot.Tab.Name, len(snapshot.Blocks))
	return snapshot.Tab, nil
}

// ListClosedTabs returns the workspace's restorable tabs, most recently closed first.
func ListClosedTabs(ctx context.Context, workspaceId string) ([]wstore.ClosedTabInfo, error) {
	return wstore.TabTrashList(ctx, workspaceId)
}

func trashTab(ctx context.Context, workspaceId string, tab *waveobj.Tab, tabIdx int) error {
	snapshot := &wstore.TabTrashSnapshot{Tab: tab, TabIdx: tabIdx}
	if tab.LayoutState != "" {
		layout, err := wstore.DBGet[*waveobj.LayoutState](ctx, tab.LayoutState)
		if err != nil {
			return fmt.Errorf("error reading layout %s: %w", tab.LayoutState, err)
		}
		snapshot.Layout = layout
	}
	blocks, err := collectBlockTree(ctx, tab.BlockIds)
	if err != nil {
		return err
	}
	snapshot.Blocks = blocks
	err = wstore.TabTrashPut(ctx, workspaceId, snapshot)
	if err != nil {
		return fmt.Errorf("error writing the tab trash: %w", err)
	}
	evictTabTrash(ctx, workspaceId)
	return nil
}

// collectBlockTree flattens blocks and their subblocks, so a restore puts back exactly what
// DeleteBlock's recursive descent takes away.
func collectBlockTree(ctx context.Context, blockIds []string) ([]*waveobj.Block, error) {
	var rtn []*waveobj.Block
	for _, blockId := range blockIds {
		block, err := wstore.DBGet[*waveobj.Block](ctx, blockId)
		if err != nil {
			return nil, fmt.Errorf("error reading block %s: %w", blockId, err)
		}
		if block == nil {
			continue
		}
		rtn = append(rtn, block)
		if len(block.SubBlockIds) == 0 {
			continue
		}
		subBlocks, err := collectBlockTree(ctx, block.SubBlockIds)
		if err != nil {
			return nil, err
		}
		rtn = append(rtn, subBlocks...)
	}
	return rtn, nil
}

func evictTabTrash(ctx context.Context, workspaceId string) {
	evicted, err := wstore.TabTrashEvict(ctx, workspaceId)
	if err != nil {
		log.Printf("error evicting the tab trash for workspace %s: %v\n", workspaceId, err)
		return
	}
	deleteSnapshotZones(evicted)
}

func evictWorkspaceTabTrash(ctx context.Context, workspaceId string) {
	evicted, err := wstore.TabTrashEvictWorkspace(ctx, workspaceId)
	if err != nil {
		log.Printf("error clearing the tab trash for workspace %s: %v\n", workspaceId, err)
		return
	}
	deleteSnapshotZones(evicted)
}

// deleteSnapshotZones releases the blockfiles a snapshot was keeping alive. It runs off the
// caller's context because eviction happens inside a tab close, and the zone deletes must
// not ride that transaction's connection or its deadline.
func deleteSnapshotZones(snapshots []*wstore.TabTrashSnapshot) {
	var blockIds []string
	for _, snapshot := range snapshots {
		for _, block := range snapshot.Blocks {
			blockIds = append(blockIds, block.OID)
		}
	}
	if len(blockIds) == 0 {
		return
	}
	go func() {
		defer func() {
			panichandler.PanicHandler("wcore:deleteSnapshotZones", recover())
		}()
		deleteCtx, cancelFn := context.WithTimeout(context.Background(), TabTrashZoneDeleteTimeout)
		defer cancelFn()
		for _, blockId := range blockIds {
			err := filestore.WFS.DeleteZone(deleteCtx, blockId)
			if err != nil {
				log.Printf("error deleting filestore zone %s for an evicted tab: %v\n", blockId, err)
			}
		}
	}()
}
