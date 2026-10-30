// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Timeouts per kind of git call. A hung network call must not pin a worker.
const (
	statusTimeout = 30 * time.Second
	fetchTimeout  = 90 * time.Second
	mergeTimeout  = 60 * time.Second
	pushTimeout   = 120 * time.Second
)

// gitConfigArgs go before every subcommand. No automatic maintenance or gc
// (fetch and merge would otherwise spawn a detached `git maintenance run`
// that can repack and prune outside our timeout), and no fsmonitor daemon:
// a repo with core.fsmonitor=true would get a long-lived daemon started by a
// background status, whose cookie writes then wake our own watcher.
var gitConfigArgs = []string{
	"-c", "core.fsmonitor=false",
	"-c", "maintenance.auto=false",
	"-c", "gc.auto=0",
}

// localEnvVars are the variables that point git at a particular repo
// (`git rev-parse --local-env-vars`) plus inherited -c parameters. Inherited
// from whatever started Wave, they would aim every call at the wrong repo.
var localEnvVars = map[string]bool{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_CONFIG": true, "GIT_CONFIG_PARAMETERS": true,
	"GIT_CONFIG_COUNT": true, "GIT_OBJECT_DIRECTORY": true, "GIT_DIR": true, "GIT_WORK_TREE": true,
	"GIT_IMPLICIT_WORK_TREE": true, "GIT_GRAFT_FILE": true, "GIT_INDEX_FILE": true,
	"GIT_NO_REPLACE_OBJECTS": true, "GIT_REPLACE_REF_BASE": true, "GIT_PREFIX": true,
	"GIT_SHALLOW_FILE": true, "GIT_COMMON_DIR": true,
}

// gitEnv makes every call non-interactive and side-effect free where git
// allows it: no optional index refresh or lock (status must not race the
// user's own git or wake file watchers), no prompt of any kind — terminal,
// askpass program (an empty GIT_ASKPASS makes git skip core.askPass and
// SSH_ASKPASS) or ssh's own askpass — and stable output.
func gitEnv() []string {
	env := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if localEnvVars[name] || strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS_REQUIRE=never",
		"GCM_INTERACTIVE=never",
		"LC_ALL=C",
	)
}

// runGit runs git in dir. Background calls go through lowPriority (see
// prio_*.go) so periodic scans never compete with what the user is doing;
// calls the user clicked run at normal priority.
func runGit(ctx context.Context, dir string, background bool, args ...string) ([]byte, error) {
	out, _, err := runGitInput(ctx, dir, background, nil, nil, args...)
	return out, err
}

// runGitInput is runGit with stdin, extra environment and the exit code, for
// commands whose non-zero exit is an answer rather than a failure. git runs
// in its own process group (see proc_*.go), so a timeout kills its children
// (ssh, remote helpers, hooks) too.
func runGitInput(ctx context.Context, dir string, background bool, stdin []byte, extraEnv []string, args ...string) ([]byte, int, error) {
	argv := append(append([]string{"git"}, gitConfigArgs...), args...)
	if background {
		argv = lowPriority(argv)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	ownProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, -1, errors.New("git " + args[0] + " timed out")
		}
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return stdout.Bytes(), code, errors.New(lastLine(stderr.String(), err.Error()))
	}
	return stdout.Bytes(), 0, nil
}

// lastLine returns the last non-empty line of git's stderr: the one that says
// what went wrong ("fatal: Not possible to fast-forward, aborting.").
func lastLine(s, fallback string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return truncate(l, 200)
		}
	}
	return fallback
}

func gitStatus(ctx context.Context, dir string, background bool) (statusInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, err := runGit(ctx, dir, background, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return statusInfo{}, err
	}
	return parsePorcelainV2(out)
}

// sshEnv makes ssh non-interactive for network calls: BatchMode fails a
// host-key or password prompt instead of waiting for it. It is left alone
// when the user chose an ssh command (GIT_SSH_COMMAND, GIT_SSH or the repo's
// core.sshCommand), which GIT_SSH_COMMAND would otherwise override.
func sshEnv(ctx context.Context, dir string) []string {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return nil
	}
	if out, _ := runGit(ctx, dir, true, "config", "--get", "core.sshCommand"); len(bytes.TrimSpace(out)) > 0 {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
}

func gitFetch(ctx context.Context, dir string, background bool) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	_, _, err := runGitInput(ctx, dir, background, nil, sshEnv(ctx, dir),
		"fetch", "--quiet", "--no-auto-maintenance", "--no-write-commit-graph")
	return err
}

// gitFastForward moves the branch to its upstream, and only forward. It
// refuses rather than overwrite a gitignored file the incoming commits start
// tracking (a local .env, say), and never autostashes.
func gitFastForward(ctx context.Context, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, mergeTimeout)
	defer cancel()
	_, err := runGit(ctx, dir, false,
		"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", "--quiet", "@{upstream}")
	return err
}

// upstreamOf returns the remote and the remote ref branch tracks, e.g.
// "origin", "refs/heads/main".
func upstreamOf(ctx context.Context, dir, branch string) (remote, ref string, err error) {
	out, err := runGit(ctx, dir, false, "for-each-ref",
		"--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
	if err != nil {
		return "", "", err
	}
	remote, ref, _ = strings.Cut(strings.TrimSpace(string(out)), "\x00")
	if remote == "" || remote == "." || !strings.HasPrefix(ref, "refs/heads/") {
		return "", "", fmt.Errorf("%s does not track a remote branch", branch)
	}
	return remote, ref, nil
}

// gitPush pushes exactly one ref, non-forced: the branch to the upstream
// branch its ahead/behind counts were measured against. An explicit refspec
// overrides configured push refspecs (which may carry "+" and push other
// branches), push.default=matching and mirror remotes; no tags follow.
func gitPush(ctx context.Context, dir, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	remote, ref, err := upstreamOf(ctx, dir, branch)
	if err != nil {
		return err
	}
	_, _, err = runGitInput(ctx, dir, false, nil, sshEnv(ctx, dir),
		"push", "--quiet", "--no-follow-tags", "--", remote, "refs/heads/"+branch+":"+ref)
	return err
}

// gitIgnored returns which of the worktree-relative paths git ignores.
// Exit 1 means "none of them"; a directory matches dir-only patterns too.
func gitIgnored(ctx context.Context, dir string, rels []string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	in := []byte(strings.Join(rels, "\x00") + "\x00")
	out, code, err := runGitInput(ctx, dir, true, in, nil, "check-ignore", "--stdin", "-z")
	if code == 1 {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[p] = true
		}
	}
	return ignored, nil
}
