// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// leaf/branch build a rootnode tree shaped like the frontend LayoutNode JSON.
func leaf(id, blockId string, dir string, size float64) map[string]any {
	return map[string]any{"id": id, "data": map[string]any{"blockId": blockId}, "flexDirection": dir, "size": size}
}
func branch(id string, dir string, size float64, children ...map[string]any) map[string]any {
	return map[string]any{"id": id, "flexDirection": dir, "size": size, "children": children}
}

func parseTree(t *testing.T, tree map[string]any) layoutNodeJSON {
	t.Helper()
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	var root layoutNodeJSON
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("unmarshal tree: %v", err)
	}
	return root
}

// TestRemapLayoutTreeNestedShape locks in that a deeply nested arrangement (a
// sub-row inside a column inside the root row — the shape the old insertatindex
// replay flattened) survives the remap: same structure, sizes, and directions,
// fresh node ids, and leaf block ids swapped via makeBlock.
func TestRemapLayoutTreeNestedShape(t *testing.T) {
	tree := branch("root", "row", 10,
		branch("colA", "column", 14.3,
			branch("rowA1", "row", 14.17,
				leaf("n-brain", "b-brain", "column", 9.93),
				leaf("n-cfg1", "b-cfg1", "column", 10.07),
			),
			leaf("n-cfg2", "b-cfg2", "row", 5.83),
		),
		branch("colB", "column", 7.8,
			leaf("n-sys", "b-sys", "row", 7.54),
			leaf("n-prev", "b-prev", "row", 12.46),
		),
		leaf("n-proc", "b-proc", "column", 7.89),
	)
	root := parseTree(t, tree)

	created := make(map[string]string)
	newRoot, nodeIdMap, err := remapLayoutTree(root, func(oldBlockId string) (string, error) {
		newId := "new-" + oldBlockId
		created[oldBlockId] = newId
		return newId, nil
	})
	if err != nil {
		t.Fatalf("remapLayoutTree: %v", err)
	}
	if len(created) != 6 {
		t.Fatalf("makeBlock called for %d blocks, want 6", len(created))
	}

	var shape func(n layoutNodeJSON) string
	shape = func(n layoutNodeJSON) string {
		if len(n.Children) == 0 {
			blockId := ""
			if n.Data != nil {
				blockId = n.Data.BlockId
			}
			return fmt.Sprintf("%s(%s,%.2f)", blockId, n.FlexDirection, *n.Size)
		}
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			parts = append(parts, shape(c))
		}
		return fmt.Sprintf("%s,%.2f[%s]", n.FlexDirection, *n.Size, strings.Join(parts, " "))
	}
	want := "row,10.00[" +
		"column,14.30[row,14.17[new-b-brain(column,9.93) new-b-cfg1(column,10.07)] new-b-cfg2(row,5.83)] " +
		"column,7.80[new-b-sys(row,7.54) new-b-prev(row,12.46)] " +
		"new-b-proc(column,7.89)]"
	if got := shape(*newRoot); got != want {
		t.Fatalf("remapped shape mismatch:\n got  %s\n want %s", got, want)
	}

	if len(nodeIdMap) != 10 {
		t.Fatalf("nodeIdMap has %d entries, want 10", len(nodeIdMap))
	}
	seen := make(map[string]bool)
	for oldId, newId := range nodeIdMap {
		if newId == "" || newId == oldId {
			t.Fatalf("node %q not remapped to a fresh id (got %q)", oldId, newId)
		}
		if seen[newId] {
			t.Fatalf("duplicate remapped node id %q", newId)
		}
		seen[newId] = true
	}
	if nodeIdMap["missing"] != "" {
		t.Fatalf("unknown node id must map to empty string")
	}

	// the frontend's validateNode rejects nodes carrying both children and data
	// (or a leaf with a children key), so the marshaled form must omit the unused field
	raw, err := json.Marshal(newRoot)
	if err != nil {
		t.Fatalf("marshal remapped tree: %v", err)
	}
	var checkNode func(n map[string]any) error
	checkNode = func(n map[string]any) error {
		_, hasChildren := n["children"]
		_, hasData := n["data"]
		if hasChildren == hasData {
			return fmt.Errorf("node %v must have exactly one of children/data", n["id"])
		}
		if hasChildren {
			for _, c := range n["children"].([]any) {
				if err := checkNode(c.(map[string]any)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal remapped tree: %v", err)
	}
	if err := checkNode(roundTrip); err != nil {
		t.Fatal(err)
	}
}

// TestRemapLayoutTreeBlockError verifies a failing makeBlock aborts the remap.
func TestRemapLayoutTreeBlockError(t *testing.T) {
	tree := branch("root", "row", 10,
		leaf("n1", "b1", "column", 10),
		leaf("n2", "b2", "column", 10),
	)
	root := parseTree(t, tree)
	_, _, err := remapLayoutTree(root, func(oldBlockId string) (string, error) {
		if oldBlockId == "b2" {
			return "", fmt.Errorf("boom")
		}
		return "new-" + oldBlockId, nil
	})
	if err == nil || !strings.Contains(err.Error(), "b2") {
		t.Fatalf("expected error mentioning failing block, got %v", err)
	}
}
