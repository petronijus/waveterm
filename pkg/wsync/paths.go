// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"log"
	"regexp"
	"strings"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
)

// Portable path handling for saved layouts. Block metas carry machine-absolute
// paths (a terminal's cmd:cwd comes from OSC 7, a preview's file from navigation),
// which breaks restoring a layout on another OS where the same project lives
// elsewhere. On save, paths are rewritten into machine-neutral form; on load,
// they're resolved back through this machine's config:
//
//	${name}/rest — named root from the machine-local sync:pathroots setting
//	~/rest       — home-relative; the consumers (shell start, preview) expand it
//
// Only local blocks are rewritten: a block with a connection points at paths on
// the remote host, which stay valid no matter which machine opens the layout.

var pathRootToken = regexp.MustCompile(`^\$\{([a-zA-Z0-9_-]+)\}(?:/(.*))?$`)

// portableBlockPathKeys are the block meta keys that hold a filesystem location.
var portableBlockPathKeys = []string{waveobj.MetaKey_CmdCwd, waveobj.MetaKey_File}

func getPathRoots() map[string]string {
	return wconfig.GetWatcher().GetFullConfig().Settings.SyncPathRoots
}

// normPath normalizes separators to "/" and trims trailing slashes so paths from
// different OSes (and OSC 7 vs native dialogs) compare consistently.
func normPath(p string) string {
	n := strings.ReplaceAll(p, "\\", "/")
	for len(n) > 1 && strings.HasSuffix(n, "/") {
		n = strings.TrimSuffix(n, "/")
	}
	return n
}

// makePortablePath rewrites an absolute local path into machine-neutral form:
// the longest-prefix named root wins, then home-relative ~, otherwise the path
// is returned unchanged (and stays machine-specific by design).
func makePortablePath(path string, roots map[string]string) string {
	if path == "" || strings.HasPrefix(path, "~") || pathRootToken.MatchString(path) {
		return path
	}
	norm := normPath(path)
	bestName, bestRoot := "", ""
	for name, rootVal := range roots {
		rootAbs := normPath(wavebase.ExpandHomeDirSafe(rootVal))
		if rootAbs == "" || rootAbs == "/" {
			continue
		}
		if norm != rootAbs && !strings.HasPrefix(norm, rootAbs+"/") {
			continue
		}
		if len(rootAbs) > len(bestRoot) {
			bestName, bestRoot = name, rootAbs
		}
	}
	if bestName != "" {
		if norm == bestRoot {
			return "${" + bestName + "}"
		}
		return "${" + bestName + "}" + norm[len(bestRoot):]
	}
	home := normPath(wavebase.GetHomeDir())
	if home != "" && home != "/" {
		if norm == home {
			return "~"
		}
		if strings.HasPrefix(norm, home+"/") {
			return "~" + norm[len(home):]
		}
	}
	return path
}

// localizePath resolves a ${name}/rest token through this machine's roots. An
// unknown root falls back to ~ so the block still opens (in home) instead of
// erroring on a path that can't exist here. ~-form paths pass through — the
// consumers expand them against the local home.
func localizePath(path string, roots map[string]string) string {
	m := pathRootToken.FindStringSubmatch(path)
	if m == nil {
		return path
	}
	name, rest := m[1], m[2]
	rootVal, ok := roots[name]
	if !ok || rootVal == "" {
		log.Printf("wsync: layout path root %q not in sync:pathroots on this machine, falling back to ~ (path %q)\n", name, path)
		return "~"
	}
	rootVal = normPath(rootVal)
	if rest == "" {
		return rootVal
	}
	return rootVal + "/" + rest
}

// portablizeBlockMeta returns a copy of the block meta ready for a layout
// snapshot: navigation history is dropped (it holds stale machine-absolute
// paths and means nothing in a freshly created block) and, for local blocks,
// the location keys are rewritten into portable form.
func portablizeBlockMeta(meta waveobj.MetaMapType, roots map[string]string) waveobj.MetaMapType {
	if meta == nil {
		return meta
	}
	out := make(waveobj.MetaMapType, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	delete(out, waveobj.MetaKey_History)
	delete(out, waveobj.MetaKey_HistoryForward)
	conn, _ := out[waveobj.MetaKey_Connection].(string)
	if conn != "" {
		return out
	}
	for _, key := range portableBlockPathKeys {
		if s, ok := out[key].(string); ok && s != "" {
			out[key] = makePortablePath(s, roots)
		}
	}
	return out
}

// localizeBlockMeta resolves portable location keys in a snapshot block meta
// against this machine's roots before the block is created.
func localizeBlockMeta(meta waveobj.MetaMapType, roots map[string]string) waveobj.MetaMapType {
	if meta == nil {
		return meta
	}
	out := make(waveobj.MetaMapType, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	conn, _ := out[waveobj.MetaKey_Connection].(string)
	if conn != "" {
		return out
	}
	for _, key := range portableBlockPathKeys {
		if s, ok := out[key].(string); ok && s != "" {
			out[key] = localizePath(s, roots)
		}
	}
	return out
}
