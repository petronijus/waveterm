// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// SyncOnce runs one full sync round against the shared WebDAV folder:
//
//  1. export the local state and stamp it vs the last-converged snapshot,
//  2. PUT our own state file (single writer — never conflicts),
//  3. GET every peer's state file and merge the union (last-write-wins),
//  4. reconcile the winners against our local view and apply the diff,
//  5. save the winners as the new snapshot baseline.
//
// Saving the *winners* (not our published view) as the snapshot is what keeps the
// system from ping-ponging: next round, items a peer owns compare equal to the
// snapshot and keep the peer's stamp, so we never re-claim a change we merely
// received.
func SyncOnce(ctx context.Context, store Transport, installId string) error {
	if installId == "" {
		return fmt.Errorf("wsync: empty installId")
	}
	if err := store.EnsureFolder(ctx); err != nil {
		return fmt.Errorf("ensuring sync folder: %w", err)
	}

	current, err := ExportLocalItems(ctx)
	if err != nil {
		return fmt.Errorf("exporting local state: %w", err)
	}
	prev, err := LoadSnapshot()
	if err != nil {
		return fmt.Errorf("loading snapshot: %w", err)
	}
	nowMs := time.Now().UnixMilli()
	ourItems := StampItems(current, prev, installId, nowMs)

	ourState := InstallState{InstallId: installId, PushedTs: nowMs, Items: ourItems}
	ourBytes, err := json.Marshal(ourState)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, StateFileName(installId), ourBytes); err != nil {
		return fmt.Errorf("publishing our state: %w", err)
	}

	states := []InstallState{ourState}
	names, err := store.ListStateFiles(ctx)
	if err != nil {
		return fmt.Errorf("listing peer states: %w", err)
	}
	for _, name := range names {
		if name == StateFileName(installId) {
			continue
		}
		data, ok, err := store.Get(ctx, name)
		if err != nil {
			return fmt.Errorf("fetching %s: %w", name, err)
		}
		if !ok {
			continue
		}
		var st InstallState
		if err := json.Unmarshal(data, &st); err != nil {
			log.Printf("wsync: skipping corrupt peer state %s: %v\n", name, err)
			continue
		}
		states = append(states, st)
	}

	winners := MergeStates(states)
	actions := Reconcile(winners, ourItems)
	if err := ApplyActions(ctx, actions); err != nil {
		return fmt.Errorf("applying remote changes: %w", err)
	}
	if err := SaveSnapshot(winners); err != nil {
		return fmt.Errorf("saving snapshot: %w", err)
	}
	log.Printf("wsync: synced — %d local items, %d peers, %d changes applied\n", len(ourItems), len(states)-1, len(actions))
	return nil
}
