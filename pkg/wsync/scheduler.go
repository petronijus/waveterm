// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wavetermdev/waveterm/pkg/secretstore"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
)

const (
	// SecretName_WebDAVPassword is the secret-store key holding the Nextcloud
	// app-password — kept out of settings.json so it is never written in plaintext.
	SecretName_WebDAVPassword = "sync:webdavpassword"

	DefaultSyncFolder     = "waveterm-sync"
	DefaultSyncIntervalMs = 60000
	MinSyncIntervalMs     = 10000
	syncStartupDelay      = 5 * time.Second
	syncRoundTimeout      = 2 * time.Minute
)

// SyncStatus is a snapshot of the scheduler's last activity, for the UI.
type SyncStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	LastSyncTs int64  `json:"lastsyncts,omitempty"`
	LastError  string `json:"lasterror,omitempty"`
}

// Scheduler drives SyncOnce on a timer plus on-demand triggers. One per process.
type Scheduler struct {
	lock       sync.Mutex
	installId  string
	started    bool
	trigger    chan struct{}
	runLock    sync.Mutex // serializes sync rounds (timer vs manual)
	statusLock sync.Mutex
	status     SyncStatus
}

var globalScheduler = &Scheduler{trigger: make(chan struct{}, 1)}

func GetScheduler() *Scheduler {
	return globalScheduler
}

// Start launches the scheduler loop for this install. Idempotent. Pass the
// Client.InstallId — it identifies this machine's state file and breaks LWW ties.
func (s *Scheduler) Start(installId string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.started || installId == "" {
		return
	}
	s.started = true
	s.installId = installId
	go s.run()
}

// TriggerNow requests an immediate sync round (non-blocking; coalesces with any
// already-pending trigger).
func (s *Scheduler) TriggerNow() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

func (s *Scheduler) run() {
	timer := time.NewTimer(syncStartupDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-s.trigger:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), syncRoundTimeout)
		interval := s.doSync(ctx)
		cancel()
		timer.Reset(interval)
	}
}

// GetStatus returns the last recorded sync status.
func (s *Scheduler) GetStatus() SyncStatus {
	s.statusLock.Lock()
	defer s.statusLock.Unlock()
	return s.status
}

func (s *Scheduler) setStatus(st SyncStatus) {
	s.statusLock.Lock()
	defer s.statusLock.Unlock()
	s.status = st
}

// SyncNow runs one sync round on demand (serialized against the timer) and returns
// the resulting status — used by the "Sync now" command.
func (s *Scheduler) SyncNow(ctx context.Context) SyncStatus {
	s.doSync(ctx)
	return s.GetStatus()
}

// doSync loads config, runs a sync round if enabled+configured, records status, and
// returns the delay until the next tick (always returned so the loop keeps ticking
// even when sync is off — a later config change then takes effect). The runLock
// ensures a manual SyncNow and the timer never run a round concurrently.
func (s *Scheduler) doSync(ctx context.Context) time.Duration {
	s.runLock.Lock()
	defer s.runLock.Unlock()
	store, enabled, interval, err := loadSyncConfig()
	st := SyncStatus{Enabled: enabled, LastSyncTs: s.GetStatus().LastSyncTs}
	if err != nil {
		st.LastError = err.Error()
		s.setStatus(st)
		log.Printf("wsync: %v\n", err)
		return interval
	}
	if !enabled {
		s.setStatus(st)
		return interval
	}
	st.Configured = true
	if err := SyncOnce(ctx, store, s.installId); err != nil {
		st.LastError = err.Error()
		log.Printf("wsync: round failed: %v\n", err)
	} else {
		st.LastSyncTs = time.Now().UnixMilli()
	}
	s.setStatus(st)
	return interval
}

// loadSyncConfig picks and builds the storage transport from settings and reports
// whether sync is enabled and fully configured. A non-empty sync:folderpath selects
// the credential-free local-folder backend (a Nextcloud/Drive desktop-client folder);
// otherwise it falls back to the direct WebDAV backend, which needs url + user + the
// app-password from the secret store. The poll interval is returned even when
// disabled so the loop keeps a sane cadence.
func loadSyncConfig() (Transport, bool, time.Duration, error) {
	settings := wconfig.GetWatcher().GetFullConfig().Settings
	interval := time.Duration(DefaultSyncIntervalMs) * time.Millisecond
	if settings.SyncIntervalMs != nil && *settings.SyncIntervalMs >= MinSyncIntervalMs {
		interval = time.Duration(*settings.SyncIntervalMs) * time.Millisecond
	}
	if !settings.SyncEnabled {
		return nil, false, interval, nil
	}
	if strings.TrimSpace(settings.SyncFolderPath) != "" {
		return MakeLocalFolderTransport(settings.SyncFolderPath), true, interval, nil
	}
	folder := settings.SyncFolder
	if folder == "" {
		folder = DefaultSyncFolder
	}
	if settings.SyncWebDAVURL == "" || settings.SyncWebDAVUser == "" {
		return nil, false, interval, fmt.Errorf("sync enabled but neither sync:folderpath nor sync:webdavurl/sync:webdavuser is set")
	}
	pw, ok, err := secretstore.GetSecret(SecretName_WebDAVPassword)
	if err != nil {
		return nil, false, interval, fmt.Errorf("reading webdav password secret: %w", err)
	}
	if !ok || pw == "" {
		return nil, false, interval, fmt.Errorf("sync enabled but secret %q not set", SecretName_WebDAVPassword)
	}
	cfg := WebDAVConfig{
		BaseURL:  settings.SyncWebDAVURL,
		Folder:   folder,
		User:     settings.SyncWebDAVUser,
		Password: pw,
	}
	return MakeWebDAVClient(cfg), true, interval, nil
}
