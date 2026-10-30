// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain isolates git from the developer's own configuration: no global or
// system config, a fixed identity, deterministic branch name.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wgo-gitconfig")
	if err != nil {
		panic(err)
	}
	cfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = Test\n\temail = test@example.invalid\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n"), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, file, content string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, file), content)
	git(t, dir, "add", file)
	git(t, dir, "commit", "-q", "-m", "change "+file)
}

// world is a bare remote outside the scan root and clones inside it.
type world struct {
	root   string // scan root
	remote string
}

// realDir resolves symlinks: macOS temp dirs live under /var -> /private/var,
// and FSEvents reports the resolved path.
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := realDir(t)
	w := &world{root: filepath.Join(base, "dev"), remote: filepath.Join(base, "remote.git")}
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "--bare", w.remote)
	seed := filepath.Join(base, "seed")
	git(t, base, "clone", "-q", w.remote, seed)
	commit(t, seed, "README", "seed\n")
	git(t, seed, "push", "-q", "origin", "main")
	return w
}

func (w *world) clone(t *testing.T, rel string) string {
	t.Helper()
	dir := filepath.Join(w.root, rel)
	git(t, w.root, "clone", "-q", w.remote, dir)
	return dir
}

// eventually polls cond until it holds or the deadline passes.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}
