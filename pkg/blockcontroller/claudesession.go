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

	// The transcript can appear a while after launch — claude may not write it until the
	// first message, and the user might sit at the prompt first. Poll quickly at first,
	// then slowly, rather than assuming the session shows up immediately. Cheap either
	// way: this is one directory read per tick, once per claude invocation.
	claudeSessionPollFast     = 400 * time.Millisecond
	claudeSessionPollSlow     = 3 * time.Second
	claudeSessionFastDuration = 30 * time.Second
	claudeSessionPollTimeout  = 10 * time.Minute
)

// A session id is a UUID, which is also what the transcript file is named.
var claudeSessionIdRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Matches an explicit `--resume <id>` / `-r <id>` on the command line, which lets us
// skip the correlation entirely.
var claudeResumeArgRegex = regexp.MustCompile(`(?:^|\s)(?:-r|--resume)[\s=]+([0-9a-fA-F-]{36})(?:\s|$)`)

// claudeDbg logs under the same term:activitydebug setting as the activity tracker, since
// this runs off the same shell-integration signal and is diagnosed together with it.
func claudeDbg(blockId string, format string, args ...any) {
	if !activityDebugEnabled() {
		return
	}
	log.Printf("[claudesession] blk=%s "+format+"\n", append([]any{blockId}, args...)...)
}

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

// findNewClaudeSession returns the session started since the snapshot.
//
// A transcript that did not exist before is a far stronger signal than one that merely
// grew: any other claude running in the same directory keeps appending to its own file,
// and treating that as a candidate made the common case ambiguous (two terminals on one
// repo, or a session left running elsewhere). So created-since beats modified-since, and
// modified-since is only consulted when nothing was created — which is what `--continue`
// looks like, since it reuses an existing transcript.
//
// Still returns "" when the winning category has more than one candidate: binding the
// wrong conversation is worse than offering no button.
func findNewClaudeSession(projectDir string, before claudeSessionSnapshot) string {
	cur := snapshotClaudeSessions(projectDir)
	var created, modified []string
	for id, modTime := range cur {
		prev, existed := before[id]
		if !existed {
			created = append(created, id)
			continue
		}
		if modTime.After(prev) {
			modified = append(modified, id)
		}
	}
	if len(created) == 1 {
		return created[0]
	}
	if len(created) > 1 {
		return ""
	}
	if len(modified) == 1 {
		return modified[0]
	}
	return ""
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
			claudeDbg(blockId, "no cmd:cwd on block, cannot locate transcripts")
			return
		}
		projectDir := claudeProjectDirForCwd(cwd)
		if projectDir == "" {
			claudeDbg(blockId, "no claude config dir")
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
		claudeDbg(blockId, "watching %s (%d existing transcripts)", projectDir, len(before))
		start := time.Now()
		deadline := start.Add(claudeSessionPollTimeout)
		for time.Now().Before(deadline) {
			interval := claudeSessionPollSlow
			if time.Since(start) < claudeSessionFastDuration {
				interval = claudeSessionPollFast
			}
			time.Sleep(interval)
			sessionId := findNewClaudeSession(projectDir, before)
			if sessionId == "" {
				continue
			}
			claudeDbg(blockId, "bound session %s after %v", sessionId, time.Since(start).Round(time.Millisecond))
			setClaudeSessionMeta(blockId, sessionId, cwd)
			return
		}
		claudeDbg(blockId, "gave up after %v, no session could be attributed", claudeSessionPollTimeout)
	}()
}
