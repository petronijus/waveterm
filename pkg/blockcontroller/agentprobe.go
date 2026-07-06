// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/wavetermdev/waveterm/pkg/shellexec"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wstore"
)

// Agent identification from the block's live process tree (the technique agent
// multiplexers like herdr use). The shell-integration OSC 16162 "C" marker tells the
// activity tracker an agent command is running, but it's lost when the marker never
// fires (broken preexec) or when a durable session outlives the wavesrv that saw it.
// The pty's process tree is ground truth either way: walk the local shell's
// descendants and match known agent binaries. Only local blocks can be probed — a
// remote block's processes live on the remote host.

const (
	agentProbeCacheTTL    = 10 * time.Second
	agentProbeMaxProcs    = 64 // defensive cap on tree traversal
	agentProbeMaxDepth    = 6
	agentProbeCallTimeout = 500 * time.Millisecond
)

type agentProbeCacheEntry struct {
	kind    string
	checkTs time.Time
}

var (
	agentProbeCacheLock sync.Mutex
	agentProbeCache     = map[string]agentProbeCacheEntry{}
)

// localShellPidForBlock returns the pid of the block's local shell process, or 0
// when there is none (remote connection, no controller, not started).
func localShellPidForBlock(blockId string) int {
	ctrl := getController(blockId)
	if ctrl == nil || ctrl.GetConnName() != "" {
		return 0
	}
	switch c := ctrl.(type) {
	case *ShellController:
		c.Lock.Lock()
		defer c.Lock.Unlock()
		if c.ShellProc == nil {
			return 0
		}
		cmdWrap, ok := c.ShellProc.Cmd.(shellexec.CmdWrap)
		if !ok || cmdWrap.Cmd == nil || cmdWrap.Cmd.Process == nil {
			return 0
		}
		return cmdWrap.Cmd.Process.Pid
	case *DurableShellController:
		c.Lock.Lock()
		jobId := c.JobId
		c.Lock.Unlock()
		if jobId == "" {
			return 0
		}
		ctx, cancel := context.WithTimeout(context.Background(), agentProbeCallTimeout)
		defer cancel()
		job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
		if err != nil || job == nil {
			return 0
		}
		return job.CmdPid
	}
	return 0
}

func agentKindForProcName(name string) string {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	for _, a := range agentCommandRegexes {
		if a.re.MatchString(name) {
			return a.kind
		}
	}
	return ""
}

func agentKindForProcess(p *process.Process) string {
	if name, err := p.Name(); err == nil {
		if kind := agentKindForProcName(name); kind != "" {
			return kind
		}
	}
	// node/bun-launched agents (e.g. gemini) report the runtime as the process name;
	// the script path in argv gives them away.
	cmdline, err := p.CmdlineSlice()
	if err != nil {
		return ""
	}
	for i, arg := range cmdline {
		if i > 2 {
			break
		}
		if kind := agentKindForProcName(filepath.Base(arg)); kind != "" {
			return kind
		}
	}
	return ""
}

// probeAgentKind is a package var so tests can stub process-tree probing.
var probeAgentKind = probeAgentKindCached

// probeAgentKindCached walks the block's local shell process tree (BFS, capped) and
// returns the kind of the first known AI agent found, or "". Results are cached
// briefly — agent TUIs can ring the bell in bursts.
func probeAgentKindCached(blockId string) string {
	agentProbeCacheLock.Lock()
	if entry, ok := agentProbeCache[blockId]; ok && time.Since(entry.checkTs) < agentProbeCacheTTL {
		agentProbeCacheLock.Unlock()
		return entry.kind
	}
	agentProbeCacheLock.Unlock()
	kind := probeAgentKindUncached(blockId)
	agentProbeCacheLock.Lock()
	agentProbeCache[blockId] = agentProbeCacheEntry{kind: kind, checkTs: time.Now()}
	agentProbeCacheLock.Unlock()
	return kind
}

func probeAgentKindUncached(blockId string) string {
	shellPid := localShellPidForBlock(blockId)
	if shellPid <= 0 {
		return ""
	}
	root, err := process.NewProcess(int32(shellPid))
	if err != nil {
		return ""
	}
	queue := []*process.Process{root}
	seen := 0
	for depth := 0; depth < agentProbeMaxDepth && len(queue) > 0; depth++ {
		var next []*process.Process
		for _, p := range queue {
			seen++
			if seen > agentProbeMaxProcs {
				return ""
			}
			// the shell itself is never the agent; skip matching the root
			if p.Pid != int32(shellPid) {
				if kind := agentKindForProcess(p); kind != "" {
					return kind
				}
			}
			children, err := p.Children()
			if err != nil {
				continue
			}
			next = append(next, children...)
		}
		queue = next
	}
	return ""
}

// resetAgentProbeCache drops a block's cached probe result (block destroy/restart).
func resetAgentProbeCache(blockId string) {
	agentProbeCacheLock.Lock()
	defer agentProbeCacheLock.Unlock()
	delete(agentProbeCache, blockId)
}
