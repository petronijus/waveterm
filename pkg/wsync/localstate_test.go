// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import "testing"

// TestCanonicalJSON verifies the empty/invalid handling that used to poison the
// settings bundle: a 0-byte config file (e.g. widgets.json) must be skipped, not
// embedded as an invalid RawMessage that breaks the bundle's marshal.
func TestCanonicalJSON(t *testing.T) {
	if out, err := canonicalJSON([]byte("")); err != nil || out != nil {
		t.Fatalf("empty input: want (nil, nil), got (%q, %v)", out, err)
	}
	if out, err := canonicalJSON([]byte("  \n\t")); err != nil || out != nil {
		t.Fatalf("whitespace input: want (nil, nil), got (%q, %v)", out, err)
	}
	if _, err := canonicalJSON([]byte("{broken")); err == nil {
		t.Fatalf("invalid JSON should return an error")
	}
	out, err := canonicalJSON([]byte("{\n  \"b\": 1,\n  \"a\": 2\n}"))
	if err != nil {
		t.Fatalf("valid JSON: %v", err)
	}
	if string(out) != `{"a":2,"b":1}` {
		t.Fatalf("expected canonical compact form, got %q", out)
	}
}
