// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"testing"
)

func item(key string, mtime int64, install string, deleted bool) SyncItem {
	si := SyncItem{Key: key, Mtime: mtime, InstallId: install, Deleted: deleted}
	if !deleted {
		si.Data = json.RawMessage(`{}`)
	}
	return si
}

func state(install string, items ...SyncItem) InstallState {
	m := make(map[string]SyncItem)
	for _, it := range items {
		m[it.Key] = it
	}
	return InstallState{InstallId: install, Items: m}
}

func TestMergeNewerWins(t *testing.T) {
	a := state("mac", item("workspace:w1", 100, "mac", false))
	b := state("linux", item("workspace:w1", 200, "linux", false))
	got := MergeStates([]InstallState{a, b})
	if got["workspace:w1"].InstallId != "linux" {
		t.Fatalf("expected newer (linux) to win, got %q", got["workspace:w1"].InstallId)
	}
	// merge order must not matter
	got2 := MergeStates([]InstallState{b, a})
	if got2["workspace:w1"].InstallId != "linux" {
		t.Fatalf("merge not order-independent")
	}
}

func TestMergeTieBreakByInstallId(t *testing.T) {
	a := state("mac", item("tab:t1", 100, "mac", false))
	b := state("linux", item("tab:t1", 100, "linux", false))
	got := MergeStates([]InstallState{a, b})
	// equal mtime → deterministic winner = higher installid ("mac" > "linux")
	if got["tab:t1"].InstallId != "mac" {
		t.Fatalf("expected tie broken to mac, got %q", got["tab:t1"].InstallId)
	}
}

func TestTombstoneNewerDeletes(t *testing.T) {
	winners := MergeStates([]InstallState{
		state("mac", item("block:b1", 100, "mac", false)),
		state("linux", item("block:b1", 200, "linux", true)),
	})
	if !winners["block:b1"].Deleted {
		t.Fatalf("newer tombstone should win the merge")
	}
	local := map[string]SyncItem{"block:b1": item("block:b1", 100, "mac", false)}
	actions := Reconcile(winners, local)
	if len(actions) != 1 || actions[0].Op != OpDelete {
		t.Fatalf("expected one delete action, got %+v", actions)
	}
}

func TestTombstoneOlderKeepsLive(t *testing.T) {
	winners := MergeStates([]InstallState{
		state("mac", item("block:b1", 300, "mac", false)),
		state("linux", item("block:b1", 200, "linux", true)),
	})
	if winners["block:b1"].Deleted {
		t.Fatalf("older tombstone must not win over newer live copy")
	}
}

func TestReconcileUpsertAndNoop(t *testing.T) {
	winners := map[string]SyncItem{
		"config:settings.json": item("config:settings.json", 500, "linux", false),
		"workspace:w2":         item("workspace:w2", 100, "mac", false),
	}
	local := map[string]SyncItem{
		// local stale → should upsert
		"config:settings.json": item("config:settings.json", 400, "mac", false),
		// local already newest → no-op
		"workspace:w2": item("workspace:w2", 100, "mac", false),
	}
	actions := Reconcile(winners, local)
	if len(actions) != 1 || actions[0].Op != OpUpsert || actions[0].Item.Key != "config:settings.json" {
		t.Fatalf("expected single upsert of settings.json, got %+v", actions)
	}
}

func TestReconcileTombstoneForUnknownIsNoop(t *testing.T) {
	winners := map[string]SyncItem{"tab:gone": item("tab:gone", 100, "linux", true)}
	actions := Reconcile(winners, map[string]SyncItem{})
	if len(actions) != 0 {
		t.Fatalf("tombstone for an item we don't have should be a no-op, got %+v", actions)
	}
}
