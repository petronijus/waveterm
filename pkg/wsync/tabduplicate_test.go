// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"strings"
	"testing"
)

func TestInsertTabAfter(t *testing.T) {
	cases := []struct {
		name    string
		ids     []string
		newId   string
		afterId string
		want    string
	}{
		// CreateTab appends, so the new id normally arrives at the end.
		{"appended new tab moves next to source", []string{"a", "b", "c", "new"}, "new", "a", "a,new,b,c"},
		{"source is last", []string{"a", "b", "new"}, "new", "b", "a,b,new"},
		{"source is first of two", []string{"a", "new"}, "new", "a", "a,new"},
		{"already adjacent stays put", []string{"a", "new", "b"}, "new", "a", "a,new,b"},
		{"new id not yet present", []string{"a", "b"}, "new", "a", "a,new,b"},
		{"middle source", []string{"a", "b", "c", "new"}, "new", "b", "a,b,new,c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := insertTabAfter(c.ids, c.newId, c.afterId)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, ",") != c.want {
				t.Errorf("got %v, want %s", got, c.want)
			}
		})
	}
}

func TestInsertTabAfterUnknownSource(t *testing.T) {
	if _, err := insertTabAfter([]string{"a", "new"}, "new", "nope"); err == nil {
		t.Errorf("expected an error when the source tab is not in the workspace")
	}
}

func TestDuplicateTabName(t *testing.T) {
	if got := duplicateTabName("work"); got != "work copy" {
		t.Errorf("got %q, want %q", got, "work copy")
	}
	// An unnamed tab keeps its auto-name from CreateTab rather than becoming " copy".
	if got := duplicateTabName(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
