// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/wavetermdev/waveterm/pkg/util/utilfn"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wcore"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// Duplicating a tab is save+load without the round trip through disk, so it lives next to
// the layout snapshot code rather than in wcore: it reuses remapLayoutTree, and wcore
// cannot import this package anyway (wsync depends on wcore, not the other way round).
//
// Unlike a saved layout, this never portablizes paths — the copy stays on this machine, so
// block meta is taken verbatim. That also keeps per-block history, which portablize strips.

const duplicateTabNameSuffix = " copy"

// DuplicateTab creates a copy of tabId in the same workspace: same arrangement, same block
// settings, placed immediately to the right of the original. Returns the new tab id.
//
// Deliberately not copied: terminal scrollback (it lives in each block's own filestore
// zone, keyed by block id) and running processes. A duplicated block starts a fresh shell
// in the same cwd. A `term:durable` block copies the flag but not the job, so it becomes a
// new durable session rather than a second view of the original.
func DuplicateTab(ctx context.Context, tabId string) (string, error) {
	tab, err := wstore.DBGet[*waveobj.Tab](ctx, tabId)
	if err != nil {
		return "", fmt.Errorf("getting tab: %w", err)
	}
	if tab == nil {
		return "", fmt.Errorf("tab %s not found", tabId)
	}
	workspaceId, err := wstore.DBFindWorkspaceForTabId(ctx, tabId)
	if err != nil {
		return "", fmt.Errorf("finding workspace for tab: %w", err)
	}
	ls, err := wstore.DBGet[*waveobj.LayoutState](ctx, tab.LayoutState)
	if err != nil {
		return "", fmt.Errorf("getting layout state: %w", err)
	}
	blocks := make(map[string]waveobj.MetaMapType, len(tab.BlockIds))
	for _, blockId := range tab.BlockIds {
		block, err := wstore.DBGet[*waveobj.Block](ctx, blockId)
		if err != nil {
			return "", fmt.Errorf("getting block %s: %w", blockId, err)
		}
		if block != nil {
			blocks[blockId] = block.Meta
		}
	}

	newTabId, err := wcore.CreateTab(ctx, workspaceId, duplicateTabName(tab.Name), true, false)
	if err != nil {
		return "", fmt.Errorf("creating tab: %w", err)
	}
	if err := placeTabAfter(ctx, workspaceId, newTabId, tabId); err != nil {
		// The tab exists and is usable; only its position is wrong, so don't fail the whole
		// operation over it.
		log.Printf("wsync: could not position duplicated tab: %v\n", err)
	}

	// A fresh tab is created with a starter terminal. Replacing the whole tree with
	// settree leaves that block orphaned, which is what CleanupOrphaned is for.
	if ls != nil && ls.RootNode != nil {
		if err := applyDuplicatedLayout(ctx, newTabId, ls, blocks); err != nil {
			return "", err
		}
	}
	if len(tab.Meta) > 0 {
		oref := waveobj.MakeORef(waveobj.OType_Tab, newTabId)
		if err := wstore.UpdateObjectMeta(ctx, oref, tab.Meta, false); err != nil {
			return "", fmt.Errorf("copying tab meta: %w", err)
		}
		wcore.SendWaveObjUpdate(oref)
	}
	log.Printf("wsync: duplicated tab %s -> %s\n", tabId, newTabId)
	return newTabId, nil
}

func duplicateTabName(name string) string {
	if name == "" {
		return ""
	}
	return name + duplicateTabNameSuffix
}

// placeTabAfter moves newTabId directly behind afterTabId in the workspace's tab order.
// CreateTab appends, which would drop the copy at the far end of the bar.
func placeTabAfter(ctx context.Context, workspaceId string, newTabId string, afterTabId string) error {
	ws, err := wcore.GetWorkspace(ctx, workspaceId)
	if err != nil {
		return fmt.Errorf("getting workspace: %w", err)
	}
	ids, err := insertTabAfter(ws.TabIds, newTabId, afterTabId)
	if err != nil {
		return err
	}
	return wcore.UpdateWorkspaceTabIds(ctx, workspaceId, ids)
}

// insertTabAfter returns tabIds with newTabId moved to directly behind afterTabId. Split
// out from the store call so the index arithmetic can be tested on its own.
func insertTabAfter(tabIds []string, newTabId string, afterTabId string) ([]string, error) {
	rtn := make([]string, 0, len(tabIds))
	for _, id := range tabIds {
		if id != newTabId {
			rtn = append(rtn, id)
		}
	}
	idx := utilfn.FindStringInSlice(rtn, afterTabId)
	if idx < 0 {
		return nil, fmt.Errorf("source tab %s not in workspace", afterTabId)
	}
	rtn = append(rtn, "")
	copy(rtn[idx+2:], rtn[idx+1:])
	rtn[idx+1] = newTabId
	return rtn, nil
}

func applyDuplicatedLayout(ctx context.Context, newTabId string, ls *waveobj.LayoutState, blocks map[string]waveobj.MetaMapType) error {
	raw, err := json.Marshal(ls.RootNode)
	if err != nil {
		return fmt.Errorf("reading layout tree: %w", err)
	}
	var savedRoot layoutNodeJSON
	if err := json.Unmarshal(raw, &savedRoot); err != nil {
		return fmt.Errorf("parsing layout tree: %w", err)
	}
	root, nodeIdMap, err := remapLayoutTree(savedRoot, func(oldBlockId string) (string, error) {
		meta := blocks[oldBlockId]
		if meta == nil {
			meta = waveobj.MetaMapType{}
		}
		block, err := wcore.CreateBlockWithTelemetry(ctx, newTabId, &waveobj.BlockDef{Meta: meta}, &waveobj.RuntimeOpts{}, false)
		if err != nil {
			return "", err
		}
		return block.OID, nil
	})
	if err != nil {
		return fmt.Errorf("recreating blocks: %w", err)
	}
	// settree rather than insertatindex: index paths resolve against the partially built
	// tree, which flattens nested splits (see remapLayoutTree's note).
	actions := []waveobj.LayoutActionData{
		{
			ActionType:      wcore.LayoutActionDataType_SetTree,
			RootNode:        root,
			FocusedNodeId:   nodeIdMap[ls.FocusedNodeId],
			MagnifiedNodeId: nodeIdMap[ls.MagnifiedNodeId],
		},
		{ActionType: wcore.LayoutActionDataType_CleanupOrphaned},
	}
	if err := wcore.QueueLayoutActionForTab(ctx, newTabId, actions...); err != nil {
		return fmt.Errorf("applying layout: %w", err)
	}
	return nil
}
