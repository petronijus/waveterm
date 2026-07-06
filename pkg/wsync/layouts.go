// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wcore"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// LayoutFilePrefix brackets the per-layout files (layout.<slug>.json) the named
// layout feature writes, so they're enumerable without colliding with session.json
// / wave-settings.json / the state.<installid>.json merge files.
const LayoutFilePrefix = "layout."

// LayoutSnapshot is one named, portable tab layout: the arrangement tree plus each
// block's meta (which carries the panel's location — term cmd:cwd, web url, preview
// file) and the tab's own meta (tab:background / bg:* so the look travels too).
// Restoring it rebuilds the blocks fresh in the same arrangement on whatever machine.
type LayoutSnapshot struct {
	Name            string                         `json:"name"`
	SavedTs         int64                          `json:"savedts"`
	RootNode        any                            `json:"rootnode,omitempty"`
	LeafOrder       []waveobj.LeafOrderEntry       `json:"leaforder,omitempty"`
	FocusedNodeId   string                         `json:"focusednodeid,omitempty"`
	MagnifiedNodeId string                         `json:"magnifiednodeid,omitempty"`
	TabMeta         waveobj.MetaMapType            `json:"tabmeta,omitempty"`
	Blocks          map[string]waveobj.MetaMapType `json:"blocks"`
}

// layoutNodeJSON mirrors the frontend LayoutNode JSON shape (camelCase fields —
// this is frontend-owned data passing through, not a wave API type) so a saved
// arrangement tree can be deep-copied with remapped ids. A leaf carries
// data.blockId and must omit children; a branch carries children and omits data
// (the frontend's validateNode rejects nodes with both or neither).
type layoutNodeJSON struct {
	Id            string           `json:"id,omitempty"`
	Data          *layoutNodeData  `json:"data,omitempty"`
	Children      []layoutNodeJSON `json:"children,omitempty"`
	FlexDirection string           `json:"flexDirection,omitempty"`
	Size          *float64         `json:"size,omitempty"`
}

type layoutNodeData struct {
	BlockId string `json:"blockId"`
}

// SaveLayout snapshots the given tab's arrangement + block metas under a name.
func SaveLayout(ctx context.Context, tabId string, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("layout name is required")
	}
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	if err := store.EnsureFolder(ctx); err != nil {
		return fmt.Errorf("ensuring sync folder: %w", err)
	}
	tab, err := wstore.DBGet[*waveobj.Tab](ctx, tabId)
	if err != nil {
		return fmt.Errorf("getting tab: %w", err)
	}
	ls, err := wstore.DBGet[*waveobj.LayoutState](ctx, tab.LayoutState)
	if err != nil {
		return fmt.Errorf("getting layout state: %w", err)
	}
	pathRoots := getPathRoots()
	blocks := make(map[string]waveobj.MetaMapType, len(tab.BlockIds))
	for _, blockId := range tab.BlockIds {
		block, err := wstore.DBGet[*waveobj.Block](ctx, blockId)
		if err != nil {
			return fmt.Errorf("getting block %s: %w", blockId, err)
		}
		blocks[blockId] = portablizeBlockMeta(block.Meta, pathRoots)
	}
	snap := LayoutSnapshot{
		Name:            name,
		SavedTs:         time.Now().UnixMilli(),
		RootNode:        ls.RootNode,
		FocusedNodeId:   ls.FocusedNodeId,
		MagnifiedNodeId: ls.MagnifiedNodeId,
		TabMeta:         tab.Meta,
		Blocks:          blocks,
	}
	if ls.LeafOrder != nil {
		snap.LeafOrder = *ls.LeafOrder
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	fileName, err := layoutFileForName(ctx, store, name)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, fileName, data); err != nil {
		return fmt.Errorf("writing layout: %w", err)
	}
	log.Printf("wsync: saved layout %q — %d blocks\n", name, len(blocks))
	return nil
}

// ListLayouts returns the display names of all saved layouts, sorted.
func ListLayouts(ctx context.Context) ([]string, error) {
	store, err := loadSessionTransport()
	if err != nil {
		return nil, err
	}
	files, err := store.ListFiles(ctx, LayoutFilePrefix)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		snap, ok, err := readLayoutFile(ctx, store, f)
		if err != nil || !ok {
			continue
		}
		names = append(names, snap.Name)
	}
	sort.Strings(names)
	return names, nil
}

// LoadLayout replaces the given tab's contents with the named saved layout —
// recreating each panel (with its saved cwd/url/file) in the saved arrangement and
// restoring the tab's background. The whole tree is shipped to the frontend as one
// settree action (per-panel insertatindex paths cannot express nested splits — see
// remapLayoutTree); the queued cleanuporphaned action then drops the tab's old blocks.
func LoadLayout(ctx context.Context, tabId string, name string) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	snap, err := findLayoutByName(ctx, store, name)
	if err != nil {
		return err
	}
	if snap.RootNode == nil {
		return fmt.Errorf("layout %q has no arrangement", snap.Name)
	}
	raw, err := json.Marshal(snap.RootNode)
	if err != nil {
		return err
	}
	var savedRoot layoutNodeJSON
	if err := json.Unmarshal(raw, &savedRoot); err != nil {
		return fmt.Errorf("parsing layout tree: %w", err)
	}
	pathRoots := getPathRoots()
	root, nodeIdMap, err := remapLayoutTree(savedRoot, func(oldBlockId string) (string, error) {
		meta := localizeBlockMeta(snap.Blocks[oldBlockId], pathRoots)
		if meta == nil {
			meta = waveobj.MetaMapType{}
		}
		block, err := wcore.CreateBlockWithTelemetry(ctx, tabId, &waveobj.BlockDef{Meta: meta}, &waveobj.RuntimeOpts{}, false)
		if err != nil {
			return "", err
		}
		return block.OID, nil
	})
	if err != nil {
		return fmt.Errorf("instantiating layout %q: %w", snap.Name, err)
	}
	actions := []waveobj.LayoutActionData{
		{
			ActionType:      wcore.LayoutActionDataType_SetTree,
			RootNode:        root,
			FocusedNodeId:   nodeIdMap[snap.FocusedNodeId],
			MagnifiedNodeId: nodeIdMap[snap.MagnifiedNodeId],
		},
		{ActionType: wcore.LayoutActionDataType_CleanupOrphaned},
	}
	if err := wcore.QueueLayoutActionForTab(ctx, tabId, actions...); err != nil {
		return fmt.Errorf("applying layout: %w", err)
	}
	if len(snap.TabMeta) > 0 {
		oref := waveobj.MakeORef(waveobj.OType_Tab, tabId)
		if err := wstore.UpdateObjectMeta(ctx, oref, snap.TabMeta, false); err != nil {
			return fmt.Errorf("restoring tab meta: %w", err)
		}
		wcore.SendWaveObjUpdate(oref)
	}
	log.Printf("wsync: loaded layout %q into tab %s\n", name, tabId)
	return nil
}

// DeleteLayout removes a named layout.
func DeleteLayout(ctx context.Context, name string) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	fileName, err := findLayoutFileName(ctx, store, name)
	if err != nil {
		return err
	}
	return store.Delete(ctx, fileName)
}

// remapLayoutTree deep-copies a saved arrangement tree, giving every node a fresh
// id (so repeated loads of one layout never alias nodes of the source tab) and
// replacing each leaf's block id via makeBlock. Structure, sizes, and flex
// directions carry over exactly. It returns the new tree plus the old→new node id
// map so focus/magnify references can follow the rename.
//
// Shipping the whole tree (settree) is deliberate: the insertatindex encoding used
// by wcore.PortableLayout resolves each index path against the partially built
// tree, which cannot express nested splits — sibling paths collide and the replay
// flattens the arrangement.
func remapLayoutTree(root layoutNodeJSON, makeBlock func(oldBlockId string) (string, error)) (*layoutNodeJSON, map[string]string, error) {
	nodeIdMap := make(map[string]string)
	var walk func(n layoutNodeJSON) (layoutNodeJSON, error)
	walk = func(n layoutNodeJSON) (layoutNodeJSON, error) {
		out := layoutNodeJSON{
			Id:            uuid.New().String(),
			FlexDirection: n.FlexDirection,
			Size:          n.Size,
		}
		if n.Id != "" {
			nodeIdMap[n.Id] = out.Id
		}
		if len(n.Children) == 0 {
			oldBlockId := ""
			if n.Data != nil {
				oldBlockId = n.Data.BlockId
			}
			newBlockId, err := makeBlock(oldBlockId)
			if err != nil {
				return layoutNodeJSON{}, fmt.Errorf("creating block for panel %q: %w", oldBlockId, err)
			}
			out.Data = &layoutNodeData{BlockId: newBlockId}
			return out, nil
		}
		out.Children = make([]layoutNodeJSON, 0, len(n.Children))
		for _, child := range n.Children {
			newChild, err := walk(child)
			if err != nil {
				return layoutNodeJSON{}, err
			}
			out.Children = append(out.Children, newChild)
		}
		return out, nil
	}
	newRoot, err := walk(root)
	if err != nil {
		return nil, nil, err
	}
	return &newRoot, nodeIdMap, nil
}

// layoutFileForName returns the file to write a layout to: an existing file whose
// stored Name matches (overwrite in place), else a fresh slug-based name.
func layoutFileForName(ctx context.Context, store Transport, name string) (string, error) {
	if existing, err := findLayoutFileName(ctx, store, name); err == nil {
		return existing, nil
	}
	return LayoutFilePrefix + slugify(name) + StateFileSuffix, nil
}

// findLayoutByName reads the layout whose stored Name matches.
func findLayoutByName(ctx context.Context, store Transport, name string) (*LayoutSnapshot, error) {
	files, err := store.ListFiles(ctx, LayoutFilePrefix)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		snap, ok, err := readLayoutFile(ctx, store, f)
		if err != nil || !ok {
			continue
		}
		if snap.Name == name {
			return snap, nil
		}
	}
	return nil, fmt.Errorf("layout %q not found", name)
}

// findLayoutFileName returns the file basename for a layout name, or an error if none.
func findLayoutFileName(ctx context.Context, store Transport, name string) (string, error) {
	files, err := store.ListFiles(ctx, LayoutFilePrefix)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		snap, ok, err := readLayoutFile(ctx, store, f)
		if err != nil || !ok {
			continue
		}
		if snap.Name == name {
			return f, nil
		}
	}
	return "", fmt.Errorf("layout %q not found", name)
}

func readLayoutFile(ctx context.Context, store Transport, fileName string) (*LayoutSnapshot, bool, error) {
	data, ok, err := store.Get(ctx, fileName)
	if err != nil || !ok {
		return nil, false, err
	}
	var snap LayoutSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, false, err
	}
	return &snap, true, nil
}

// slugify turns a layout name into a safe filename fragment.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		slug = "layout"
	}
	return slug
}
