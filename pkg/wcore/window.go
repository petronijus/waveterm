// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wcore

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/eventbus"
	"github.com/wavetermdev/waveterm/pkg/util/utilfn"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

func SwitchWorkspace(ctx context.Context, windowId string, workspaceId string) (*waveobj.Workspace, error) {
	log.Printf("SwitchWorkspace %s %s\n", windowId, workspaceId)
	ws, err := GetWorkspace(ctx, workspaceId)
	if err != nil {
		return nil, fmt.Errorf("error getting new workspace: %w", err)
	}
	window, err := GetWindow(ctx, windowId)
	if err != nil {
		return nil, fmt.Errorf("error getting window: %w", err)
	}
	curWsId := window.WorkspaceId
	if curWsId == workspaceId {
		return nil, nil
	}

	allWindows, err := wstore.DBGetAllObjsByType[*waveobj.Window](ctx, waveobj.OType_Window)
	if err != nil {
		return nil, fmt.Errorf("error getting all windows: %w", err)
	}

	for _, w := range allWindows {
		if w.WorkspaceId == workspaceId {
			log.Printf("workspace %s already has a window %s, focusing that window\n", workspaceId, w.OID)
			client := wshclient.GetBareRpcClient()
			err = wshclient.FocusWindowCommand(client, w.OID, &wshrpc.RpcOpts{Route: wshutil.ElectronRoute})
			return nil, err
		}
	}
	window.WorkspaceId = workspaceId
	err = wstore.DBUpdate(ctx, window)
	if err != nil {
		return nil, fmt.Errorf("error updating window: %w", err)
	}

	deleted, _, err := DeleteWorkspace(ctx, curWsId, false)
	if err != nil && deleted {
		print(err.Error()) // @jalileh isolated the error for now, curwId/workspace was deleted when this occurs.
	} else if err != nil {
		return nil, fmt.Errorf("error deleting workspace: %w", err)
	}

	if !deleted {
		log.Printf("current workspace %s was not deleted\n", curWsId)
	} else {
		log.Printf("deleted current workspace %s\n", curWsId)
	}

	log.Printf("switching window %s to workspace %s\n", windowId, workspaceId)
	return ws, nil
}

func GetWindow(ctx context.Context, windowId string) (*waveobj.Window, error) {
	window, err := wstore.DBMustGet[*waveobj.Window](ctx, windowId)
	if err != nil {
		log.Printf("error getting window %q: %v\n", windowId, err)
		return nil, err
	}
	return window, nil
}

func CreateWindow(ctx context.Context, winSize *waveobj.WinSize, workspaceId string) (*waveobj.Window, error) {
	log.Printf("CreateWindow %v %v\n", winSize, workspaceId)
	var ws *waveobj.Workspace
	if workspaceId == "" {
		ws1, err := CreateWorkspace(ctx, "", "", "", false, false)
		if err != nil {
			return nil, fmt.Errorf("error creating workspace: %w", err)
		}
		ws = ws1
	} else {
		ws1, err := GetWorkspace(ctx, workspaceId)
		if err != nil {
			return nil, fmt.Errorf("error getting workspace: %w", err)
		}
		ws = ws1
	}
	windowId := uuid.NewString()
	if winSize == nil {
		winSize = &waveobj.WinSize{
			Width:  0,
			Height: 0,
		}
	}
	window := &waveobj.Window{
		OID:         windowId,
		WorkspaceId: ws.OID,
		IsNew:       true,
		Pos: waveobj.Point{
			X: 0,
			Y: 0,
		},
		WinSize: *winSize,
	}
	err := wstore.DBInsert(ctx, window)
	if err != nil {
		return nil, fmt.Errorf("error inserting window: %w", err)
	}
	client, err := GetClientData(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}
	client.WindowIds = append(client.WindowIds, windowId)
	err = wstore.DBUpdate(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("error updating client: %w", err)
	}
	return GetWindow(ctx, windowId)
}

// WindowForWorkspace returns the (first) local window currently showing workspaceId,
// or nil if none does. Used by session restore to decide whether a window already
// exists for a synced workspace.
func WindowForWorkspace(ctx context.Context, workspaceId string) (*waveobj.Window, error) {
	allWindows, err := wstore.DBGetAllObjsByType[*waveobj.Window](ctx, waveobj.OType_Window)
	if err != nil {
		return nil, fmt.Errorf("error getting all windows: %w", err)
	}
	for _, w := range allWindows {
		if w.WorkspaceId == workspaceId {
			return w, nil
		}
	}
	return nil, nil
}

// OpenWindowForSync opens an OS window for an already-existing workspace at the given
// geometry — used by session restore to mirror a window saved on another machine.
// The geometry is the saved Pos/WinSize; Electron clamps it to the local display when
// it builds the BrowserWindow. No-op if a window already shows the workspace. Unlike
// the per-machine window OIDs (which never match across installs), the workspace is the
// portable identity, so we key restore on it.
func OpenWindowForSync(ctx context.Context, workspaceId string, pos waveobj.Point, winSize waveobj.WinSize) error {
	existing, err := WindowForWorkspace(ctx, workspaceId)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	if _, err := GetWorkspace(ctx, workspaceId); err != nil {
		return fmt.Errorf("error getting workspace %q for sync window: %w", workspaceId, err)
	}
	windowId := uuid.NewString()
	window := &waveobj.Window{
		OID:         windowId,
		WorkspaceId: workspaceId,
		Pos:         pos,
		WinSize:     winSize,
	}
	if err := wstore.DBInsert(ctx, window); err != nil {
		return fmt.Errorf("error inserting sync window: %w", err)
	}
	client, err := GetClientData(ctx)
	if err != nil {
		return fmt.Errorf("error getting client: %w", err)
	}
	client.WindowIds = append(client.WindowIds, windowId)
	if err := wstore.DBUpdate(ctx, client); err != nil {
		return fmt.Errorf("error updating client: %w", err)
	}
	eventbus.SendEventToElectron(eventbus.WSEventType{
		EventType: eventbus.WSEvent_ElectronNewWindow,
		Data:      windowId,
	})
	return nil
}

// CloseWindowKeepWorkspace closes a window without deleting its workspace. Session
// restore uses this when reconciling the open-window set: the workspace's own
// lifecycle is owned by the workspace items, so closing a window here must not take
// the (still-synced) workspace content with it the way CloseWindow does.
func CloseWindowKeepWorkspace(ctx context.Context, windowId string) error {
	if err := wstore.DBDelete(ctx, waveobj.OType_Window, windowId); err != nil {
		return fmt.Errorf("error deleting window: %w", err)
	}
	client, err := wstore.DBGetSingleton[*waveobj.Client](ctx)
	if err != nil {
		return fmt.Errorf("error getting client: %w", err)
	}
	client.WindowIds = utilfn.RemoveElemFromSlice(client.WindowIds, windowId)
	if err := wstore.DBUpdate(ctx, client); err != nil {
		return fmt.Errorf("error updating client: %w", err)
	}
	eventbus.SendEventToElectron(eventbus.WSEventType{
		EventType: eventbus.WSEvent_ElectronCloseWindow,
		Data:      windowId,
	})
	return nil
}

// CloseWindow closes a window and deletes its workspace if it is empty and not named.
// If fromElectron is true, it does not send an event to Electron.
func CloseWindow(ctx context.Context, windowId string, fromElectron bool) error {
	log.Printf("CloseWindow %s\n", windowId)
	window, err := GetWindow(ctx, windowId)
	if err == nil {
		log.Printf("got window %s\n", windowId)
		deleted, _, err := DeleteWorkspace(ctx, window.WorkspaceId, false)
		if err != nil {
			log.Printf("error deleting workspace: %v\n", err)
		}
		if deleted {
			log.Printf("deleted workspace %s\n", window.WorkspaceId)
		}
		err = wstore.DBDelete(ctx, waveobj.OType_Window, windowId)
		if err != nil {
			return fmt.Errorf("error deleting window: %w", err)
		}
		log.Printf("deleted window %s\n", windowId)
	} else {
		log.Printf("error getting window %s: %v\n", windowId, err)
	}
	client, err := wstore.DBGetSingleton[*waveobj.Client](ctx)
	if err != nil {
		return fmt.Errorf("error getting client: %w", err)
	}
	client.WindowIds = utilfn.RemoveElemFromSlice(client.WindowIds, windowId)
	err = wstore.DBUpdate(ctx, client)
	if err != nil {
		return fmt.Errorf("error updating client: %w", err)
	}
	log.Printf("updated client\n")
	if !fromElectron {
		eventbus.SendEventToElectron(eventbus.WSEventType{
			EventType: eventbus.WSEvent_ElectronCloseWindow,
			Data:      windowId,
		})
	}
	return nil
}

func CheckAndFixWindow(ctx context.Context, windowId string) *waveobj.Window {
	log.Printf("CheckAndFixWindow %s\n", windowId)
	window, err := GetWindow(ctx, windowId)
	if err != nil {
		log.Printf("error getting window %q (in checkAndFixWindow): %v\n", windowId, err)
		return nil
	}
	ws, err := GetWorkspace(ctx, window.WorkspaceId)
	if err != nil {
		log.Printf("error getting workspace %q (in checkAndFixWindow): %v\n", window.WorkspaceId, err)
		CloseWindow(ctx, windowId, false)
		return nil
	}
	if len(ws.TabIds) == 0 {
		log.Printf("fixing workspace with no tabs %q (in checkAndFixWindow)\n", ws.OID)
		_, err = CreateTab(ctx, ws.OID, "", true, false)
		if err != nil {
			log.Printf("error creating tab (in checkAndFixWindow): %v\n", err)
		}
	}
	return window
}

func FocusWindow(ctx context.Context, windowId string) error {
	log.Printf("FocusWindow %s\n", windowId)
	client, err := GetClientData(ctx)
	if err != nil {
		log.Printf("error getting client data: %v\n", err)
		return err
	}
	winIdx := utilfn.SliceIdx(client.WindowIds, windowId)
	if winIdx == -1 {
		log.Printf("window %s not found in client data\n", windowId)
		return nil
	}
	client.WindowIds = utilfn.MoveSliceIdxToFront(client.WindowIds, winIdx)
	log.Printf("client.WindowIds: %v\n", client.WindowIds)
	return wstore.DBUpdate(ctx, client)
}
