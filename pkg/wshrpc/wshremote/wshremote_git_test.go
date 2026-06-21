// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshremote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

func gitTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTestRun(t, dir, "init", "-b", "main")
	gitTestRun(t, dir, "config", "user.name", "test")
	gitTestRun(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, dir, "add", "a.txt")
	gitTestRun(t, dir, "commit", "-m", "initial commit")
	return dir
}

func TestGitRepoInfoAndStatus(t *testing.T) {
	impl := &ServerImpl{}
	ctx := context.Background()
	dir := setupTestRepo(t)

	info, err := impl.RemoteGitRepoInfoCommand(ctx, wshrpc.CommandGitPathData{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsRepo {
		t.Fatalf("expected isrepo=true, got errormsg=%q", info.ErrorMsg)
	}
	root := info.GitRoot

	// modify a tracked file (adds 1 line, removes 1) and add an untracked file
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nCHANGED\nline3\nline4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	st, err := impl.RemoteGitStatusCommand(ctx, wshrpc.CommandGitRootData{GitRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" {
		t.Errorf("expected branch main, got %q", st.Branch)
	}
	if st.Clean {
		t.Errorf("expected dirty working tree")
	}
	var aFile, bFile *wshrpc.GitFileStatus
	for i := range st.Files {
		switch st.Files[i].Path {
		case "a.txt":
			aFile = &st.Files[i]
		case "b.txt":
			bFile = &st.Files[i]
		}
	}
	if aFile == nil {
		t.Fatal("a.txt not in status")
	}
	if aFile.Added != 2 || aFile.Removed != 1 {
		t.Errorf("a.txt expected +2/-1, got +%d/-%d", aFile.Added, aFile.Removed)
	}
	if !aFile.Unstaged {
		t.Errorf("a.txt expected unstaged")
	}
	if bFile == nil || !bFile.Untracked {
		t.Errorf("b.txt expected untracked, got %+v", bFile)
	}
}

func TestGitStageCommitAndLog(t *testing.T) {
	impl := &ServerImpl{}
	ctx := context.Background()
	dir := setupTestRepo(t)
	info, _ := impl.RemoteGitRepoInfoCommand(ctx, wshrpc.CommandGitPathData{Path: dir})
	root := info.GitRoot

	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if res, err := impl.RemoteGitStageCommand(ctx, wshrpc.CommandGitFilesData{GitRoot: root, Paths: []string{"c.txt"}}); err != nil || !res.Success {
		t.Fatalf("stage failed: err=%v out=%v", err, res)
	}

	st, _ := impl.RemoteGitStatusCommand(ctx, wshrpc.CommandGitRootData{GitRoot: root})
	var staged bool
	for _, f := range st.Files {
		if f.Path == "c.txt" && f.Staged {
			staged = true
		}
	}
	if !staged {
		t.Fatalf("c.txt expected staged, files=%+v", st.Files)
	}

	if res, err := impl.RemoteGitCommitCommand(ctx, wshrpc.CommandGitCommitData{GitRoot: root, Message: "add c"}); err != nil || !res.Success {
		t.Fatalf("commit failed: err=%v out=%v", err, res)
	}

	log, err := impl.RemoteGitLogCommand(ctx, wshrpc.CommandGitLogData{GitRoot: root, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(log.Commits))
	}
	if log.Commits[0].Subject != "add c" {
		t.Errorf("expected newest subject 'add c', got %q", log.Commits[0].Subject)
	}
	if log.Commits[0].Author != "test" {
		t.Errorf("expected author test, got %q", log.Commits[0].Author)
	}
}

func TestGitBranches(t *testing.T) {
	impl := &ServerImpl{}
	ctx := context.Background()
	dir := setupTestRepo(t)
	info, _ := impl.RemoteGitRepoInfoCommand(ctx, wshrpc.CommandGitPathData{Path: dir})
	root := info.GitRoot

	if res, err := impl.RemoteGitCheckoutCommand(ctx, wshrpc.CommandGitCheckoutData{GitRoot: root, Branch: "feature", Create: true}); err != nil || !res.Success {
		t.Fatalf("checkout -b failed: err=%v out=%v", err, res)
	}

	branches, err := impl.RemoteGitBranchesCommand(ctx, wshrpc.CommandGitRootData{GitRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if branches.Current != "feature" {
		t.Errorf("expected current=feature, got %q", branches.Current)
	}
	names := map[string]bool{}
	for _, b := range branches.Branches {
		names[b.Name] = true
	}
	if !names["main"] || !names["feature"] {
		t.Errorf("expected main+feature branches, got %v", names)
	}
}

func TestGitRepoInfoNotARepo(t *testing.T) {
	impl := &ServerImpl{}
	ctx := context.Background()
	dir := t.TempDir()
	info, err := impl.RemoteGitRepoInfoCommand(ctx, wshrpc.CommandGitPathData{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if info.IsRepo {
		t.Errorf("expected isrepo=false for non-repo dir")
	}
}
