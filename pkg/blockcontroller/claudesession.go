// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wavetermdev/waveterm/pkg/panichandler"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wcore"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

const (
	claudeProjectsSubdir = "projects"
	claudeSessionExt     = ".jsonl"

	// Claude writes the transcript continuously, so a session that just started shows
	// up within a second or two. Poll briefly rather than watching: this runs once per
	// claude invocation, not per byte.
	claudeSessionPollInterval = 400 * time.Millisecond
	claudeSessionPollTimeout  = 20 * time.Second
)

// A session id is a UUID, which is also what the transcript file is named.
var claudeSessionIdRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Matches an explicit `--resume <id>` / `-r <id>` on the command line, which lets us
// skip the correlation entirely.
var claudeResumeArgRegex = regexp.MustCompile(`(?:^|\s)(?:-r|--resume)[\s=]+([0-9a-fA-F-]{36})(?:\s|$)`)

// claudeConfigDir mirrors Claude Code's own resolution order.
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeProjectDirForCwd maps a working directory to Claude's transcript directory.
// Claude replaces every path separator with a dash; the mapping is one-way (a directory
// whose own name contains a dash is indistinguishable from a separator), but we only
// ever need this direction.
func claudeProjectDirForCwd(cwd string) string {
	cfgDir := claudeConfigDir()
	if cfgDir == "" || cwd == "" {
		return ""
	}
	encoded := strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
	return filepath.Join(cfgDir, claudeProjectsSubdir, encoded)
}

type claudeSessionSnapshot map[string]time.Time

func snapshotClaudeSessions(projectDir string) claudeSessionSnapshot {
	rtn := make(claudeSessionSnapshot)
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return rtn
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), claudeSessionExt) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), claudeSessionExt)
		if !claudeSessionIdRegex.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		rtn[id] = info.ModTime()
	}
	return rtn
}

// findNewClaudeSession returns the session that appeared, or whose transcript advanced,
// since the snapshot. It returns "" when nothing changed or when more than one session
// changed — with several terminals running claude in the same directory there is no way
// to tell which is ours, and binding the wrong session is worse than binding none.
func findNewClaudeSession(projectDir string, before claudeSessionSnapshot) string {
	cur := snapshotClaudeSessions(projectDir)
	var found string
	for id, modTime := range cur {
		prev, existed := before[id]
		if existed && !modTime.After(prev) {
			continue
		}
		if found != "" {
			return ""
		}
		found = id
	}
	return found
}

// claudeCwdForBlock reads the cwd the shell last reported (OSC 7 → cmd:cwd), which is
// where claude will look for and create its transcript.
func claudeCwdForBlock(blockId string) string {
	ctx, cancelFn := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelFn()
	block, err := wstore.DBMustGet[*waveobj.Block](ctx, blockId)
	if err != nil {
		return ""
	}
	return block.Meta.GetString(waveobj.MetaKey_CmdCwd, "")
}

func setClaudeSessionMeta(blockId string, sessionId string, cwd string) {
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()
	oref := waveobj.MakeORef(waveobj.OType_Block, blockId)
	meta := waveobj.MetaMapType{
		waveobj.MetaKey_ClaudeSessionId: sessionId,
	}
	if cwd != "" {
		meta[waveobj.MetaKey_ClaudeCwd] = cwd
	}
	err := wstore.UpdateObjectMeta(ctx, oref, meta, false)
	if err != nil {
		log.Printf("[claudesession] blk=%s error writing meta: %v\n", blockId, err)
		return
	}
	wcore.SendWaveObjUpdate(oref)
}

// trackClaudeSession binds the claude session started by this block to the block, so the
// terminal can offer to resume it after a restart. Correlation is by transcript file
// rather than by process: the session id is not in claude's environment and it does not
// hold the transcript open, so there is nothing to read off the process itself.
// Called from the terminal-output path while the activity tracker's mutex is held, so it
// must not touch the database or the filesystem here — everything happens on the goroutine.
// Taking the "before" snapshot there rather than synchronously is safe: claude writes its
// transcript continuously, so a session whose file already existed when we looked still
// advances its mtime on a later poll and is picked up then.
func trackClaudeSession(blockId string, command string) {
	go func() {
		defer func() {
			panichandler.PanicHandler("blockcontroller:trackClaudeSession", recover())
		}()
		// A remote block's claude writes its transcript on the remote host, out of our reach.
		ctrl := getController(blockId)
		if ctrl == nil || ctrl.GetConnName() != "" {
			return
		}
		cwd := claudeCwdForBlock(blockId)
		if cwd == "" {
			return
		}
		projectDir := claudeProjectDirForCwd(cwd)
		if projectDir == "" {
			return
		}
		// An explicit --resume tells us the id outright; no need to guess.
		if m := claudeResumeArgRegex.FindStringSubmatch(command); m != nil {
			if claudeSessionIdRegex.MatchString(m[1]) {
				setClaudeSessionMeta(blockId, m[1], cwd)
				return
			}
		}
		before := snapshotClaudeSessions(projectDir)
		deadline := time.Now().Add(claudeSessionPollTimeout)
		for time.Now().Before(deadline) {
			time.Sleep(claudeSessionPollInterval)
			sessionId := findNewClaudeSession(projectDir, before)
			if sessionId == "" {
				continue
			}
			setClaudeSessionMeta(blockId, sessionId, cwd)
			return
		}
	}()
}
