// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// Fields stripped from synced DB objects: `version` is a local counter; `jobid`
// and `runtimeopts` are tied to a local PTY / this machine's terminal size, so a
// synced block reopens fresh on another machine instead of pointing at a job that
// doesn't exist there.
var blockDropKeys = []string{waveobj.VersionKeyName, "jobid", "runtimeopts"}

// ExportLocalItems gathers the current syncable state — every top-level config
// JSON file plus the portable DB objects (workspaces, tabs, blocks, layouts) —
// into a canonical key→data map ready for stamping and merge. Volatile/local-only
// fields are stripped so unchanged state serializes identically across exports
// (no spurious mtime churn).
func ExportLocalItems(ctx context.Context) (map[string]json.RawMessage, error) {
	items := make(map[string]json.RawMessage)
	if err := exportConfigFiles(items); err != nil {
		return nil, err
	}
	if err := exportDBObjects(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func exportConfigFiles(items map[string]json.RawMessage) error {
	dir := wavebase.GetWaveConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		items[KeyPrefixConfig+e.Name()] = canonicalJSON(data)
	}
	return nil
}

func exportDBObjects(ctx context.Context, items map[string]json.RawMessage) error {
	if err := exportType[*waveobj.Workspace](ctx, waveobj.OType_Workspace, items, waveobj.VersionKeyName); err != nil {
		return err
	}
	if err := exportType[*waveobj.Tab](ctx, waveobj.OType_Tab, items, waveobj.VersionKeyName); err != nil {
		return err
	}
	if err := exportType[*waveobj.LayoutState](ctx, waveobj.OType_LayoutState, items, waveobj.VersionKeyName); err != nil {
		return err
	}
	if err := exportType[*waveobj.Block](ctx, waveobj.OType_Block, items, blockDropKeys...); err != nil {
		return err
	}
	return nil
}

func exportType[T waveobj.WaveObj](ctx context.Context, otype string, items map[string]json.RawMessage, dropKeys ...string) error {
	objs, err := wstore.DBGetAllObjsByType[T](ctx, otype)
	if err != nil {
		return err
	}
	for _, obj := range objs {
		data, err := canonicalObjJSON(obj, dropKeys...)
		if err != nil {
			return err
		}
		items[otype+":"+waveobj.GetOID(obj)] = data
	}
	return nil
}

// canonicalObjJSON serializes a waveobj with the given keys removed. json.Marshal
// of the resulting map sorts keys, giving a stable byte form for change detection.
func canonicalObjJSON(obj waveobj.WaveObj, dropKeys ...string) (json.RawMessage, error) {
	m, err := waveobj.ToJsonMap(obj)
	if err != nil {
		return nil, err
	}
	for _, k := range dropKeys {
		delete(m, k)
	}
	return json.Marshal(m)
}

// canonicalJSON re-marshals config bytes so cross-machine formatting differences
// don't read as changes; falls back to the raw bytes if it isn't valid JSON.
func canonicalJSON(data []byte) json.RawMessage {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return json.RawMessage(data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(data)
	}
	return out
}
