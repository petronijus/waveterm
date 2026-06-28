// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshremote

// Per-project resource attribution for the sysmonitor. Phase 1: host processes.
//
// The system sysinfo series ("cpu", "mem:used") cover the whole machine. To show how much
// of that is "the project you're building", we walk the process table once per tick and sum
// the CPU% and resident memory of every process whose working directory is inside the project
// path — or whose pid was explicitly handed in (e.g. the process trees of the project's
// terminals, which catches build tools that chdir away from the project root).
//
// CPU% is normalized to a share of TOTAL system capacity (delta-cpu-seconds / wall-seconds /
// numCPU * 100) so it is directly comparable to, and stackable under, the system "cpu" series.
//
// Known phase-1 blind spots (handled in later phases): Docker/podman containers (run under the
// daemon, not under the project cwd), chroot/abuild sandboxes (cwd is inside the sandbox), and
// remote/VM builds. See the manual-tracker + container phases.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

// ProjUsage is one tick of attributed host usage for a tracked project.
type ProjUsage struct {
	HostCpuPct float64 // share of total system CPU capacity (0..100) used by the project's host procs
	HostMemGB  float64 // resident memory (GB) of those processes
	ProcCount  int     // how many processes were attributed (diagnostic)
}

// projSampler keeps the per-pid CPU bookkeeping needed to turn cumulative process CPU time into
// a per-tick percentage. gopsutil Process objects are recreated each tick and carry no state,
// so we hold the previous totals here. One sampler per tracked stream.
type projSampler struct {
	lock     sync.Mutex
	numCPU   int
	lastCpu  map[int32]float64 // pid -> cumulative cpu seconds at the previous sample
	lastWall time.Time
}

func makeProjSampler() *projSampler {
	n, err := cpu.Counts(true)
	if err != nil || n <= 0 {
		n = 1
	}
	return &projSampler{numCPU: n, lastCpu: make(map[int32]float64)}
}

// sample walks every process once and attributes those belonging to the project. A process
// belongs when its pid is in extraPids (the project's terminal process trees, resolved by the
// caller) OR its cwd is at/under projPath. projPath "" disables cwd matching; extraPids nil is
// fine. The returned usage is for this tick; CPU% needs a prior sample, so the very first call
// reports HostCpuPct 0 (mem and counts are valid immediately).
func (s *projSampler) sample(projPath string, extraPids map[int32]bool) ProjUsage {
	s.lock.Lock()
	defer s.lock.Unlock()

	now := time.Now()
	wallDelta := now.Sub(s.lastWall).Seconds()
	hadPrev := !s.lastWall.IsZero() && wallDelta > 0

	procs, err := process.Processes()
	if err != nil {
		return ProjUsage{}
	}
	projPath = strings.TrimRight(projPath, "/")

	newCpu := make(map[int32]float64, len(procs))
	var usage ProjUsage
	for _, p := range procs {
		times, err := p.Times()
		if err != nil {
			continue
		}
		cpuTotal := times.User + times.System
		newCpu[p.Pid] = cpuTotal

		inProj := extraPids != nil && extraPids[p.Pid]
		if !inProj && projPath != "" {
			cwd, err := p.Cwd()
			if err == nil && (cwd == projPath || strings.HasPrefix(cwd, projPath+"/")) {
				inProj = true
			}
		}
		if !inProj {
			continue
		}
		usage.ProcCount++
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			usage.HostMemGB += float64(mi.RSS) / BYTES_PER_GB
		}
		if hadPrev {
			if prev, ok := s.lastCpu[p.Pid]; ok {
				if d := cpuTotal - prev; d > 0 {
					usage.HostCpuPct += (d / wallDelta / float64(s.numCPU)) * 100
				}
			}
		}
	}

	s.lastCpu = newCpu
	s.lastWall = now
	return usage
}

// localProjSampler / localDockerSampler are the samplers used by the local sysinfo loop (one
// tracked project app-wide for now; per-block tracking lands in a later phase).
var localProjSampler = makeProjSampler()
var localDockerSampler = makeDockerSampler()

// DockerProjectFromPath derives the default docker-compose project name for a tracked path: the
// basename lowercased, which is compose's default when no explicit project name is configured.
func DockerProjectFromPath(p string) string {
	p = strings.TrimRight(expandHome(strings.TrimSpace(p)), "/")
	if p == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(p))
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// AddProjData attributes host process resource use under projPath into the sysinfo values map
// as the cpu:proj:host / mem:proj:host series, so the frontend can stack the project's share
// under the system totals. No-op (and no process walk) when projPath is blank.
func AddProjData(values map[string]float64, projPath string, dockerProject string) {
	if strings.TrimSpace(projPath) != "" {
		u := localProjSampler.sample(expandHome(projPath), nil)
		values["cpu:proj:host"] = u.HostCpuPct
		values["mem:proj:host"] = u.HostMemGB
	}
	if strings.TrimSpace(dockerProject) != "" {
		cpu, mem, _, reachable := localDockerSampler.sampleDocker(dockerProject)
		if reachable {
			values["cpu:proj:docker"] = cpu
			values["mem:proj:docker"] = mem
		}
	}
}
