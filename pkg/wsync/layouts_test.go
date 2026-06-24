// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wavetermdev/waveterm/pkg/waveobj"
)

// leaf/branch build a rootnode tree shaped like the frontend LayoutNode JSON.
func leaf(id, blockId string, size float64) map[string]any {
	return map[string]any{"id": id, "data": map[string]any{"blockId": blockId}, "size": size}
}
func branch(id string, size float64, children ...map[string]any) map[string]any {
	return map[string]any{"id": id, "size": size, "children": children}
}

// TestPortableFromSnapshotStarterShape locks in that the tree→PortableLayout walk
// reproduces the canonical index paths the hand-written GetStarterLayout uses
// ([0],[1],[1,1],[1,2]) for the same arrangement (term beside a column of three).
func TestPortableFromSnapshotStarterShape(t *testing.T) {
	root := branch("root", 10,
		leaf("n0", "b-term", 10),
		branch("n1", 10,
			leaf("n10", "b-sysinfo", 10),
			leaf("n11", "b-web", 10),
			leaf("n12", "b-preview", 10),
		),
	)
	snap := &LayoutSnapshot{
		Name:     "test",
		RootNode: root,
		Blocks: map[string]waveobj.MetaMapType{
			"b-term":    {"view": "term"},
			"b-sysinfo": {"view": "sysinfo"},
			"b-web":     {"view": "web", "url": "https://example.com"},
			"b-preview": {"view": "preview", "file": "/tmp"},
		},
	}
	got, err := portableFromSnapshot(snap)
	if err != nil {
		t.Fatalf("portableFromSnapshot: %v", err)
	}
	want := [][]int{{0}, {1}, {1, 1}, {1, 2}}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if fmt.Sprint(got[i].IndexArr) != fmt.Sprint(want[i]) {
			t.Fatalf("entry %d indexArr = %v, want %v", i, got[i].IndexArr, want[i])
		}
	}
	// the web/preview panels must carry their location meta through
	for _, e := range got {
		if e.BlockDef == nil || e.BlockDef.Meta == nil {
			t.Fatalf("entry missing blockdef/meta: %+v", e)
		}
	}
	b, _ := json.Marshal(got)
	t.Logf("portable: %s", b)
}
