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

// layoutNodeJSON mirrors the parts of a frontend LayoutNode we need to walk the
// arrangement tree: a leaf carries data.blockId; a branch carries children.
type layoutNodeJSON struct {
	Id   string `json:"id"`
	Data *struct {
		BlockId string `json:"blockId"`
	} `json:"data"`
	Children []layoutNodeJSON `json:"children"`
	Size     *float64         `json:"size"`
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
	blocks := make(map[string]waveobj.MetaMapType, len(tab.BlockIds))
	for _, blockId := range tab.BlockIds {
		block, err := wstore.DBGet[*waveobj.Block](ctx, blockId)
		if err != nil {
			return fmt.Errorf("getting block %s: %w", blockId, err)
		}
		blocks[blockId] = block.Meta
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
// restoring the tab's background. The old blocks are cleaned up by the frontend once
// ClearTree drops them from the layout.
func LoadLayout(ctx context.Context, tabId string, name string) error {
	store, err := loadSessionTransport()
	if err != nil {
		return err
	}
	snap, err := findLayoutByName(ctx, store, name)
	if err != nil {
		return err
	}
	portable, err := portableFromSnapshot(snap)
	if err != nil {
		return err
	}
	if err := wcore.ApplyPortableLayout(ctx, tabId, portable, false); err != nil {
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

// portableFromSnapshot converts a saved arrangement tree into a wcore.PortableLayout.
// It walks the tree depth-first: the first child of a node inherits the parent's
// index path (it lands in the parent's slot, then later siblings wrap it into a
// branch), and each subsequent sibling appends its position. This reproduces the
// same shapes the tiling engine builds (sizes preserved). Split direction is derived
// by the engine's depth-alternation, so layouts built through normal use round-trip.
func portableFromSnapshot(snap *LayoutSnapshot) (wcore.PortableLayout, error) {
	if snap.RootNode == nil {
		return nil, fmt.Errorf("layout %q has no arrangement", snap.Name)
	}
	raw, err := json.Marshal(snap.RootNode)
	if err != nil {
		return nil, err
	}
	var root layoutNodeJSON
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parsing layout tree: %w", err)
	}
	out := make(wcore.PortableLayout, 0)
	var walk func(n layoutNodeJSON, path []int)
	walk = func(n layoutNodeJSON, path []int) {
		if len(n.Children) == 0 {
			blockId := ""
			if n.Data != nil {
				blockId = n.Data.BlockId
			}
			meta := snap.Blocks[blockId]
			if meta == nil {
				meta = waveobj.MetaMapType{}
			}
			var size *uint
			if n.Size != nil {
				s := uint(*n.Size)
				size = &s
			}
			idx := append([]int{}, path...)
			if len(idx) == 0 {
				idx = []int{0}
			}
			focused := n.Id != "" && n.Id == snap.FocusedNodeId
			out = append(out, wcore.PortableLayout{{
				IndexArr: idx,
				Size:     size,
				BlockDef: &waveobj.BlockDef{Meta: meta},
				Focused:  focused,
			}}...)
			return
		}
		for i, child := range n.Children {
			if i == 0 {
				walk(child, path)
			} else {
				walk(child, append(append([]int{}, path...), i))
			}
		}
	}
	walk(root, []int{})
	if len(out) == 0 {
		return nil, fmt.Errorf("layout %q has no panels", snap.Name)
	}
	return out, nil
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
