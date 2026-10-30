// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import "testing"

// TestClassify pins the sync safety rules: an action exists only for a clean
// branch that is strictly behind (fast-forward) or strictly ahead (push).
func TestClassify(t *testing.T) {
	tracking := statusInfo{Branch: "main", Upstream: "origin/main", HasAB: true}
	with := func(f func(*statusInfo)) statusInfo { s := tracking; f(&s); return s }

	tests := []struct {
		name   string
		in     statusInfo
		state  State
		action Action
	}{
		{"in sync", tracking, StateInSync, ActionNone},
		{"behind", with(func(s *statusInfo) { s.Behind = 2 }), StateBehind, ActionPull},
		{"ahead", with(func(s *statusInfo) { s.Ahead = 1 }), StateAhead, ActionPush},
		{"diverged", with(func(s *statusInfo) { s.Ahead, s.Behind = 1, 1 }), StateDiverged, ActionNone},
		{"behind but dirty", with(func(s *statusInfo) { s.Behind, s.Changed = 1, 1 }), StateDirty, ActionNone},
		{"ahead but untracked file", with(func(s *statusInfo) { s.Ahead, s.Untracked = 1, 1 }), StateDirty, ActionNone},
		{"conflict", with(func(s *statusInfo) { s.Conflicts = 1 }), StateDirty, ActionNone},
		{"no upstream", statusInfo{Branch: "main"}, StateNoUpstream, ActionNone},
		{"upstream gone", statusInfo{Branch: "main", Upstream: "origin/main"}, StateUpstreamGone, ActionNone},
		{"detached", statusInfo{}, StateDetached, ActionNone},
		{"no commits", statusInfo{NoCommits: true, Branch: "main"}, StateNoCommits, ActionNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, action, reason := classify(tc.in)
			if state != tc.state || action != tc.action {
				t.Errorf("got %s/%q, want %s/%q", state, action, tc.state, tc.action)
			}
			if action == ActionNone && state != StateInSync && reason == "" {
				t.Error("a state without an action must say why")
			}
		})
	}
}

func TestEveryStateHasSeverity(t *testing.T) {
	for _, s := range []State{StateInSync, StateBehind, StateAhead, StateDiverged, StateDirty, StateNoUpstream,
		StateUpstreamGone, StateDetached, StateNoCommits, StateError, StateUnknown} {
		if _, ok := severity[s]; !ok {
			t.Errorf("%s has no severity", s)
		}
	}
}

func TestActionable(t *testing.T) {
	views := []RepoView{
		{Rel: "b", Path: "/b", Action: ActionPull},
		{Rel: "a", Path: "/a", Action: ActionPull},
		{Rel: "c", Path: "/c", Action: ActionPush},
		{Rel: "d", Action: ActionPush, Busy: "pushing"},
		{Rel: "e"},
	}
	targets, pulls, pushes := actionable(views)
	if len(pulls) != 2 || pulls[0] != "a" || len(pushes) != 1 || pushes[0] != "c" {
		t.Errorf("pulls %v pushes %v", pulls, pushes)
	}
	if len(targets) != 3 {
		t.Fatalf("targets %v", targets)
	}
	for _, tg := range targets {
		if tg.Action == ActionNone {
			t.Errorf("target without an action: %+v", tg)
		}
	}
}
