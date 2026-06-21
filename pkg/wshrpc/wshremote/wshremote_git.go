// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshremote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

const (
	gitReadTimeout   = 15 * time.Second
	gitActionTimeout = 30 * time.Second
	gitSyncTimeout   = 90 * time.Second
)

// runGit runs `git -C <gitRoot> <args...>` non-interactively and returns stdout, stderr.
// Auth prompts are disabled so credential-requiring commands fail fast instead of hanging.
func runGit(ctx context.Context, gitRoot string, args ...string) (string, string, error) {
	fullArgs := append([]string{"-C", gitRoot}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes",
		"GIT_PAGER=cat",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func gitAction(ctx context.Context, gitRoot string, timeout time.Duration, args ...string) (*wshrpc.GitActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, err := runGit(ctx, gitRoot, args...)
	out := strings.TrimSpace(strings.TrimRight(stdout, "\n") + "\n" + strings.TrimRight(stderr, "\n"))
	return &wshrpc.GitActionResult{Success: err == nil, Output: out}, nil
}

func gitAtoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func gitAtoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func gitShortHash(h string) string {
	h = strings.TrimSpace(h)
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func (impl *ServerImpl) RemoteGitRepoInfoCommand(ctx context.Context, data wshrpc.CommandGitPathData) (*wshrpc.GitRepoInfo, error) {
	path := data.Path
	if path == "" {
		path = "~"
	}
	expanded, err := wavebase.ExpandHomeDir(path)
	if err != nil {
		return &wshrpc.GitRepoInfo{IsRepo: false, ErrorMsg: err.Error()}, nil
	}
	expanded = filepath.Clean(expanded)
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	stdout, stderr, err := runGit(ctx, expanded, "rev-parse", "--show-toplevel")
	if err != nil {
		return &wshrpc.GitRepoInfo{IsRepo: false, ErrorMsg: strings.TrimSpace(stderr)}, nil
	}
	return &wshrpc.GitRepoInfo{IsRepo: true, GitRoot: strings.TrimSpace(stdout)}, nil
}

func (impl *ServerImpl) RemoteGitStatusCommand(ctx context.Context, data wshrpc.CommandGitRootData) (*wshrpc.GitStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	root := data.GitRoot
	stdout, stderr, err := runGit(ctx, root, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return nil, fmt.Errorf("git status failed: %s", strings.TrimSpace(stderr))
	}
	status := &wshrpc.GitStatus{}
	fileMap := make(map[string]*wshrpc.GitFileStatus)
	var files []*wshrpc.GitFileStatus

	tokens := strings.Split(stdout, "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "" {
			continue
		}
		switch {
		case strings.HasPrefix(tok, "# "):
			parseStatusHeader(tok, status)
		case strings.HasPrefix(tok, "1 "):
			f := parseStatusEntry(tok, 8)
			fileMap[f.Path] = f
			files = append(files, f)
		case strings.HasPrefix(tok, "2 "):
			f := parseStatusEntry(tok, 9)
			if i+1 < len(tokens) {
				f.OrigPath = tokens[i+1]
				i++
			}
			fileMap[f.Path] = f
			files = append(files, f)
		case strings.HasPrefix(tok, "u "):
			f := parseStatusEntry(tok, 10)
			f.Staged = true
			f.Unstaged = true
			fileMap[f.Path] = f
			files = append(files, f)
		case strings.HasPrefix(tok, "? "):
			f := &wshrpc.GitFileStatus{Path: tok[2:], IndexStatus: ".", WorkStatus: "?", Untracked: true, Unstaged: true}
			fileMap[f.Path] = f
			files = append(files, f)
		}
	}

	mergeNumstat(ctx, root, false, fileMap)
	mergeNumstat(ctx, root, true, fileMap)

	status.Files = make([]wshrpc.GitFileStatus, 0, len(files))
	for _, f := range files {
		status.Files = append(status.Files, *f)
	}
	status.Clean = len(files) == 0
	status.StashCount = gitStashCount(ctx, root)
	return status, nil
}

func parseStatusHeader(tok string, st *wshrpc.GitStatus) {
	rest := strings.TrimPrefix(tok, "# ")
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return
	}
	switch fields[0] {
	case "branch.oid":
		st.Head = gitShortHash(fields[1])
	case "branch.head":
		if fields[1] == "(detached)" {
			st.Detached = true
		} else {
			st.Branch = fields[1]
		}
	case "branch.upstream":
		st.Upstream = fields[1]
	case "branch.ab":
		if len(fields) >= 3 {
			st.Ahead = gitAtoi(strings.TrimPrefix(fields[1], "+"))
			st.Behind = gitAtoi(strings.TrimPrefix(fields[2], "-"))
		}
	}
}

// parseStatusEntry parses a porcelain v2 entry. fieldsBeforePath is the count of
// space-separated fields that precede the pathname (8 for "1", 9 for "2", 10 for "u").
func parseStatusEntry(tok string, fieldsBeforePath int) *wshrpc.GitFileStatus {
	parts := strings.SplitN(tok, " ", fieldsBeforePath+1)
	xy := ""
	if len(parts) > 1 {
		xy = parts[1]
	}
	path := ""
	if len(parts) > fieldsBeforePath {
		path = parts[fieldsBeforePath]
	}
	idx, work := ".", "."
	if len(xy) >= 2 {
		idx = string(xy[0])
		work = string(xy[1])
	}
	f := &wshrpc.GitFileStatus{
		Path:        path,
		IndexStatus: idx,
		WorkStatus:  work,
		Staged:      idx != "." && idx != " ",
		Unstaged:    work != "." && work != " ",
	}
	return f
}

func mergeNumstat(ctx context.Context, root string, staged bool, fileMap map[string]*wshrpc.GitFileStatus) {
	args := []string{"diff", "--numstat", "-z"}
	if staged {
		args = append(args, "--cached")
	}
	stdout, _, err := runGit(ctx, root, args...)
	if err != nil {
		return
	}
	tokens := strings.Split(stdout, "\x00")
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t == "" {
			continue
		}
		cols := strings.SplitN(t, "\t", 3)
		if len(cols) < 3 {
			continue
		}
		addS, remS, path := cols[0], cols[1], cols[2]
		if path == "" {
			// rename: numstat -z emits "added\tremoved\t\0oldpath\0newpath"
			if i+2 < len(tokens) {
				path = tokens[i+2]
				i += 2
			}
		}
		f := fileMap[path]
		if f == nil {
			continue
		}
		if addS == "-" || remS == "-" {
			f.Binary = true
			continue
		}
		f.Added += gitAtoi(addS)
		f.Removed += gitAtoi(remS)
	}
}

func gitStashCount(ctx context.Context, root string) int {
	stdout, _, err := runGit(ctx, root, "stash", "list")
	if err != nil {
		return 0
	}
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return 0
	}
	return len(strings.Split(stdout, "\n"))
}

func (impl *ServerImpl) RemoteGitBranchesCommand(ctx context.Context, data wshrpc.CommandGitRootData) (*wshrpc.GitBranchList, error) {
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	const format = "%(refname:short)%00%(upstream:short)%00%(HEAD)%00%(committerdate:unix)"
	stdout, stderr, err := runGit(ctx, data.GitRoot, "for-each-ref", "--format="+format, "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref failed: %s", strings.TrimSpace(stderr))
	}
	rtn := &wshrpc.GitBranchList{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 4 {
			continue
		}
		b := wshrpc.GitBranch{Name: f[0], Upstream: f[1], LastCommitTs: gitAtoi64(f[3])}
		if f[2] == "*" {
			b.IsCurrent = true
			rtn.Current = f[0]
		}
		rtn.Branches = append(rtn.Branches, b)
	}
	return rtn, nil
}

func (impl *ServerImpl) RemoteGitLogCommand(ctx context.Context, data wshrpc.CommandGitLogData) (*wshrpc.GitLog, error) {
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	limit := data.Limit
	if limit <= 0 {
		limit = 50
	}
	ref := data.Ref
	if ref == "" {
		ref = "HEAD"
	}
	const format = "--pretty=format:%h%x00%H%x00%an%x00%ae%x00%at%x00%s"
	args := []string{"log", fmt.Sprintf("--skip=%d", data.Offset), fmt.Sprintf("--max-count=%d", limit+1), format, ref}
	stdout, _, err := runGit(ctx, data.GitRoot, args...)
	if err != nil {
		// empty repo / bad ref → no commits
		return &wshrpc.GitLog{}, nil
	}
	rtn := &wshrpc.GitLog{}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return rtn, nil
	}
	if len(lines) > limit {
		rtn.HasMore = true
		lines = lines[:limit]
	}
	for _, line := range lines {
		f := strings.Split(line, "\x00")
		if len(f) < 6 {
			continue
		}
		rtn.Commits = append(rtn.Commits, wshrpc.GitCommit{
			Hash:        f[0],
			FullHash:    f[1],
			Author:      f[2],
			AuthorEmail: f[3],
			Ts:          gitAtoi64(f[4]),
			Subject:     f[5],
		})
	}
	return rtn, nil
}

func (impl *ServerImpl) RemoteGitCheckoutCommand(ctx context.Context, data wshrpc.CommandGitCheckoutData) (*wshrpc.GitActionResult, error) {
	args := []string{"checkout"}
	if data.Create {
		args = append(args, "-b")
	}
	args = append(args, data.Branch)
	return gitAction(ctx, data.GitRoot, gitActionTimeout, args...)
}

func (impl *ServerImpl) RemoteGitStageCommand(ctx context.Context, data wshrpc.CommandGitFilesData) (*wshrpc.GitActionResult, error) {
	if len(data.Paths) == 0 {
		return gitAction(ctx, data.GitRoot, gitActionTimeout, "add", "-A")
	}
	return gitAction(ctx, data.GitRoot, gitActionTimeout, append([]string{"add", "--"}, data.Paths...)...)
}

func (impl *ServerImpl) RemoteGitUnstageCommand(ctx context.Context, data wshrpc.CommandGitFilesData) (*wshrpc.GitActionResult, error) {
	paths := data.Paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return gitAction(ctx, data.GitRoot, gitActionTimeout, append([]string{"restore", "--staged", "--"}, paths...)...)
}

func (impl *ServerImpl) RemoteGitCommitCommand(ctx context.Context, data wshrpc.CommandGitCommitData) (*wshrpc.GitActionResult, error) {
	args := []string{"commit"}
	if data.All {
		args = append(args, "-a")
	}
	if data.Amend {
		args = append(args, "--amend")
	}
	args = append(args, "-m", data.Message)
	return gitAction(ctx, data.GitRoot, gitActionTimeout, args...)
}

func (impl *ServerImpl) RemoteGitDiscardCommand(ctx context.Context, data wshrpc.CommandGitFilesData) (*wshrpc.GitActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, gitActionTimeout)
	defer cancel()
	root := data.GitRoot
	restorePaths := data.Paths
	if len(restorePaths) == 0 {
		restorePaths = []string{"."}
	}
	restoreOut, restoreErr, _ := runGit(ctx, root, append([]string{"restore", "--"}, restorePaths...)...)
	cleanArgs := []string{"clean", "-fd"}
	if len(data.Paths) > 0 {
		cleanArgs = append(append(cleanArgs, "--"), data.Paths...)
	}
	cleanOut, cleanErr, err := runGit(ctx, root, cleanArgs...)
	out := strings.TrimSpace(strings.Join([]string{
		strings.TrimRight(restoreOut, "\n"), strings.TrimRight(restoreErr, "\n"),
		strings.TrimRight(cleanOut, "\n"), strings.TrimRight(cleanErr, "\n"),
	}, "\n"))
	return &wshrpc.GitActionResult{Success: err == nil, Output: out}, nil
}

func (impl *ServerImpl) RemoteGitSyncCommand(ctx context.Context, data wshrpc.CommandGitSyncData) (*wshrpc.GitActionResult, error) {
	remote := data.Remote
	if remote == "" {
		remote = "origin"
	}
	var args []string
	switch data.Action {
	case wshrpc.GitSyncPull:
		args = []string{"pull"}
	case wshrpc.GitSyncFetch:
		args = []string{"fetch", remote}
	case wshrpc.GitSyncPush:
		if data.SetUpstream {
			args = []string{"push", "-u", remote, "HEAD"}
		} else {
			args = []string{"push"}
		}
	default:
		return &wshrpc.GitActionResult{Success: false, Output: "unknown sync action: " + data.Action}, nil
	}
	return gitAction(ctx, data.GitRoot, gitSyncTimeout, args...)
}

func (impl *ServerImpl) RemoteGitStashCommand(ctx context.Context, data wshrpc.CommandGitStashData) (*wshrpc.GitActionResult, error) {
	var args []string
	switch data.Action {
	case wshrpc.GitStashPush:
		args = []string{"stash", "push"}
		if data.Message != "" {
			args = append(args, "-m", data.Message)
		}
	case wshrpc.GitStashPop:
		args = []string{"stash", "pop"}
	case wshrpc.GitStashApply:
		args = []string{"stash", "apply"}
	case wshrpc.GitStashDrop:
		args = []string{"stash", "drop", fmt.Sprintf("stash@{%d}", data.Index)}
	case wshrpc.GitStashList:
		args = []string{"stash", "list"}
	default:
		return &wshrpc.GitActionResult{Success: false, Output: "unknown stash action: " + data.Action}, nil
	}
	return gitAction(ctx, data.GitRoot, gitActionTimeout, args...)
}

func (impl *ServerImpl) RemoteGitDiffCommand(ctx context.Context, data wshrpc.CommandGitDiffData) (*wshrpc.GitDiff, error) {
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	args := []string{"diff"}
	if data.Staged {
		args = append(args, "--cached")
	}
	if data.FullContext {
		// a huge context count makes the single hunk span the whole file, so the
		// frontend can render the full file with +/- lines instead of just hunks
		args = append(args, "--unified=1000000")
	}
	args = append(args, "--", data.Path)
	stdout, stderr, err := runGit(ctx, data.GitRoot, args...)
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %s", strings.TrimSpace(stderr))
	}
	rtn := &wshrpc.GitDiff{Path: data.Path, Diff: stdout}
	if strings.Contains(stdout, "Binary files") {
		rtn.Binary = true
	}
	return rtn, nil
}
