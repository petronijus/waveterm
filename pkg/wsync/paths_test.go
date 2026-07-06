// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"testing"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
)

func TestMakePortablePath(t *testing.T) {
	home := wavebase.GetHomeDir()
	roots := map[string]string{
		"dev":  home + "/Documents/Dev",
		"deep": home + "/Documents/Dev/nested",
	}
	tests := []struct {
		name string
		path string
		want string
	}{
		{"root match", home + "/Documents/Dev/waveterm", "${dev}/waveterm"},
		{"longest root wins", home + "/Documents/Dev/nested/proj", "${deep}/proj"},
		{"root itself", home + "/Documents/Dev", "${dev}"},
		{"home fallback", home + "/Downloads", "~/Downloads"},
		{"home itself", home, "~"},
		{"outside home stays", "/opt/data", "/opt/data"},
		{"tilde passes through", "~/Documents/Dev/x", "~/Documents/Dev/x"},
		{"token passes through", "${dev}/x", "${dev}/x"},
		{"blank", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := makePortablePath(tt.path, roots); got != tt.want {
				t.Errorf("makePortablePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMakePortablePathTildeRoot(t *testing.T) {
	// root values themselves may be written in ~-form in settings.json
	roots := map[string]string{"dev": "~/Documents/Dev"}
	home := wavebase.GetHomeDir()
	got := makePortablePath(home+"/Documents/Dev/waveterm", roots)
	if got != "${dev}/waveterm" {
		t.Errorf("tilde-form root: got %q, want ${dev}/waveterm", got)
	}
}

func TestLocalizePath(t *testing.T) {
	roots := map[string]string{
		"dev": "~/Documents/Dev",
		"win": "D:\\Dev",
	}
	tests := []struct {
		name string
		path string
		want string
	}{
		{"known root", "${dev}/waveterm", "~/Documents/Dev/waveterm"},
		{"root only", "${dev}", "~/Documents/Dev"},
		{"windows root normalizes", "${win}/proj", "D:/Dev/proj"},
		{"unknown root falls back to home", "${nope}/proj", "~"},
		{"tilde passes through", "~/Downloads", "~/Downloads"},
		{"absolute passes through", "/opt/data", "/opt/data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localizePath(tt.path, roots); got != tt.want {
				t.Errorf("localizePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPortablizeBlockMeta(t *testing.T) {
	home := wavebase.GetHomeDir()
	roots := map[string]string{"dev": home + "/Documents/Dev"}
	meta := waveobj.MetaMapType{
		waveobj.MetaKey_View:           "term",
		waveobj.MetaKey_CmdCwd:         home + "/Documents/Dev/waveterm",
		waveobj.MetaKey_History:        []string{home + "/old"},
		waveobj.MetaKey_HistoryForward: []string{home + "/older"},
	}
	out := portablizeBlockMeta(meta, roots)
	if got := out[waveobj.MetaKey_CmdCwd]; got != "${dev}/waveterm" {
		t.Errorf("cmd:cwd = %v, want ${dev}/waveterm", got)
	}
	if _, ok := out[waveobj.MetaKey_History]; ok {
		t.Errorf("history should be stripped from snapshot meta")
	}
	if _, ok := out[waveobj.MetaKey_HistoryForward]; ok {
		t.Errorf("history:forward should be stripped from snapshot meta")
	}
	// the original meta must not be mutated
	if meta[waveobj.MetaKey_CmdCwd] != home+"/Documents/Dev/waveterm" {
		t.Errorf("portablizeBlockMeta mutated the source meta")
	}
	if _, ok := meta[waveobj.MetaKey_History]; !ok {
		t.Errorf("portablizeBlockMeta mutated the source meta (history)")
	}
}

func TestPortablizeBlockMetaRemoteUntouched(t *testing.T) {
	home := wavebase.GetHomeDir()
	roots := map[string]string{"dev": home + "/Documents/Dev"}
	meta := waveobj.MetaMapType{
		waveobj.MetaKey_Connection: "user@host",
		waveobj.MetaKey_CmdCwd:     "/home/remoteuser/proj",
	}
	out := portablizeBlockMeta(meta, roots)
	if got := out[waveobj.MetaKey_CmdCwd]; got != "/home/remoteuser/proj" {
		t.Errorf("remote cmd:cwd = %v, want untouched /home/remoteuser/proj", got)
	}
}

func TestLocalizeBlockMeta(t *testing.T) {
	roots := map[string]string{"dev": "~/Documents/Dev"}
	meta := waveobj.MetaMapType{
		waveobj.MetaKey_View:   "preview",
		waveobj.MetaKey_File:   "${dev}/waveterm",
		waveobj.MetaKey_CmdCwd: "${dev}/waveterm",
	}
	out := localizeBlockMeta(meta, roots)
	if got := out[waveobj.MetaKey_File]; got != "~/Documents/Dev/waveterm" {
		t.Errorf("file = %v, want ~/Documents/Dev/waveterm", got)
	}
	if got := out[waveobj.MetaKey_CmdCwd]; got != "~/Documents/Dev/waveterm" {
		t.Errorf("cmd:cwd = %v, want ~/Documents/Dev/waveterm", got)
	}
	remote := waveobj.MetaMapType{
		waveobj.MetaKey_Connection: "user@host",
		waveobj.MetaKey_CmdCwd:     "${dev}/x",
	}
	outRemote := localizeBlockMeta(remote, roots)
	if got := outRemote[waveobj.MetaKey_CmdCwd]; got != "${dev}/x" {
		t.Errorf("remote meta should pass through, got %v", got)
	}
}
