// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
)

const snapshotFileName = "sync-snapshot.json"

// snapshotPath is where this install records the items it last published, so the
// next export can tell what actually changed (and assign mtimes accordingly). It
// lives in the data dir's db folder and is itself machine-local (never synced).
func snapshotPath() string {
	return filepath.Join(wavebase.GetWaveDataDir(), "db", snapshotFileName)
}

// LoadSnapshot reads the last-published item set; a missing file yields an empty
// snapshot (first run).
func LoadSnapshot() (map[string]SyncItem, error) {
	data, err := os.ReadFile(snapshotPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SyncItem{}, nil
		}
		return nil, err
	}
	var m map[string]SyncItem
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]SyncItem{}
	}
	return m, nil
}

// SaveSnapshot records the items just published as the new baseline.
func SaveSnapshot(items map[string]SyncItem) error {
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return atomicWriteFile(snapshotPath(), data)
}
