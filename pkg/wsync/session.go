// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/wavetermdev/waveterm/pkg/secretstore"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
	"github.com/wavetermdev/waveterm/pkg/wcore"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// SessionFileName is the single shared snapshot the manual Save/Load writes and reads.
// Unlike the per-install state files used by the (now manual-only) merge path, there is
// exactly one of these — Save overwrites it, Load restores it.
const SessionFileName = "session.json"

// WindowSlot is the portable part of an open window: which workspace it shows and where
// it sits. The window OID is per-machine and intentionally dropped — restore keys on the
// workspace, which is the only identity shared across installs.
type WindowSlot struct {
	WorkspaceId string          `json:"workspaceid"`
	Pos         waveobj.Point   `json:"pos"`
	WinSize     waveobj.WinSize `json:"winsize"`
}

// SessionSnapshot is the manually-saved session: the portable DB objects
// (workspaces/tabs/blocks/layouts) plus the set of open windows. Config files are
// deliberately excluded so loading a session never clobbers a machine's own settings
// (including the sync transport config itself).
type SessionSnapshot struct {
	SavedTs   int64                      `json:"savedts"`
	InstallId string                     `json:"installid"`
	Items     map[string]json.RawMessage `json:"items"`
	Windows   []WindowSlot               `json:"windows"`
}

// SaveSessionNow is the entry point the manual "Save session" command calls: it builds
// the configured transport, resolves this install's id, and writes the snapshot.
func SaveSessionNow(ctx context.Context) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	clientData, err := wcore.GetClientData(ctx)
	if err != nil {
		return fmt.Errorf("getting client data: %w", err)
	}
	return SaveSession(ctx, store, clientData.InstallId)
}

// LoadSessionNow is the entry point the manual "Load session" command calls.
func LoadSessionNow(ctx context.Context) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	return LoadSession(ctx, store)
}

// SaveSession captures the current session and overwrites the shared snapshot file.
func SaveSession(ctx context.Context, store Transport, installId string) error {
	if err := store.EnsureFolder(ctx); err != nil {
		return fmt.Errorf("ensuring sync folder: %w", err)
	}
	items := make(map[string]json.RawMessage)
	if err := exportDBObjects(ctx, items); err != nil {
		return fmt.Errorf("exporting session objects: %w", err)
	}
	windows, err := exportWindowSlots(ctx)
	if err != nil {
		return fmt.Errorf("exporting windows: %w", err)
	}
	snap := SessionSnapshot{
		SavedTs:   time.Now().UnixMilli(),
		InstallId: installId,
		Items:     items,
		Windows:   windows,
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, SessionFileName, data); err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	log.Printf("wsync: saved session — %d objects, %d windows\n", len(items), len(windows))
	return nil
}

// LoadSession restores the shared snapshot: it upserts the saved DB objects and then
// reconciles the open-window set to match the snapshot (opening missing windows, closing
// extras — but never the last remaining window).
func LoadSession(ctx context.Context, store Transport) error {
	data, ok, err := store.Get(ctx, SessionFileName)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}
	if !ok {
		return fmt.Errorf("no saved session found")
	}
	var snap SessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parsing session: %w", err)
	}
	if err := applySessionItems(ctx, snap.Items); err != nil {
		return fmt.Errorf("restoring session objects: %w", err)
	}
	if err := restoreWindows(ctx, snap.Windows); err != nil {
		return fmt.Errorf("restoring windows: %w", err)
	}
	log.Printf("wsync: loaded session — %d objects, %d windows\n", len(snap.Items), len(snap.Windows))
	return nil
}

// exportWindowSlots collapses the local windows to one portable slot per workspace.
func exportWindowSlots(ctx context.Context) ([]WindowSlot, error) {
	windows, err := wstore.DBGetAllObjsByType[*waveobj.Window](ctx, waveobj.OType_Window)
	if err != nil {
		return nil, err
	}
	slots := make([]WindowSlot, 0, len(windows))
	seen := make(map[string]bool)
	for _, w := range windows {
		if seen[w.WorkspaceId] {
			continue
		}
		seen[w.WorkspaceId] = true
		slots = append(slots, WindowSlot{WorkspaceId: w.WorkspaceId, Pos: w.Pos, WinSize: w.WinSize})
	}
	return slots, nil
}

// applySessionItems upserts every saved DB object into the local store, reusing the same
// applier the merge path uses. Saved sessions carry only object keys (no config), so a
// plain upsert per item is all that's needed.
func applySessionItems(ctx context.Context, items map[string]json.RawMessage) error {
	actions := make([]ApplyAction, 0, len(items))
	for key, data := range items {
		actions = append(actions, ApplyAction{Op: OpUpsert, Item: SyncItem{Key: key, Data: data}})
	}
	return ApplyActions(ctx, actions)
}

// restoreWindows makes the local open-window set match the snapshot: open a window for
// each saved workspace that isn't already shown, then close any local window whose
// workspace isn't in the snapshot. The last window is never closed, so the app is never
// left without a window.
func restoreWindows(ctx context.Context, slots []WindowSlot) error {
	wanted := make(map[string]bool, len(slots))
	for _, slot := range slots {
		wanted[slot.WorkspaceId] = true
		if err := wcore.OpenWindowForSync(ctx, slot.WorkspaceId, slot.Pos, slot.WinSize); err != nil {
			log.Printf("wsync: could not open window for workspace %s: %v\n", slot.WorkspaceId, err)
		}
	}
	allWindows, err := wstore.DBGetAllObjsByType[*waveobj.Window](ctx, waveobj.OType_Window)
	if err != nil {
		return err
	}
	remaining := len(allWindows)
	for _, w := range allWindows {
		if wanted[w.WorkspaceId] {
			continue
		}
		if remaining <= 1 {
			break
		}
		if err := wcore.CloseWindowKeepWorkspace(ctx, w.OID); err != nil {
			log.Printf("wsync: could not close window %s: %v\n", w.OID, err)
			continue
		}
		remaining--
	}
	return nil
}

// loadSessionTransport builds the storage transport for the manual session feature from
// settings. It mirrors loadSyncConfig's backend selection but does not require the
// sync:enabled toggle — Save/Load are explicit user actions, so a configured folder or
// WebDAV endpoint is enough.
func loadSessionTransport() (Transport, error) {
	settings := wconfig.GetWatcher().GetFullConfig().Settings
	if strings.TrimSpace(settings.SyncFolderPath) != "" {
		return MakeLocalFolderTransport(settings.SyncFolderPath), nil
	}
	if settings.SyncWebDAVURL == "" || settings.SyncWebDAVUser == "" {
		return nil, fmt.Errorf("session sync not configured: set sync:folderpath or sync:webdavurl/sync:webdavuser")
	}
	folder := settings.SyncFolder
	if folder == "" {
		folder = DefaultSyncFolder
	}
	pw, ok, err := secretstore.GetSecret(SecretName_WebDAVPassword)
	if err != nil {
		return nil, fmt.Errorf("reading webdav password secret: %w", err)
	}
	if !ok || pw == "" {
		return nil, fmt.Errorf("webdav configured but secret %q not set", SecretName_WebDAVPassword)
	}
	return MakeWebDAVClient(WebDAVConfig{
		BaseURL:  settings.SyncWebDAVURL,
		Folder:   folder,
		User:     settings.SyncWebDAVUser,
		Password: pw,
	}), nil
}
