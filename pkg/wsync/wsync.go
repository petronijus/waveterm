// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Package wsync implements cross-machine sync of Wave config + workspace state.
// Each install publishes its OWN state file (state.<installid>.json) to shared
// storage; every install then merges the union of all such files, resolving each
// item by last-write-wins on a wall-clock mtime. Single-writer-per-file is what
// lets this work over a dumb backend (WebDAV) with no locking: no two machines
// ever write the same file, so there are no write conflicts to resolve — only a
// deterministic read-side merge.
package wsync

import "encoding/json"

const (
	// SyncItem.Key prefixes — stable identity for an item across machines.
	KeyPrefixConfig    = "config:"    // config:<relpath>, e.g. "config:settings.json"
	KeyPrefixWorkspace = "workspace:" // workspace:<oid>
	KeyPrefixTab       = "tab:"       // tab:<oid>
	KeyPrefixBlock     = "block:"     // block:<oid>
	KeyPrefixLayout    = "layout:"    // layout:<oid>

	// reconciliation ops returned by Reconcile
	OpUpsert = "upsert"
	OpDelete = "delete"
)

// SyncItem is one syncable unit (a config file or a DB object), stamped for LWW.
type SyncItem struct {
	Key       string          `json:"key"`
	Mtime     int64           `json:"mtime"`             // wall-clock unix-milli of last local change
	InstallId string          `json:"installid"`         // install that last wrote it (tiebreak)
	Deleted   bool            `json:"deleted,omitempty"` // tombstone
	Data      json.RawMessage `json:"data,omitempty"`    // omitted when Deleted
}

// InstallState is the full payload one machine publishes (its state.<installid>.json).
type InstallState struct {
	InstallId string              `json:"installid"`
	PushedTs  int64               `json:"pushedts"`
	Items     map[string]SyncItem `json:"items"`
}

// ApplyAction is a reconciliation step for the local store.
type ApplyAction struct {
	Op   string // OpUpsert | OpDelete
	Item SyncItem
}

// itemWins reports whether a should win over b under last-write-wins: newer mtime
// wins; ties are broken deterministically by installid so every machine, given the
// same set of states, converges on the same winner regardless of merge order.
func itemWins(a, b SyncItem) bool {
	if a.Mtime != b.Mtime {
		return a.Mtime > b.Mtime
	}
	return a.InstallId > b.InstallId
}

// MergeStates unions per-install states into the winning item per key. Tombstones
// participate in the merge (a delete with a newer mtime beats a stale live copy),
// so deletions propagate instead of being resurrected by a machine that still has
// the old live copy.
func MergeStates(states []InstallState) map[string]SyncItem {
	winners := make(map[string]SyncItem)
	for _, st := range states {
		for key, item := range st.Items {
			cur, ok := winners[key]
			if !ok || itemWins(item, cur) {
				winners[key] = item
			}
		}
	}
	return winners
}

// Reconcile compares merged winners against the current local items and returns
// the actions needed to bring local into line: upsert when the winner is new or
// newer, delete when a winning tombstone is newer than a live local copy. Items
// where local is same-or-newer are left untouched (local will republish them on
// the next push), and tombstones for things we don't have are no-ops.
func Reconcile(winners map[string]SyncItem, local map[string]SyncItem) []ApplyAction {
	var actions []ApplyAction
	for key, win := range winners {
		cur, ok := local[key]
		if ok && !itemWins(win, cur) {
			continue // local is the same or newer — nothing to do
		}
		if win.Deleted {
			if ok && !cur.Deleted {
				actions = append(actions, ApplyAction{Op: OpDelete, Item: win})
			}
			continue
		}
		actions = append(actions, ApplyAction{Op: OpUpsert, Item: win})
	}
	return actions
}
