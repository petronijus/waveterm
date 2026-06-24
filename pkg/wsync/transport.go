// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import "context"

// Transport is the storage backend that sync reads and writes its per-install
// state files through. It is deliberately tiny: the whole protocol is "ensure the
// folder, put my own state file, list peers' state files, get each one" — so a new
// backend (a local desktop-synced folder, WebDAV, and later a cloud API such as
// Google Drive) only has to implement these few verbs. Single-writer-per-file means
// no backend ever needs locking or conflict resolution.
type Transport interface {
	// EnsureFolder makes sure the shared sync folder exists (idempotent).
	EnsureFolder(ctx context.Context) error
	// Get reads a state file; returns (nil, false, nil) when it does not exist.
	Get(ctx context.Context, name string) ([]byte, bool, error)
	// Put writes (overwriting) a state file.
	Put(ctx context.Context, name string, data []byte) error
	// Delete removes a state file; a missing file is treated as success.
	Delete(ctx context.Context, name string) error
	// ListStateFiles returns the basenames of all state.<installid>.json files.
	ListStateFiles(ctx context.Context) ([]string, error)
	// ListFiles returns the basenames of all "<prefix>*.json" files in the folder
	// (used to enumerate named artifacts such as saved layouts).
	ListFiles(ctx context.Context, prefix string) ([]string, error)
}

// compile-time assertions that the backends satisfy Transport.
var (
	_ Transport = (*WebDAVClient)(nil)
	_ Transport = (*LocalFolderTransport)(nil)
)
