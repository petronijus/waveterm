// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import "time"

// State is the one thing a row says about a repo; the most urgent one wins.
type State string

const (
	StateInSync       State = "in-sync"
	StateBehind       State = "behind"
	StateAhead        State = "ahead"
	StateDiverged     State = "diverged"
	StateDirty        State = "dirty"
	StateNoUpstream   State = "no-upstream"
	StateUpstreamGone State = "upstream-gone"
	StateDetached     State = "detached"
	StateNoCommits    State = "no-commits"
	StateError        State = "error"
	StateUnknown      State = "unknown" // not scanned yet
)

// Action is what the Sync button does for a repo. Sync never commits, stashes,
// rebases or merges anything that is not a fast-forward.
type Action string

const (
	ActionNone Action = ""
	ActionPull Action = "pull" // fast-forward to the upstream
	ActionPush Action = "push"
)

// severity orders the rows: lower first.
var severity = map[State]int{
	StateError:        0,
	StateDiverged:     1,
	StateBehind:       2,
	StateAhead:        3,
	StateDirty:        4,
	StateUpstreamGone: 5,
	StateNoUpstream:   6,
	StateDetached:     7,
	StateNoCommits:    8,
	StateUnknown:      9,
	StateInSync:       10,
}

// classify turns a status into the row's state, the sync action and, when
// there is no action, the reason a human has to step in.
func classify(s statusInfo) (State, Action, string) {
	switch {
	case s.NoCommits:
		return StateNoCommits, ActionNone, "no commits yet"
	case s.Branch == "":
		return StateDetached, ActionNone, "detached HEAD"
	case s.dirty():
		return StateDirty, ActionNone, "uncommitted changes — commit or stash by hand"
	case s.Upstream == "":
		return StateNoUpstream, ActionNone, "branch tracks no upstream"
	case !s.HasAB:
		return StateUpstreamGone, ActionNone, "upstream " + s.Upstream + " no longer exists"
	case s.Ahead > 0 && s.Behind > 0:
		return StateDiverged, ActionNone, "diverged — rebase or merge by hand"
	case s.Behind > 0:
		return StateBehind, ActionPull, ""
	case s.Ahead > 0:
		return StateAhead, ActionPush, ""
	default:
		return StateInSync, ActionNone, ""
	}
}

// RepoView is one row as the UI sees it.
type RepoView struct {
	Path     string    `json:"path"` // absolute worktree root, the row's identity
	Rel      string    `json:"rel"`  // relative to its scan root, for display
	Branch   string    `json:"branch"`
	Upstream string    `json:"upstream"`
	Ahead    int       `json:"ahead"`
	Behind   int       `json:"behind"`
	Changed  int       `json:"changed"` // changed + untracked + conflicted entries
	State    State     `json:"state"`
	Action   Action    `json:"action"`
	Reason   string    `json:"reason"`
	Busy     string    `json:"busy"`     // "fetching", "pulling", "pushing" or ""
	Err      string    `json:"err"`      // last sync failure; cleared when the repo changes
	FetchErr string    `json:"fetcherr"` // last background fetch failure (offline, auth)
	Activity time.Time `json:"activity"` // last reflog write: commit, checkout, pull
}

func (v RepoView) needsAttention() bool {
	return v.State != StateInSync || v.Err != ""
}

// Overview is everything the panel renders.
type Overview struct {
	Repos     []RepoView `json:"repos"`
	Watching  string     `json:"watching"` // how changes are detected
	LastFetch time.Time  `json:"lastfetch"`
	NextFetch time.Time  `json:"nextfetch"`
	Fetching  bool       `json:"fetching"`
	ScanErr   string     `json:"scanerr"`
}
