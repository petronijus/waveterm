// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"bytes"
	"encoding/json"
)

// StampItems turns the current local item set into the items this install should
// publish, deciding mtimes by diffing against the previously-published snapshot:
//   - unchanged item  → keep its prior mtime + installid (so we don't claim a write
//     we didn't make and steal the LWW from a peer),
//   - new/changed item → stamp nowMs + this installid,
//   - item gone that was live last time → a fresh tombstone (nowMs),
//   - item gone that was already a tombstone → keep the tombstone as-is.
//
// Equality is a byte compare of the serialized data; the callers serialize through
// the same path (json of the waveobj / the raw config file), and struct field
// order + sorted map keys make that stable for unchanged objects.
func StampItems(current map[string]json.RawMessage, prev map[string]SyncItem, installId string, nowMs int64) map[string]SyncItem {
	out := make(map[string]SyncItem, len(current))
	for key, data := range current {
		if p, ok := prev[key]; ok && !p.Deleted && bytes.Equal(p.Data, data) {
			out[key] = p // unchanged since last publish — preserve its stamp
			continue
		}
		out[key] = SyncItem{Key: key, Mtime: nowMs, InstallId: installId, Data: data}
	}
	for key, p := range prev {
		if _, ok := current[key]; ok {
			continue
		}
		if p.Deleted {
			out[key] = p // keep the existing tombstone (and its mtime)
			continue
		}
		out[key] = SyncItem{Key: key, Mtime: nowMs, InstallId: installId, Deleted: true}
	}
	return out
}
