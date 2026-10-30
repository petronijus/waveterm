// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"log"
	"sync"
	"time"

	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wps"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
)

// One engine per overview block: it starts with the block's first
// GitOverviewStartCommand and stops when the block closes, so nothing runs
// while no overview is open.
var (
	enginesLock sync.Mutex
	engines     = map[string]*Engine{}
	listenOnce  sync.Once
)

const (
	defaultMaxDepth     = 4
	defaultFetchMinutes = 15
)

// Start starts or reconfigures the engine of blockId and returns its state.
func Start(data wshrpc.CommandGitOverviewStartData) wshrpc.GitOverviewData {
	listenOnce.Do(listenBlockClose)
	blockId := data.BlockId
	enginesLock.Lock()
	e := engines[blockId]
	if e == nil {
		e = NewEngine(func(ov Overview) { publish(blockId, ov) })
		engines[blockId] = e
	}
	enginesLock.Unlock()
	e.Configure(configFrom(data))
	return toWire(e.Snapshot())
}

// Sync syncs the targets of blockId's engine, each only with the action the
// user saw.
func Sync(data wshrpc.CommandGitOverviewSyncData) {
	e := engineOf(data.BlockId)
	if e == nil {
		return
	}
	targets := make([]SyncTarget, 0, len(data.Targets))
	for _, t := range data.Targets {
		targets = append(targets, SyncTarget{Root: t.Path, Action: Action(t.Action)})
	}
	e.SyncAll(targets)
}

// Refresh rescans and fetches everything now.
func Refresh(blockId string) {
	if e := engineOf(blockId); e != nil {
		e.Refresh()
	}
}

func engineOf(blockId string) *Engine {
	enginesLock.Lock()
	defer enginesLock.Unlock()
	return engines[blockId]
}

func stop(blockId string) {
	enginesLock.Lock()
	e := engines[blockId]
	delete(engines, blockId)
	enginesLock.Unlock()
	if e != nil {
		e.Close()
	}
}

func listenBlockClose() {
	rpcClient := wshclient.GetBareRpcClient()
	rpcClient.EventListener.On(wps.Event_BlockClose, func(event *wps.WaveEvent) {
		if blockId, ok := event.Data.(string); ok {
			stop(blockId)
		}
	})
	if err := wshclient.EventSubCommand(rpcClient, wps.SubscriptionRequest{Event: wps.Event_BlockClose, AllScopes: true}, nil); err != nil {
		log.Printf("gitoverview: cannot subscribe to blockclose, engines stop only on restart: %v", err)
	}
}

func configFrom(data wshrpc.CommandGitOverviewStartData) Config {
	cfg := Config{
		Roots:         data.Roots,
		MaxDepth:      data.MaxDepth,
		Ignore:        ignoreMatcher(data.Ignore),
		FetchInterval: time.Duration(data.FetchMinutes) * time.Minute,
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = defaultMaxDepth
	}
	if data.FetchMinutes <= 0 {
		cfg.FetchInterval = defaultFetchMinutes * time.Minute
	}
	return cfg
}

func publish(blockId string, ov Overview) {
	wps.Broker.Publish(wps.WaveEvent{
		Event:  wps.Event_GitOverview,
		Scopes: []string{waveobj.MakeORef(waveobj.OType_Block, blockId).String()},
		Data:   toWire(ov),
	})
}

func unixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func toWire(ov Overview) wshrpc.GitOverviewData {
	repos := make([]wshrpc.GitOverviewRepo, 0, len(ov.Repos))
	for _, v := range ov.Repos {
		repos = append(repos, wshrpc.GitOverviewRepo{
			Path: v.Path, Rel: v.Rel, Branch: v.Branch, Upstream: v.Upstream,
			Ahead: v.Ahead, Behind: v.Behind, Changed: v.Changed,
			State: string(v.State), Action: string(v.Action), Reason: v.Reason,
			Busy: v.Busy, Err: v.Err, FetchErr: v.FetchErr, Activity: unixMs(v.Activity),
		})
	}
	return wshrpc.GitOverviewData{
		Repos: repos, Watching: ov.Watching, LastFetch: unixMs(ov.LastFetch), NextFetch: unixMs(ov.NextFetch),
		Fetching: ov.Fetching, ScanErr: ov.ScanErr,
	}
}
