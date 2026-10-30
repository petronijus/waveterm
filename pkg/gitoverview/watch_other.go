// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !linux

package gitoverview

// pollWatcher covers nothing: every repo is polled.
type pollWatcher struct{ all map[string]bool }

func newWatcher(_ []string, repos []repoLoc, _ watchSink) (watcher, error) {
	all := make(map[string]bool, len(repos))
	for _, r := range repos {
		all[r.Root] = true
	}
	return pollWatcher{all: all}, nil
}

func (p pollWatcher) Mode() string               { return "polling" }
func (p pollWatcher) Uncovered() map[string]bool { return p.all }
func (p pollWatcher) Close()                     {}
