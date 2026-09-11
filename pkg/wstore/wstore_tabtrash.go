// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wstore

import (
	"context"
	"fmt"
	"time"

	"github.com/wavetermdev/waveterm/pkg/util/dbutil"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
)

const TabTrashMaxPerWorkspace = 10
const TabTrashMaxAge = 7 * 24 * time.Hour

// trashRowCols keeps the column list in one place; every read scans a full tabTrashRow.
const trashRowCols = "tabid, workspaceid, name, seq, tabidx, closedat, data"

// TabTrashSnapshot is everything needed to bring a closed tab back: the tab row,
// its layout tree, and every block that was in it. The blocks' filestore zones
// (terminal scrollback) are deliberately left in place while a snapshot exists and
// are only dropped once the snapshot is evicted.
type TabTrashSnapshot struct {
	Tab    *waveobj.Tab         `json:"tab"`
	Layout *waveobj.LayoutState `json:"layout"`
	Blocks []*waveobj.Block     `json:"blocks"`
	TabIdx int                  `json:"tabidx"`
}

type ClosedTabInfo struct {
	TabId       string `json:"tabid"`
	WorkspaceId string `json:"workspaceid"`
	Name        string `json:"name"`
	BlockCount  int    `json:"blockcount"`
	ClosedAt    int64  `json:"closedat"`
}

type tabTrashRow struct {
	TabId       string `db:"tabid"`
	WorkspaceId string `db:"workspaceid"`
	Name        string `db:"name"`
	Seq         int64  `db:"seq"`
	TabIdx      int    `db:"tabidx"`
	ClosedAt    int64  `db:"closedat"`
	Data        string `db:"data"`
}

func (row tabTrashRow) toSnapshot() (*TabTrashSnapshot, error) {
	var snapshot TabTrashSnapshot
	if err := dbutil.QuickScanJson(&snapshot, row.Data); err != nil {
		return nil, fmt.Errorf("error decoding tab trash snapshot %s: %w", row.TabId, err)
	}
	snapshot.TabIdx = row.TabIdx
	return &snapshot, nil
}

func (row tabTrashRow) toInfo() ClosedTabInfo {
	info := ClosedTabInfo{
		TabId:       row.TabId,
		WorkspaceId: row.WorkspaceId,
		Name:        row.Name,
		ClosedAt:    row.ClosedAt,
	}
	snapshot, err := row.toSnapshot()
	if err == nil && snapshot.Tab != nil {
		info.BlockCount = len(snapshot.Tab.BlockIds)
	}
	return info
}

// TabTrashPut stores a snapshot of a just-closed tab. A tab id already in the trash
// is replaced, so re-closing a restored tab keeps a single entry.
func TabTrashPut(ctx context.Context, workspaceId string, snapshot *TabTrashSnapshot) error {
	if snapshot == nil || snapshot.Tab == nil {
		return fmt.Errorf("cannot trash a nil tab snapshot")
	}
	return WithTx(ctx, func(tx *TxWrap) error {
		// seq is a monotonic close counter: closedat has millisecond resolution, so two tabs
		// closed in the same millisecond would have no defined order for undo or eviction.
		query := `INSERT OR REPLACE INTO db_tab_trash (tabid, workspaceid, name, seq, tabidx, closedat, data)
		          VALUES (?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM db_tab_trash), ?, ?, ?)`
		tx.Exec(query, snapshot.Tab.OID, workspaceId, snapshot.Tab.Name, snapshot.TabIdx,
			time.Now().UnixMilli(), dbutil.QuickJson(snapshot))
		return nil
	})
}

// TabTrashList returns the workspace's closed tabs, most recently closed first.
func TabTrashList(ctx context.Context, workspaceId string) ([]ClosedTabInfo, error) {
	return WithTxRtn(ctx, func(tx *TxWrap) ([]ClosedTabInfo, error) {
		query := `SELECT ` + trashRowCols + ` FROM db_tab_trash
		          WHERE workspaceid = ? ORDER BY seq DESC`
		var rows []tabTrashRow
		tx.Select(&rows, query, workspaceId)
		rtn := make([]ClosedTabInfo, 0, len(rows))
		for _, row := range rows {
			rtn = append(rtn, row.toInfo())
		}
		return rtn, nil
	})
}

// TabTrashTake removes a snapshot from the trash and returns it. An empty tabId takes
// the most recently closed tab of the workspace. Returns nil when the trash is empty.
func TabTrashTake(ctx context.Context, workspaceId string, tabId string) (*TabTrashSnapshot, error) {
	return WithTxRtn(ctx, func(tx *TxWrap) (*TabTrashSnapshot, error) {
		var row tabTrashRow
		var found bool
		if tabId == "" {
			query := `SELECT ` + trashRowCols + ` FROM db_tab_trash
			          WHERE workspaceid = ? ORDER BY seq DESC LIMIT 1`
			found = tx.Get(&row, query, workspaceId)
		} else {
			query := `SELECT ` + trashRowCols + ` FROM db_tab_trash
			          WHERE workspaceid = ? AND tabid = ?`
			found = tx.Get(&row, query, workspaceId, tabId)
		}
		if !found {
			return nil, nil
		}
		tx.Exec("DELETE FROM db_tab_trash WHERE tabid = ?", row.TabId)
		return row.toSnapshot()
	})
}

// TabTrashEvict drops the snapshots that have aged out or fallen past the per-workspace
// limit and returns them, so their filestore zones can be released.
func TabTrashEvict(ctx context.Context, workspaceId string) ([]*TabTrashSnapshot, error) {
	cutoff := time.Now().Add(-TabTrashMaxAge).UnixMilli()
	return WithTxRtn(ctx, func(tx *TxWrap) ([]*TabTrashSnapshot, error) {
		query := `SELECT ` + trashRowCols + ` FROM db_tab_trash
		          WHERE workspaceid = ? AND (closedat < ? OR tabid NOT IN (
		              SELECT tabid FROM db_tab_trash WHERE workspaceid = ? ORDER BY seq DESC LIMIT ?
		          )) ORDER BY seq`
		var rows []tabTrashRow
		tx.Select(&rows, query, workspaceId, cutoff, workspaceId, TabTrashMaxPerWorkspace)
		return deleteTrashRows(tx, rows)
	})
}

// TabTrashEvictWorkspace drops every snapshot belonging to a workspace, for when the
// workspace itself goes away.
func TabTrashEvictWorkspace(ctx context.Context, workspaceId string) ([]*TabTrashSnapshot, error) {
	return WithTxRtn(ctx, func(tx *TxWrap) ([]*TabTrashSnapshot, error) {
		query := `SELECT ` + trashRowCols + ` FROM db_tab_trash WHERE workspaceid = ? ORDER BY seq`
		var rows []tabTrashRow
		tx.Select(&rows, query, workspaceId)
		return deleteTrashRows(tx, rows)
	})
}

func deleteTrashRows(tx *TxWrap, rows []tabTrashRow) ([]*TabTrashSnapshot, error) {
	rtn := make([]*TabTrashSnapshot, 0, len(rows))
	for _, row := range rows {
		tx.Exec("DELETE FROM db_tab_trash WHERE tabid = ?", row.TabId)
		snapshot, err := row.toSnapshot()
		if err != nil {
			continue
		}
		rtn = append(rtn, snapshot)
	}
	return rtn, nil
}
