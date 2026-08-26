// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshserver

import (
	"reflect"
	"testing"
)

func TestParseTmuxSessionList(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   []string
	}{
		{
			name:   "empty output",
			stdout: "",
			want:   []string{},
		},
		{
			name:   "single session",
			stdout: "mactop\n",
			want:   []string{"mactop"},
		},
		{
			name:   "multiple sessions",
			stdout: "mactop\nomlx-11335\nomlx-11336\n",
			want:   []string{"mactop", "omlx-11335", "omlx-11336"},
		},
		{
			name:   "blank lines and surrounding whitespace",
			stdout: "\n  mactop  \n\nomlx-11335\n\n",
			want:   []string{"mactop", "omlx-11335"},
		},
		{
			name:   "session name with spaces",
			stdout: "my session with spaces\n",
			want:   []string{"my session with spaces"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTmuxSessionList(tt.stdout)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseTmuxSessionList(%q) = %#v, want %#v", tt.stdout, got, tt.want)
			}
		})
	}
}
