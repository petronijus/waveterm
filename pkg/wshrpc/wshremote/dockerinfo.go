// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshremote

// Per-project Docker attribution for the sysmonitor (phase 2). Host-process attribution
// (projinfo.go) misses Docker builds entirely — the container work runs under dockerd, not
// under the project's shell/cwd. Here we talk to the Docker Engine API directly over the unix
// socket (no docker CLI, no SDK dependency: just net/http + JSON), list the containers that
// belong to the tracked project (matched by the com.docker.compose.project label), read each
// container's stats, and sum CPU% (as a share of total host capacity, comparable to the system
// "cpu" series) + memory. Gracefully no-ops when there's no Docker / no permission.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// candidateSockets are the Docker-compatible engine sockets we probe: Docker, plus Podman's
// rootful and rootless sockets (Podman exposes the same /containers API), so a containerised
// build is caught regardless of which engine ran it.
func candidateSockets() []string {
	socks := []string{"/var/run/docker.sock", "/run/podman/podman.sock"}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		socks = append(socks, xdg+"/podman/podman.sock")
	}
	return socks
}

type dockerCpuSample struct {
	containerUsage float64 // cpu_usage.total_usage (ns), cumulative
	systemUsage    float64 // system_cpu_usage (ns), cumulative
}

type dockerSampler struct {
	lock    sync.Mutex
	clients []*http.Client             // one per candidate engine socket
	lastCpu map[string]dockerCpuSample // container id -> previous cumulative cpu reading
}

func makeSocketClient(socket string) *http.Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: tr, Timeout: 2 * time.Second}
}

func makeDockerSampler() *dockerSampler {
	var clients []*http.Client
	for _, sock := range candidateSockets() {
		clients = append(clients, makeSocketClient(sock))
	}
	return &dockerSampler{
		clients: clients,
		lastCpu: make(map[string]dockerCpuSample),
	}
}

func dockerGet(client *http.Client, path string, out any) error {
	req, err := http.NewRequest("GET", "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("docker api %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type dockerContainer struct {
	Id     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

// containerMatches decides whether a container belongs to the tracked project. The token (from
// sysinfo:dockerproject, or the path basename) matches when it equals the compose-project label
// (compose builds) OR is a substring of the image or container name (plain `docker run` builds,
// e.g. an image named "<project>-builder" — which the compose-label filter alone would miss).
func containerMatches(c dockerContainer, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	if strings.EqualFold(c.Labels["com.docker.compose.project"], token) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Image), token) {
		return true
	}
	for _, n := range c.Names {
		if strings.Contains(strings.ToLower(strings.TrimPrefix(n, "/")), token) {
			return true
		}
	}
	return false
}

type dockerStats struct {
	CpuStats struct {
		CpuUsage struct {
			TotalUsage float64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCpuUsage float64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage float64 `json:"usage"`
		Stats struct {
			Cache        float64 `json:"cache"`
			InactiveFile float64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

// listMatchingContainers returns the ids of running containers on one engine socket that belong
// to the tracked project (see containerMatches). All running containers are listed and matched
// client-side so plain `docker run` builds are caught, not just docker-compose projects.
func listMatchingContainers(client *http.Client, token string) ([]string, error) {
	var containers []dockerContainer
	if err := dockerGet(client, "/containers/json", &containers); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(containers))
	for _, c := range containers {
		if containerMatches(c, token) {
			ids = append(ids, c.Id)
		}
	}
	return ids, nil
}

// sampleDocker aggregates CPU%/mem for the tracked project's Docker containers. CPU% is the
// container's share of total host CPU capacity (delta container cpu-ns / delta system cpu-ns *
// 100), computed against the previous tick's cumulative reading. Returns zero usage (no error)
// when Docker is unavailable, so the caller just omits the docker series.
func (d *dockerSampler) sampleDocker(project string) (cpuPct float64, memGB float64, found int, reachable bool) {
	project = strings.TrimSpace(project)
	if project == "" {
		return 0, 0, 0, false
	}
	d.lock.Lock()
	defer d.lock.Unlock()

	live := make(map[string]bool)
	for _, client := range d.clients {
		ids, err := listMatchingContainers(client, project)
		if err != nil {
			continue // this engine socket isn't there / not reachable
		}
		reachable = true
		for _, id := range ids {
			live[id] = true
			var st dockerStats
			if err := dockerGet(client, "/containers/"+id+"/stats?stream=false", &st); err != nil {
				continue
			}
			found++
			// memory: usage minus reclaimable page cache (matches `docker stats`).
			mem := st.MemoryStats.Usage - st.MemoryStats.Stats.InactiveFile
			if mem < 0 {
				mem = st.MemoryStats.Usage
			}
			memGB += mem / BYTES_PER_GB
			cur := dockerCpuSample{
				containerUsage: st.CpuStats.CpuUsage.TotalUsage,
				systemUsage:    st.CpuStats.SystemCpuUsage,
			}
			if prev, ok := d.lastCpu[id]; ok {
				cDelta := cur.containerUsage - prev.containerUsage
				sDelta := cur.systemUsage - prev.systemUsage
				if cDelta > 0 && sDelta > 0 {
					cpuPct += (cDelta / sDelta) * 100
				}
			}
			d.lastCpu[id] = cur
		}
	}
	// drop bookkeeping for containers that went away.
	for id := range d.lastCpu {
		if !live[id] {
			delete(d.lastCpu, id)
		}
	}
	return cpuPct, memGB, found, reachable
}
