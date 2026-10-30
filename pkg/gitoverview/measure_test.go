// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || linux

package gitoverview

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMeasureRealTree reports what the engine costs on a real directory tree.
// It only runs when WGO_MEASURE_ROOT is set (see README.md, Tests), never fetches
// and never syncs, so it changes nothing in the repos it reads.
func TestMeasureRealTree(t *testing.T) {
	root := os.Getenv("WGO_MEASURE_ROOT")
	if root == "" {
		t.Skip("set WGO_MEASURE_ROOT to measure a real tree")
	}
	idleFor := 30 * time.Second
	if s := os.Getenv("WGO_MEASURE_IDLE"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			t.Fatal(err)
		}
		idleFor = d
	}

	start := time.Now()
	locs, err := discover([]string{expandHome(root)}, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovery: %d repos in %s", len(locs), time.Since(start).Round(time.Millisecond))

	tm := defaultTimings
	tm.firstFetchDelay = 24 * time.Hour // never fetch while measuring
	var publishes int
	e := newEngine(func(Overview) { publishes++ }, tm)
	defer e.Close()

	before := usage(t)
	start = time.Now()
	e.Configure(Config{Roots: []string{root}, MaxDepth: 4, FetchInterval: 24 * time.Hour})
	eventually(t, 2*time.Minute, "every repo scanned", func() bool {
		for _, v := range e.Snapshot().Repos {
			if v.State == StateUnknown {
				return false
			}
		}
		return len(e.Snapshot().Repos) > 0
	})
	afterScan := usage(t)
	runsAfterScan := e.statusRuns()
	t.Logf("first full scan: %s wall, git CPU %s (user+sys of all git children)",
		time.Since(start).Round(time.Millisecond), (afterScan.children - before.children).Round(time.Millisecond))

	time.Sleep(idleFor)
	afterIdle := usage(t)
	t.Logf("idle %s: git CPU %s, own CPU %s, publishes %d, watcher %s",
		idleFor, (afterIdle.children - afterScan.children).Round(time.Millisecond),
		(afterIdle.self - afterScan.self).Round(time.Millisecond), publishes, e.Snapshot().Watching)

	var rerun []string
	for rel, n := range e.statusRuns() {
		if d := n - runsAfterScan[rel]; d > 0 {
			rerun = append(rerun, fmt.Sprintf("%s×%d", rel, d))
		}
	}
	sort.Strings(rerun)
	t.Logf("status re-runs while idle, caused by the watcher: %s", strings.Join(rerun, ", "))

	e.mu.Lock()
	t.Logf("watcher overflows %d, coalesced rescans %v", e.overflows, e.rescans)
	e.mu.Unlock()

	var attention []string
	for _, v := range e.Snapshot().Repos {
		if v.needsAttention() {
			attention = append(attention, v.Rel+"="+string(v.State))
		}
	}
	t.Logf("need attention (%d): %s", len(attention), strings.Join(attention, ", "))
}

type cpu struct{ self, children time.Duration }

func usage(t *testing.T) cpu {
	t.Helper()
	var self, kids syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &self); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &kids); err != nil {
		t.Fatal(err)
	}
	tv := func(r syscall.Rusage) time.Duration {
		return time.Duration(r.Utime.Nano() + r.Stime.Nano())
	}
	return cpu{self: tv(self), children: tv(kids)}
}
