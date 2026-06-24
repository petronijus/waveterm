// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
)

// LocalFolderTransport stores state files in a plain local directory — typically a
// folder inside a Nextcloud / Dropbox / Drive *desktop-client* sync root. The user's
// existing desktop client does the cross-machine transport, so Wave needs no URL,
// account, or password at all — just a path. Because each install only ever writes
// its own state.<installid>.json, the desktop client never sees two machines touch
// the same file and so never produces conflict copies.
type LocalFolderTransport struct {
	dir string
}

// MakeLocalFolderTransport expands ~ / $HOME in the path. The directory itself is
// created lazily by EnsureFolder.
func MakeLocalFolderTransport(folderPath string) *LocalFolderTransport {
	return &LocalFolderTransport{dir: wavebase.ExpandHomeDirSafe(strings.TrimSpace(folderPath))}
}

func (t *LocalFolderTransport) EnsureFolder(ctx context.Context) error {
	return os.MkdirAll(t.dir, 0o755)
}

func (t *LocalFolderTransport) Get(ctx context.Context, name string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(t.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func (t *LocalFolderTransport) Put(ctx context.Context, name string, data []byte) error {
	return atomicWriteFile(filepath.Join(t.dir, name), data)
}

func (t *LocalFolderTransport) Delete(ctx context.Context, name string) error {
	if err := os.Remove(filepath.Join(t.dir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListStateFiles returns the state.<installid>.json basenames in the folder. The
// temp files atomicWriteFile leaves mid-write are dot-prefixed (.wsync-*.tmp) and
// never match the state.* / .json bracketing, so they are naturally skipped.
func (t *LocalFolderTransport) ListStateFiles(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, StateFilePrefix) && strings.HasSuffix(name, StateFileSuffix) {
			names = append(names, name)
		}
	}
	return names, nil
}

// ListFiles returns the "<prefix>*.json" basenames in the folder. Dot-prefixed
// temp files (.wsync-*.tmp) never match the .json suffix and are skipped.
func (t *LocalFolderTransport) ListFiles(ctx context.Context, prefix string) ([]string, error) {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, StateFileSuffix) {
			names = append(names, name)
		}
	}
	return names, nil
}
