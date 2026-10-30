// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"strings"
	"testing"
)

func z(lines ...string) []byte { return []byte(strings.Join(lines, "\x00") + "\x00") }

func TestParsePorcelainV2(t *testing.T) {
	tests := []struct {
		name string
		out  []byte
		want statusInfo
	}{
		{
			name: "clean, tracking, in sync",
			out:  z("# branch.oid 1234", "# branch.head main", "# branch.upstream origin/main", "# branch.ab +0 -0"),
			want: statusInfo{Branch: "main", Upstream: "origin/main", HasAB: true},
		},
		{
			name: "ahead and behind with every entry kind",
			out: z("# branch.oid 1234", "# branch.head feat/x", "# branch.upstream origin/feat/x", "# branch.ab +2 -3",
				"1 .M N... 100644 100644 100644 aaa bbb file.go",
				"2 R. N... 100644 100644 100644 aaa bbb R100 new name.go", "old name.go",
				"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go",
				"? untracked.txt"),
			want: statusInfo{Branch: "feat/x", Upstream: "origin/feat/x", HasAB: true, Ahead: 2, Behind: 3, Changed: 2, Untracked: 1, Conflicts: 1},
		},
		{
			name: "a rename's original path is not counted as an entry",
			out:  z("# branch.oid 1", "# branch.head main", "2 R. N... 100644 100644 100644 a b R100 to", "? looks-like-untracked"),
			want: statusInfo{Branch: "main", Changed: 1},
		},
		{
			name: "upstream configured but gone",
			out:  z("# branch.oid 1", "# branch.head main", "# branch.upstream origin/main"),
			want: statusInfo{Branch: "main", Upstream: "origin/main"},
		},
		{
			name: "detached",
			out:  z("# branch.oid 1", "# branch.head (detached)"),
			want: statusInfo{},
		},
		{
			name: "no commits",
			out:  z("# branch.oid (initial)", "# branch.head main"),
			want: statusInfo{NoCommits: true, Branch: "main"},
		},
		{
			name: "empty output",
			out:  nil,
			want: statusInfo{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePorcelainV2(tc.out)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestParsePorcelainV2Rejects(t *testing.T) {
	for _, out := range [][]byte{
		z("# branch.ab +x -1"),
		z("# branch.ab 1"),
		z("X unknown entry"),
	} {
		if _, err := parsePorcelainV2(out); err == nil {
			t.Errorf("expected an error for %q", out)
		}
	}
}
