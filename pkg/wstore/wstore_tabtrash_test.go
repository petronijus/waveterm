// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
)

func setupTrashTestDB(t *testing.T) context.Context {
	t.Helper()
	dataDir := t.TempDir()
	prevDataHome := wavebase.DataHome_VarCache
	wavebase.DataHome_VarCache = dataDir
	err := os.MkdirAll(filepath.Join(dataDir, wavebase.WaveDBDir), 0o755)
	if err != nil {
		t.Fatalf("error creating db dir: %v", err)
	}
	err = InitWStore()
	if err != nil {
		t.Fatalf("error initializing wstore: %v", err)
	}
	t.Cleanup(func() {
		if globalDB != nil {
			globalDB.Close()
			globalDB = nil
		}
		wavebase.DataHome_VarCache = prevDataHome
	})
	return context.Background()
}

func makeTestSnapshot(tabId string, name string, tabIdx int, blockIds ...string) *TabTrashSnapshot {
	tab := &waveobj.Tab{
		OID:         tabId,
		Name:        name,
		LayoutState: "layout-" + tabId,
		BlockIds:    blockIds,
	}
	snapshot := &TabTrashSnapshot{
		Tab:    tab,
		Layout: &waveobj.LayoutState{OID: tab.LayoutState},
		TabIdx: tabIdx,
	}
	for _, blockId := range blockIds {
		snapshot.Blocks = append(snapshot.Blocks, &waveobj.Block{
			OID:        blockId,
			ParentORef: waveobj.MakeORef(waveobj.OType_Tab, tabId).String(),
		})
	}
	return snapshot
}

func TestTabTrashRoundTrip(t *testing.T) {
	ctx := setupTrashTestDB(t)
	const wsId = "ws-1"
	snapshot := makeTestSnapshot("tab-1", "Lumia 1020", 3, "block-a", "block-b")
	if err := TabTrashPut(ctx, wsId, snapshot); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}

	infos, err := TabTrashList(ctx, wsId)
	if err != nil {
		t.Fatalf("TabTrashList: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 closed tab, got %d", len(infos))
	}
	if infos[0].Name != "Lumia 1020" || infos[0].BlockCount != 2 || infos[0].TabId != "tab-1" {
		t.Fatalf("unexpected closed tab info: %+v", infos[0])
	}
	if infos[0].ClosedAt == 0 {
		t.Fatalf("expected a closedat timestamp")
	}

	taken, err := TabTrashTake(ctx, wsId, "")
	if err != nil {
		t.Fatalf("TabTrashTake: %v", err)
	}
	if taken == nil {
		t.Fatalf("expected a snapshot back")
	}
	if taken.Tab.Name != "Lumia 1020" || taken.TabIdx != 3 {
		t.Fatalf("snapshot did not survive the round trip: %+v", taken.Tab)
	}
	if taken.Layout == nil || taken.Layout.OID != "layout-tab-1" {
		t.Fatalf("layout did not survive the round trip: %+v", taken.Layout)
	}
	if len(taken.Blocks) != 2 || taken.Blocks[0].OID != "block-a" {
		t.Fatalf("blocks did not survive the round trip: %+v", taken.Blocks)
	}

	// taking is destructive: the same tab must not come back twice
	taken, err = TabTrashTake(ctx, wsId, "")
	if err != nil {
		t.Fatalf("TabTrashTake (second): %v", err)
	}
	if taken != nil {
		t.Fatalf("expected an empty trash, got %+v", taken.Tab)
	}
}

func TestTabTrashTakesMostRecentFirst(t *testing.T) {
	ctx := setupTrashTestDB(t)
	const wsId = "ws-1"
	// same-millisecond closes: ordering has to come from seq, not from closedat
	for _, name := range []string{"first", "second", "third"} {
		if err := TabTrashPut(ctx, wsId, makeTestSnapshot("tab-"+name, name, 0)); err != nil {
			t.Fatalf("TabTrashPut %s: %v", name, err)
		}
	}
	for _, want := range []string{"third", "second", "first"} {
		taken, err := TabTrashTake(ctx, wsId, "")
		if err != nil {
			t.Fatalf("TabTrashTake: %v", err)
		}
		if taken == nil || taken.Tab.Name != want {
			t.Fatalf("expected %q next out of the trash, got %+v", want, taken)
		}
	}
}

func TestTabTrashTakeByIdAndWorkspaceIsolation(t *testing.T) {
	ctx := setupTrashTestDB(t)
	if err := TabTrashPut(ctx, "ws-1", makeTestSnapshot("tab-1", "one", 0)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	if err := TabTrashPut(ctx, "ws-2", makeTestSnapshot("tab-2", "two", 0)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	taken, err := TabTrashTake(ctx, "ws-1", "tab-2")
	if err != nil {
		t.Fatalf("TabTrashTake: %v", err)
	}
	if taken != nil {
		t.Fatalf("a tab must not be restorable from another workspace: %+v", taken.Tab)
	}
	taken, err = TabTrashTake(ctx, "ws-2", "tab-2")
	if err != nil {
		t.Fatalf("TabTrashTake: %v", err)
	}
	if taken == nil || taken.Tab.OID != "tab-2" {
		t.Fatalf("expected tab-2 back from ws-2, got %+v", taken)
	}
}

func TestTabTrashEvictsPastTheLimit(t *testing.T) {
	ctx := setupTrashTestDB(t)
	const wsId = "ws-1"
	total := TabTrashMaxPerWorkspace + 3
	for i := 0; i < total; i++ {
		tabId := fmt.Sprintf("tab-%02d", i)
		if err := TabTrashPut(ctx, wsId, makeTestSnapshot(tabId, tabId, 0, "block-"+tabId)); err != nil {
			t.Fatalf("TabTrashPut %s: %v", tabId, err)
		}
	}
	if err := TabTrashPut(ctx, "ws-other", makeTestSnapshot("tab-other", "other", 0)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}

	evicted, err := TabTrashEvict(ctx, wsId)
	if err != nil {
		t.Fatalf("TabTrashEvict: %v", err)
	}
	if len(evicted) != 3 {
		t.Fatalf("expected 3 evicted snapshots, got %d", len(evicted))
	}
	for i, snapshot := range evicted {
		want := fmt.Sprintf("tab-%02d", i)
		if snapshot.Tab.OID != want {
			t.Fatalf("expected the oldest tabs evicted first, got %s at %d", snapshot.Tab.OID, i)
		}
		if len(snapshot.Blocks) != 1 {
			t.Fatalf("an evicted snapshot must carry its blocks so their zones can be released: %+v", snapshot)
		}
	}
	infos, err := TabTrashList(ctx, wsId)
	if err != nil {
		t.Fatalf("TabTrashList: %v", err)
	}
	if len(infos) != TabTrashMaxPerWorkspace {
		t.Fatalf("expected %d kept, got %d", TabTrashMaxPerWorkspace, len(infos))
	}
	otherInfos, err := TabTrashList(ctx, "ws-other")
	if err != nil {
		t.Fatalf("TabTrashList: %v", err)
	}
	if len(otherInfos) != 1 {
		t.Fatalf("eviction must not reach into another workspace, got %d", len(otherInfos))
	}
}

func TestTabTrashEvictWorkspace(t *testing.T) {
	ctx := setupTrashTestDB(t)
	if err := TabTrashPut(ctx, "ws-1", makeTestSnapshot("tab-1", "one", 0, "block-a")); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	if err := TabTrashPut(ctx, "ws-2", makeTestSnapshot("tab-2", "two", 0)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	evicted, err := TabTrashEvictWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("TabTrashEvictWorkspace: %v", err)
	}
	if len(evicted) != 1 || evicted[0].Tab.OID != "tab-1" {
		t.Fatalf("unexpected eviction result: %+v", evicted)
	}
	infos, err := TabTrashList(ctx, "ws-2")
	if err != nil {
		t.Fatalf("TabTrashList: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("ws-2 must be untouched, got %d", len(infos))
	}
}

func TestTabTrashReplacesSameTab(t *testing.T) {
	ctx := setupTrashTestDB(t)
	const wsId = "ws-1"
	if err := TabTrashPut(ctx, wsId, makeTestSnapshot("tab-1", "before", 0)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	if err := TabTrashPut(ctx, wsId, makeTestSnapshot("tab-1", "after", 2)); err != nil {
		t.Fatalf("TabTrashPut: %v", err)
	}
	infos, err := TabTrashList(ctx, wsId)
	if err != nil {
		t.Fatalf("TabTrashList: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "after" {
		t.Fatalf("re-closing a restored tab must keep one entry, got %+v", infos)
	}
}
