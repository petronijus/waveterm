// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"sort"
	"testing"
)

func TestLocalFolderTransportRoundTrip(t *testing.T) {
	ctx := context.Background()
	tr := MakeLocalFolderTransport(t.TempDir())

	if err := tr.EnsureFolder(ctx); err != nil {
		t.Fatalf("EnsureFolder: %v", err)
	}

	// missing file → (nil, false, nil)
	if _, ok, err := tr.Get(ctx, StateFileName("nope")); err != nil || ok {
		t.Fatalf("Get(missing) = ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	mac := StateFileName("mac-uuid")
	linux := StateFileName("linux-uuid")
	if err := tr.Put(ctx, mac, []byte(`{"installid":"mac-uuid"}`)); err != nil {
		t.Fatalf("Put mac: %v", err)
	}
	if err := tr.Put(ctx, linux, []byte(`{"installid":"linux-uuid"}`)); err != nil {
		t.Fatalf("Put linux: %v", err)
	}

	data, ok, err := tr.Get(ctx, mac)
	if err != nil || !ok || string(data) != `{"installid":"mac-uuid"}` {
		t.Fatalf("Get mac = %q ok=%v err=%v", data, ok, err)
	}

	names, err := tr.ListStateFiles(ctx)
	if err != nil {
		t.Fatalf("ListStateFiles: %v", err)
	}
	sort.Strings(names)
	want := []string{linux, mac}
	sort.Strings(want)
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("ListStateFiles = %v, want %v", names, want)
	}

	// Delete is idempotent: removing twice is not an error.
	if err := tr.Delete(ctx, mac); err != nil {
		t.Fatalf("Delete mac: %v", err)
	}
	if err := tr.Delete(ctx, mac); err != nil {
		t.Fatalf("Delete mac (again): %v", err)
	}
	names, err = tr.ListStateFiles(ctx)
	if err != nil || len(names) != 1 || names[0] != linux {
		t.Fatalf("after delete ListStateFiles = %v err=%v, want [%s]", names, err, linux)
	}
}

// ListStateFiles must ignore non-state files (and the .wsync-*.tmp scratch files
// atomicWriteFile leaves behind), returning only state.<installid>.json basenames.
func TestLocalFolderTransportListFilters(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tr := MakeLocalFolderTransport(dir)

	if err := tr.Put(ctx, StateFileName("a"), []byte(`{}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := tr.Put(ctx, "readme.txt", []byte("hi")); err != nil {
		t.Fatalf("Put readme: %v", err)
	}
	if err := tr.Put(ctx, "sync-snapshot.json", []byte(`{}`)); err != nil {
		t.Fatalf("Put snapshot: %v", err)
	}

	names, err := tr.ListStateFiles(ctx)
	if err != nil {
		t.Fatalf("ListStateFiles: %v", err)
	}
	if len(names) != 1 || names[0] != StateFileName("a") {
		t.Fatalf("ListStateFiles = %v, want [%s]", names, StateFileName("a"))
	}
}
