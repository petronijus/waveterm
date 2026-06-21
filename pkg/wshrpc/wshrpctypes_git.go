// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// git-related types and methods for wsh rpc calls
package wshrpc

import "context"

type WshRpcRemoteGitInterface interface {
	RemoteGitRepoInfoCommand(ctx context.Context, data CommandGitPathData) (*GitRepoInfo, error)
	RemoteGitStatusCommand(ctx context.Context, data CommandGitRootData) (*GitStatus, error)
	RemoteGitBranchesCommand(ctx context.Context, data CommandGitRootData) (*GitBranchList, error)
	RemoteGitLogCommand(ctx context.Context, data CommandGitLogData) (*GitLog, error)
	RemoteGitCheckoutCommand(ctx context.Context, data CommandGitCheckoutData) (*GitActionResult, error)
	RemoteGitStageCommand(ctx context.Context, data CommandGitFilesData) (*GitActionResult, error)
	RemoteGitUnstageCommand(ctx context.Context, data CommandGitFilesData) (*GitActionResult, error)
	RemoteGitCommitCommand(ctx context.Context, data CommandGitCommitData) (*GitActionResult, error)
	RemoteGitDiscardCommand(ctx context.Context, data CommandGitFilesData) (*GitActionResult, error)
	RemoteGitSyncCommand(ctx context.Context, data CommandGitSyncData) (*GitActionResult, error)
	RemoteGitStashCommand(ctx context.Context, data CommandGitStashData) (*GitActionResult, error)
	RemoteGitDiffCommand(ctx context.Context, data CommandGitDiffData) (*GitDiff, error)
}

const (
	GitSyncPull  = "pull"
	GitSyncPush  = "push"
	GitSyncFetch = "fetch"

	GitStashPush  = "push"
	GitStashPop   = "pop"
	GitStashApply = "apply"
	GitStashDrop  = "drop"
	GitStashList  = "list"
)

type CommandGitPathData struct {
	Path string `json:"path"` // a cwd inside the repo (may contain "~")
}

type CommandGitRootData struct {
	GitRoot string `json:"gitroot"`
}

type CommandGitLogData struct {
	GitRoot string `json:"gitroot"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

type CommandGitCheckoutData struct {
	GitRoot string `json:"gitroot"`
	Branch  string `json:"branch"`
	Create  bool   `json:"create,omitempty"`
}

type CommandGitFilesData struct {
	GitRoot string   `json:"gitroot"`
	Paths   []string `json:"paths,omitempty"` // repo-relative; empty = all
}

type CommandGitCommitData struct {
	GitRoot string `json:"gitroot"`
	Message string `json:"message"`
	Amend   bool   `json:"amend,omitempty"`
	All     bool   `json:"all,omitempty"`
}

type CommandGitSyncData struct {
	GitRoot     string `json:"gitroot"`
	Action      string `json:"action"` // pull | push | fetch
	Remote      string `json:"remote,omitempty"`
	SetUpstream bool   `json:"setupstream,omitempty"`
}

type CommandGitStashData struct {
	GitRoot string `json:"gitroot"`
	Action  string `json:"action"` // push | pop | apply | drop | list
	Index   int    `json:"index,omitempty"`
	Message string `json:"message,omitempty"`
}

type CommandGitDiffData struct {
	GitRoot string `json:"gitroot"`
	Path    string `json:"path"`             // repo-relative file
	Staged  bool   `json:"staged,omitempty"` // diff the staged version
}

type GitRepoInfo struct {
	IsRepo   bool   `json:"isrepo"`
	GitRoot  string `json:"gitroot,omitempty"` // absolute, resolved
	ErrorMsg string `json:"errormsg,omitempty"`
}

type GitStatus struct {
	Branch     string          `json:"branch"` // "" if detached
	Detached   bool            `json:"detached,omitempty"`
	Head       string          `json:"head"` // short hash
	Upstream   string          `json:"upstream,omitempty"`
	Ahead      int             `json:"ahead,omitempty"`
	Behind     int             `json:"behind,omitempty"`
	Files      []GitFileStatus `json:"files,omitempty"`
	StashCount int             `json:"stashcount,omitempty"`
	Clean      bool            `json:"clean,omitempty"`
}

type GitFileStatus struct {
	Path        string `json:"path"`
	OrigPath    string `json:"origpath,omitempty"` // for renames
	IndexStatus string `json:"indexstatus"`        // 'M','A','D','R','C','U',' ','?'
	WorkStatus  string `json:"workstatus"`
	Staged      bool   `json:"staged"`
	Unstaged    bool   `json:"unstaged"`
	Untracked   bool   `json:"untracked,omitempty"`
	Added       int    `json:"added"`   // +
	Removed     int    `json:"removed"` // -
	Binary      bool   `json:"binary,omitempty"`
}

type GitBranchList struct {
	Current  string      `json:"current"`
	Branches []GitBranch `json:"branches,omitempty"`
}

type GitBranch struct {
	Name         string `json:"name"`
	IsCurrent    bool   `json:"iscurrent,omitempty"`
	Upstream     string `json:"upstream,omitempty"`
	Ahead        int    `json:"ahead,omitempty"`
	Behind       int    `json:"behind,omitempty"`
	LastCommitTs int64  `json:"lastcommitts,omitempty"`
}

type GitLog struct {
	Commits []GitCommit `json:"commits,omitempty"`
	HasMore bool        `json:"hasmore,omitempty"`
}

type GitCommit struct {
	Hash        string `json:"hash"` // short
	FullHash    string `json:"fullhash"`
	Author      string `json:"author"`
	AuthorEmail string `json:"authoremail,omitempty"`
	Ts          int64  `json:"ts"` // unix
	Subject     string `json:"subject"`
}

type GitActionResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"` // combined stdout/stderr
}

type GitDiff struct {
	Path   string `json:"path"`
	Diff   string `json:"diff"` // unified diff text
	Binary bool   `json:"binary,omitempty"`
}
