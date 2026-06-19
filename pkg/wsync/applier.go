// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// ApplyActions writes the reconciled changes into the local machine. Config items
// become file writes/removes in the config dir (the fsnotify watcher reloads them);
// DB-object items are written through wstore (DBUpdate/DBInsert/DBDelete), whose
// post-commit broadcast makes the UI update live. Windows, jobs and filestore are
// never touched — they stay machine-local.
func ApplyActions(ctx context.Context, actions []ApplyAction) error {
	for _, act := range actions {
		var err error
		if strings.HasPrefix(act.Item.Key, KeyPrefixConfig) {
			err = applyConfig(act)
		} else {
			err = applyDBObject(ctx, act)
		}
		if err != nil {
			return fmt.Errorf("applying %s %q: %w", act.Op, act.Item.Key, err)
		}
	}
	return nil
}

func applyConfig(act ApplyAction) error {
	name := strings.TrimPrefix(act.Item.Key, KeyPrefixConfig)
	if !isSafeConfigName(name) {
		return fmt.Errorf("unsafe config file name %q", name)
	}
	fullPath := filepath.Join(wavebase.GetWaveConfigDir(), name)
	if act.Op == OpDelete {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicWriteFile(fullPath, act.Item.Data)
}

func applyDBObject(ctx context.Context, act ApplyAction) error {
	otype, oid, ok := splitObjKey(act.Item.Key)
	if !ok {
		return fmt.Errorf("malformed object key")
	}
	if act.Op == OpDelete {
		return wstore.DBDelete(ctx, otype, oid)
	}
	obj, err := waveobj.FromJson(act.Item.Data)
	if err != nil {
		return err
	}
	if obj.GetOType() != otype || waveobj.GetOID(obj) != oid {
		return fmt.Errorf("object payload (%s) does not match key (%s:%s)", obj.GetOType(), otype, oid)
	}
	exists, err := wstore.DBExistsORef(ctx, waveobj.MakeORef(otype, oid))
	if err != nil {
		return err
	}
	if exists {
		return wstore.DBUpdate(ctx, obj)
	}
	return wstore.DBInsert(ctx, obj)
}

// splitObjKey splits "otype:oid" on the first colon. Config keys are handled
// separately, so any key reaching here must be an object key.
func splitObjKey(key string) (otype string, oid string, ok bool) {
	idx := strings.IndexByte(key, ':')
	if idx <= 0 || idx == len(key)-1 {
		return "", "", false
	}
	otype, oid = key[:idx], key[idx+1:]
	if !waveobj.ValidOTypes[otype] {
		return "", "", false
	}
	return otype, oid, true
}

// isSafeConfigName guards file writes against path traversal — a synced name must
// be a plain filename inside the config dir.
func isSafeConfigName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return name == filepath.Base(name)
}

// atomicWriteFile writes via a temp file + rename so a reader (or a crash) never
// sees a half-written config/snapshot file.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".wsync-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
