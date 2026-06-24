// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import "testing"

func TestJoinURL(t *testing.T) {
	got := joinURL("https://host/remote.php/dav/files/petr/", "waveterm-sync", "state.abc.json")
	want := "https://host/remote.php/dav/files/petr/waveterm-sync/state.abc.json"
	if got != want {
		t.Fatalf("joinURL = %q, want %q", got, want)
	}
	// empty/extra slashes collapse
	if joinURL("https://h/base", "/folder/", "") != "https://h/base/folder" {
		t.Fatalf("joinURL slash handling wrong: %q", joinURL("https://h/base", "/folder/", ""))
	}
}

func TestParseStateFileNames(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/remote.php/dav/files/petr/waveterm-sync/</d:href></d:response>
  <d:response><d:href>/remote.php/dav/files/petr/waveterm-sync/state.mac-uuid.json</d:href></d:response>
  <d:response><d:href>/remote.php/dav/files/petr/waveterm-sync/state.linux-uuid.json</d:href></d:response>
  <d:response><d:href>/remote.php/dav/files/petr/waveterm-sync/readme.txt</d:href></d:response>
</d:multistatus>`)
	names, err := parseFileNames(body, StateFilePrefix)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 state files, got %v", names)
	}
	if names[0] != "state.mac-uuid.json" || names[1] != "state.linux-uuid.json" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestStateFileNameRoundTrip(t *testing.T) {
	name := StateFileName("abc-123")
	if name != "state.abc-123.json" {
		t.Fatalf("StateFileName = %q", name)
	}
	got, err := parseFileNames([]byte(
		`<d:multistatus xmlns:d="DAV:"><d:response><d:href>/x/`+name+`</d:href></d:response></d:multistatus>`), StateFilePrefix)
	if err != nil || len(got) != 1 || got[0] != name {
		t.Fatalf("round trip failed: %v err=%v", got, err)
	}
}
