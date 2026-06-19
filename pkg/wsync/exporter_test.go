// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"testing"
)

func TestStampItemsUnchangedKeepsMtime(t *testing.T) {
	prev := map[string]SyncItem{
		"workspace:w1": {Key: "workspace:w1", Mtime: 100, InstallId: "mac", Data: json.RawMessage(`{"name":"a"}`)},
	}
	current := map[string]json.RawMessage{"workspace:w1": json.RawMessage(`{"name":"a"}`)}
	out := StampItems(current, prev, "mac", 999)
	if out["workspace:w1"].Mtime != 100 {
		t.Fatalf("unchanged item should keep mtime 100, got %d", out["workspace:w1"].Mtime)
	}
}

func TestStampItemsChangedRestamps(t *testing.T) {
	prev := map[string]SyncItem{
		"workspace:w1": {Key: "workspace:w1", Mtime: 100, InstallId: "mac", Data: json.RawMessage(`{"name":"a"}`)},
	}
	current := map[string]json.RawMessage{"workspace:w1": json.RawMessage(`{"name":"b"}`)}
	out := StampItems(current, prev, "mac", 999)
	if out["workspace:w1"].Mtime != 999 || out["workspace:w1"].InstallId != "mac" {
		t.Fatalf("changed item should restamp to 999, got %+v", out["workspace:w1"])
	}
}

func TestStampItemsNewIsStamped(t *testing.T) {
	current := map[string]json.RawMessage{"tab:t9": json.RawMessage(`{}`)}
	out := StampItems(current, map[string]SyncItem{}, "linux", 555)
	if out["tab:t9"].Mtime != 555 || out["tab:t9"].Deleted {
		t.Fatalf("new item should be stamped live at 555, got %+v", out["tab:t9"])
	}
}

func TestStampItemsGoneBecomesTombstone(t *testing.T) {
	prev := map[string]SyncItem{
		"block:b1": {Key: "block:b1", Mtime: 100, InstallId: "mac", Data: json.RawMessage(`{}`)},
	}
	out := StampItems(map[string]json.RawMessage{}, prev, "mac", 999)
	got := out["block:b1"]
	if !got.Deleted || got.Mtime != 999 {
		t.Fatalf("removed item should become a fresh tombstone at 999, got %+v", got)
	}
	if got.Data != nil {
		t.Fatalf("tombstone should carry no data")
	}
}

func TestStampItemsExistingTombstonePreserved(t *testing.T) {
	prev := map[string]SyncItem{
		"block:b1": {Key: "block:b1", Mtime: 50, InstallId: "mac", Deleted: true},
	}
	out := StampItems(map[string]json.RawMessage{}, prev, "mac", 999)
	if out["block:b1"].Mtime != 50 {
		t.Fatalf("existing tombstone should keep mtime 50, got %d", out["block:b1"].Mtime)
	}
}
