// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// git overview: which local repos are out of sync (pkg/gitoverview)
package wshrpc

import "context"

type WshRpcGitOverviewInterface interface {
	// GitOverviewStartCommand starts (or reconfigures) the overview engine of
	// a block and returns its current state; updates follow as
	// Event_GitOverview scoped to the block. The engine stops when the block
	// closes.
	GitOverviewStartCommand(ctx context.Context, data CommandGitOverviewStartData) (GitOverviewData, error)
	// GitOverviewSyncCommand fast-forwards or pushes the given repos, each
	// only with the action the user saw on its row.
	GitOverviewSyncCommand(ctx context.Context, data CommandGitOverviewSyncData) error
	// GitOverviewRefreshCommand rescans the roots and fetches every repo now.
	GitOverviewRefreshCommand(ctx context.Context, data CommandGitOverviewBlockData) error
}

const (
	GitOverviewActionPull = "pull"
	GitOverviewActionPush = "push"
)

type CommandGitOverviewStartData struct {
	BlockId      string   `json:"blockid"`
	Roots        []string `json:"roots"`
	MaxDepth     int      `json:"maxdepth,omitempty"`
	Ignore       []string `json:"ignore,omitempty"`
	FetchMinutes int      `json:"fetchminutes,omitempty"`
}

type CommandGitOverviewBlockData struct {
	BlockId string `json:"blockid"`
}

type GitOverviewSyncTarget struct {
	Path   string `json:"path"`
	Action string `json:"action"` // GitOverviewActionPull or GitOverviewActionPush
}

type CommandGitOverviewSyncData struct {
	BlockId string                  `json:"blockid"`
	Targets []GitOverviewSyncTarget `json:"targets"`
}

type GitOverviewRepo struct {
	Path     string `json:"path"` // absolute worktree root, the row's identity
	Rel      string `json:"rel"`  // relative to its scan root
	Branch   string `json:"branch,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Changed  int    `json:"changed,omitempty"` // changed + untracked + conflicted entries
	State    string `json:"state"`
	Action   string `json:"action,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Busy     string `json:"busy,omitempty"`     // "fetching", "pulling", "pushing"
	Err      string `json:"err,omitempty"`      // last sync failure
	FetchErr string `json:"fetcherr,omitempty"` // last background fetch failure
	Activity int64  `json:"activity,omitempty"` // unix ms of the last reflog write
}

type GitOverviewData struct {
	Repos     []GitOverviewRepo `json:"repos"`
	Watching  string            `json:"watching"`            // how changes are detected
	LastFetch int64             `json:"lastfetch,omitempty"` // unix ms
	NextFetch int64             `json:"nextfetch,omitempty"` // unix ms
	Fetching  bool              `json:"fetching,omitempty"`
	ScanErr   string            `json:"scanerr,omitempty"`
}
