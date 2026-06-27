// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
)

// SettingsBundleFileName is the single shared file the manual "Save settings" writes
// (and "Load settings" reads). One file holds every config JSON; Save overwrites it.
const SettingsBundleFileName = "wave-settings.json"

// syncKeyPrefix marks the transport config inside settings.json (sync:folderpath,
// sync:webdavurl, …). Those keys are machine-local — stripped on save and preserved
// on load so syncing settings between machines never repoints/breaks a machine's sync.
const syncKeyPrefix = "sync:"

// SettingsBundle is the portable settings set: every config JSON file in the config
// dir (recursively, keyed by relative slash-path) bundled into one file. Unlike the
// session snapshot, this is *only* config — no DB objects, no windows.
type SettingsBundle struct {
	SavedTs int64                      `json:"savedts"`
	Files   map[string]json.RawMessage `json:"files"`
}

// SaveSettingsNow bundles the config dir and writes it to the configured transport.
func SaveSettingsNow(ctx context.Context) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	if err := store.EnsureFolder(ctx); err != nil {
		return fmt.Errorf("ensuring sync folder: %w", err)
	}
	files, err := collectConfigFiles()
	if err != nil {
		return fmt.Errorf("collecting config files: %w", err)
	}
	bundle := SettingsBundle{SavedTs: time.Now().UnixMilli(), Files: files}
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, SettingsBundleFileName, data); err != nil {
		return fmt.Errorf("writing settings: %w", err)
	}
	log.Printf("wsync: saved settings — %d files\n", len(files))
	return nil
}

// LoadSettingsNow reads the bundle and writes each config file back into the config
// dir. settings.json is merged so this machine's own sync:* transport config survives.
func LoadSettingsNow(ctx context.Context) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	data, ok, err := store.Get(ctx, SettingsBundleFileName)
	if err != nil {
		return fmt.Errorf("reading settings: %w", err)
	}
	if !ok {
		return fmt.Errorf("no saved settings found")
	}
	var bundle SettingsBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("parsing settings: %w", err)
	}
	if err := applyConfigFiles(bundle.Files); err != nil {
		return fmt.Errorf("applying settings: %w", err)
	}
	log.Printf("wsync: loaded settings — %d files\n", len(bundle.Files))
	return nil
}

// collectConfigFiles walks the config dir for *.json files (including subdirs such as
// presets/), keyed by their slash-separated relative path. The sync:* transport keys
// are stripped from settings.json so they never travel between machines.
func collectConfigFiles() (map[string]json.RawMessage, error) {
	dir := wavebase.GetWaveConfigDir()
	files := make(map[string]json.RawMessage)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == wconfig.SettingsFile {
			raw = stripSyncKeys(raw)
		}
		files[rel] = canonicalJSON(raw)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// applyConfigFiles writes each bundled config file into the config dir, guarding
// against path traversal. settings.json is merged onto the local file.
func applyConfigFiles(files map[string]json.RawMessage) error {
	dir := wavebase.GetWaveConfigDir()
	for rel, raw := range files {
		if !isSafeRelConfigPath(rel) {
			return fmt.Errorf("unsafe config path %q", rel)
		}
		data := []byte(raw)
		if rel == wconfig.SettingsFile {
			merged, err := mergeSettings(filepath.Join(dir, wconfig.SettingsFile), raw)
			if err != nil {
				return err
			}
			data = merged
		}
		if err := atomicWriteFile(filepath.Join(dir, filepath.FromSlash(rel)), data); err != nil {
			return err
		}
	}
	return nil
}

// stripSyncKeys removes sync:* keys from a settings.json body. A non-object body is
// returned unchanged.
func stripSyncKeys(raw []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	for k := range m {
		if strings.HasPrefix(k, syncKeyPrefix) {
			delete(m, k)
		}
	}
	out, err := wconfig.MarshalConfigJSON(m)
	if err != nil {
		return raw
	}
	return out
}

// mergeSettings overlays the incoming settings.json (which carries no sync:* keys)
// onto the local file, so the machine's own sync:* transport config is preserved.
func mergeSettings(localPath string, incoming json.RawMessage) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(incoming, &in); err != nil {
		// not an object — just write it through as-is
		return incoming, nil
	}
	local := map[string]any{}
	if b, err := os.ReadFile(localPath); err == nil {
		_ = json.Unmarshal(b, &local)
	}
	for k, v := range in {
		local[k] = v
	}
	// Pretty-print (key-ordered, indented) so a synced settings.json stays readable —
	// one key per line — instead of collapsing to a single minified noodle.
	return wconfig.MarshalConfigJSON(local)
}

// isSafeRelConfigPath allows files inside the config dir (subdirs ok) but rejects
// absolute paths and any ".." traversal.
func isSafeRelConfigPath(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return false
	}
	return !filepath.IsAbs(rel)
}
