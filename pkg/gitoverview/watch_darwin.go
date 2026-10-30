// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/fsnotify/fsevents"
)

// fseventsLatency lets the kernel coalesce bursts (a checkout, a build) into
// one delivery.
const fseventsLatency = 1 * time.Second

// fsWatcher is one FSEvents stream over the scan roots, plus any git dir that
// lives outside them. FSEvents is recursive and costs nothing while idle, so
// every repo is covered and nothing is polled.
type fsWatcher struct {
	stream *fsevents.EventStream
	done   chan struct{}
}

func newWatcher(roots []string, repos []repoLoc, sink watchSink) (watcher, error) {
	paths := append([]string(nil), roots...)
	for _, r := range repos {
		for _, d := range []string{r.GitDir, r.CommonDir} {
			if !underAny(d, paths) {
				paths = append(paths, d)
			}
		}
	}
	sort.Strings(paths)
	es := &fsevents.EventStream{
		Paths:   paths,
		Latency: fseventsLatency,
		Flags:   fsevents.WatchRoot,
	}
	if err := es.Start(); err != nil {
		return nil, err
	}
	w := &fsWatcher{stream: es, done: make(chan struct{})}
	go w.loop(sink)
	return w, nil
}

func (w *fsWatcher) loop(sink watchSink) {
	for {
		select {
		case <-w.done:
			return
		case batch, ok := <-w.stream.Events:
			if !ok {
				return
			}
			dirs := make([]string, 0, len(batch))
			for _, ev := range batch {
				if ev.Flags&(fsevents.KernelDropped|fsevents.UserDropped|fsevents.RootChanged) != 0 {
					sink.overflow()
					continue
				}
				if ev.Flags&fsevents.MustScanSubDirs != 0 {
					sink.rescan(ev.Path)
					continue
				}
				p := ev.Path
				if ev.Flags&fsevents.ItemIsFile != 0 {
					p = filepath.Dir(p)
				}
				dirs = append(dirs, p)
			}
			if len(dirs) > 0 {
				sink.changed(dirs)
			}
		}
	}
}

func (w *fsWatcher) Mode() string               { return "FSEvents" }
func (w *fsWatcher) Uncovered() map[string]bool { return nil }

// Close stops the stream first and only then the loop: the library's
// callback blocks on the unbuffered Events channel, so the loop must keep
// draining until no callback can run any more.
func (w *fsWatcher) Close() {
	w.stream.Stop()
	close(w.done)
}
