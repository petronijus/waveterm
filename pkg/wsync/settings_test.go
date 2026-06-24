// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestStripSyncKeys verifies the transport config never travels with saved settings.
func TestStripSyncKeys(t *testing.T) {
	in := []byte(`{"app:theme":"dark","sync:folderpath":"/home/me/sync","sync:webdavurl":"https://x","term:fontsize":13}`)
	out := stripSyncKeys(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for k := range m {
		if k == "sync:folderpath" || k == "sync:webdavurl" {
			t.Fatalf("sync:* key %q survived stripping", k)
		}
	}
	if m["app:theme"] != "dark" || m["term:fontsize"] != float64(13) {
		t.Fatalf("non-sync keys not preserved: %v", m)
	}
}

// TestMergeSettings verifies loading settings keeps this machine's own sync:* config
// (incoming has none) while taking every other incoming key.
func TestMergeSettings(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "settings.json")
	local := []byte(`{"app:theme":"light","sync:folderpath":"/local/path","term:fontsize":12}`)
	if err := os.WriteFile(localPath, local, 0o644); err != nil {
		t.Fatal(err)
	}
	// incoming carries no sync:* (stripped on save), changes theme, adds a key
	incoming := json.RawMessage(`{"app:theme":"dark","window:opacity":0.9}`)
	merged, err := mergeSettings(localPath, incoming)
	if err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if m["sync:folderpath"] != "/local/path" {
		t.Fatalf("local sync:folderpath not preserved: %v", m["sync:folderpath"])
	}
	if m["app:theme"] != "dark" {
		t.Fatalf("incoming theme not applied: %v", m["app:theme"])
	}
	if m["window:opacity"] != 0.9 {
		t.Fatalf("incoming new key not applied: %v", m["window:opacity"])
	}
	if m["term:fontsize"] != float64(12) {
		t.Fatalf("local-only key dropped: %v", m["term:fontsize"])
	}
}
