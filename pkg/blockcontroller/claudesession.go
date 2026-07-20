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

	// Claude writes its transcript on the first user message, not at launch, so a session
	// left sitting at the prompt has nothing to attribute yet. The watch therefore runs
	// for as long as claude is running in the block (see claudeStillRunning); this cap is
	// only a backstop for a tracker that never reports an exit. Cheap either way: one
	// directory read per tick, once per claude invocation.
	claudeSessionPollFast     = 400 * time.Millisecond
	claudeSessionPollSlow     = 3 * time.Second
	claudeSessionFastDuration = 30 * time.Second
	claudeSessionPollTimeout  = 12 * time.Hour
)

// A session id is a UUID, which is also what the transcript file is named.
var claudeSessionIdRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Matches an explicit `--resume <id>` / `-r <id>` on the command line, which lets us
// skip the correlation entirely.
var claudeResumeArgRegex = regexp.MustCompile(`(?:^|\s)(?:-r|--resume)[\s=]+([0-9a-fA-F-]{36})(?:\s|$)`)

// claudeStillRunning reports whether the block's tracked command is still the claude that
// started this watch. Claude only writes its transcript once the user sends the first
// message, which can be long after launch — so the watch is bounded by the agent's
// lifetime rather than by a fixed timeout that would expire while it sits at the prompt.
func claudeStillRunning(blockId string) bool {
	activityTrackersLock.Lock()
	t := activityTrackers[blockId]
	activityTrackersLock.Unlock()
	if t == nil {
		return false
	}
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.running && t.agentKind == "claude"
}

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
//
// The path is resolved first, because claude keys by the real path: a session run in
// /tmp/x lands under -private-tmp-x on macOS. Watching the unresolved path would silently
// watch a directory that never changes.
func claudeProjectDirForCwd(cwd string) string {
	cfgDir := claudeConfigDir()
	if cfgDir == "" || cwd == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err == nil {
		cwd = resolved
	}
	encoded := strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
	return filepath.Join(cfgDir, claudeProjectsSubdir, encoded)
}

// Size matters as much as mtime: claude touches an unrelated transcript when it starts
// (session listing), which looks identical to activity if you only watch mtime. Growth is
// what actually means "a conversation is being written here".
type claudeSessionStat struct {
	modTime time.Time
	size    int64
}

type claudeSessionSnapshot map[string]claudeSessionStat

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
		rtn[id] = claudeSessionStat{modTime: info.ModTime(), size: info.Size()}
	}
	return rtn
}

// findNewClaudeSession returns the session started since the snapshot.
//
// A transcript that did not exist before is a far stronger signal than one that merely
// grew: any other claude running in the same directory keeps appending to its own file,
// and treating that as a candidate made the common case ambiguous (two terminals on one
// repo, or a session left running elsewhere). So created-since beats grown-since, and
// growth is only consulted when nothing was created — which is what `--continue` looks
// like, since it reuses an existing transcript.
//
// "Grown" means the file got bigger, not merely newer. Launching claude bumps the mtime
// of an existing transcript without writing to it, which is indistinguishable from real
// activity if you only compare timestamps — that misbound a stale session in testing.
//
// Still returns "" when the winning category has more than one candidate: binding the
// wrong conversation is worse than offering no button.
func findNewClaudeSession(projectDir string, before claudeSessionSnapshot) string {
	cur := snapshotClaudeSessions(projectDir)
	var created, grown []string
	for id, stat := range cur {
		prev, existed := before[id]
		if !existed {
			created = append(created, id)
			continue
		}
		// Deliberately not `modTime.After(prev.modTime)`: starting claude bumps the
		// mtime of an unrelated transcript without adding to it, and that misattributed
		// a stale session to the block. Only growth counts.
		if stat.size > prev.size {
			grown = append(grown, id)
		}
	}
	if len(created) == 1 {
		return created[0]
	}
	if len(created) > 1 {
		return ""
	}
	if len(grown) == 1 {
		return grown[0]
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
			if !claudeStillRunning(blockId) {
				claudeDbg(blockId, "claude exited after %v without a transcript to attribute", time.Since(start).Round(time.Second))
				return
			}
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
