// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

import (
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/baseds"
)

// captureEvents redirects published activity events into a slice for assertions.
func captureEvents(t *testing.T) *[]baseds.TermActivityData {
	t.Helper()
	var mu sync.Mutex
	var events []baseds.TermActivityData
	orig := publishActivity
	publishActivity = func(evs []baseds.TermActivityData) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evs...)
	}
	t.Cleanup(func() { publishActivity = orig })
	return &events
}

func cmdStartSeq(cmd string) []byte {
	cmd64 := base64.StdEncoding.EncodeToString([]byte(cmd))
	return []byte("\x1b]16162;C;{\"cmd64\":\"" + cmd64 + "\"}\x07")
}

func hasState(events []baseds.TermActivityData, state string) bool {
	for _, e := range events {
		if e.State == state {
			return true
		}
	}
	return false
}

func trackerSnapshot(blockId string) (running bool, command string, agentKind string) {
	tr := getActivityTracker(blockId)
	tr.lock.Lock()
	defer tr.lock.Unlock()
	return tr.running, tr.command, tr.agentKind
}

func TestTermActivity_CommandStartParsesAcrossChunkBoundary(t *testing.T) {
	captureEvents(t)
	blockId := "test-split"
	ResetTermActivity(blockId)
	seq := cmdStartSeq("claude")
	mid := len(seq) / 2
	FeedTermActivity(blockId, seq[:mid])
	FeedTermActivity(blockId, seq[mid:])

	running, command, agentKind := trackerSnapshot(blockId)
	if !running {
		t.Fatalf("expected running after command start")
	}
	if command != "claude" {
		t.Fatalf("command = %q, want %q", command, "claude")
	}
	if agentKind != "claude" {
		t.Fatalf("agentKind = %q, want %q", agentKind, "claude")
	}
}

func TestTermActivity_DoneEmitsExitCode(t *testing.T) {
	events := captureEvents(t)
	blockId := "test-done"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("ls"))
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":3}\x07"))

	var done *baseds.TermActivityData
	for i := range *events {
		if (*events)[i].State == termActivityDone {
			done = &(*events)[i]
		}
	}
	if done == nil {
		t.Fatalf("no done event emitted; got %+v", *events)
	}
	if done.ExitCode == nil || *done.ExitCode != 3 {
		t.Fatalf("done exitcode = %v, want 3", done.ExitCode)
	}
	if running, _, _ := trackerSnapshot(blockId); running {
		t.Fatalf("expected not running after D")
	}
}

func TestTermActivity_PromptFinalizesWhenNoDone(t *testing.T) {
	captureEvents(t)
	blockId := "test-prompt"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("ls"))
	FeedTermActivity(blockId, []byte("\x1b]16162;A\x07")) // next prompt, no preceding D
	if running, _, _ := trackerSnapshot(blockId); running {
		t.Fatalf("expected A to finalize a still-running command")
	}
}

func TestTermActivity_ResetCancels(t *testing.T) {
	captureEvents(t)
	blockId := "test-reset"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("claude"))
	FeedTermActivity(blockId, []byte("\x1b]16162;R\x07"))
	if running, _, agentKind := trackerSnapshot(blockId); running || agentKind != "" {
		t.Fatalf("expected R to clear running/agent, got running=%v agent=%q", running, agentKind)
	}
}

func TestTermActivity_BellWaitsOnlyForAgents(t *testing.T) {
	// Agent running: a bell flips to "waiting".
	eventsAgent := captureEvents(t)
	agentBlock := "test-bell-agent"
	ResetTermActivity(agentBlock)
	FeedTermActivity(agentBlock, cmdStartSeq("claude"))
	FeedTermActivity(agentBlock, []byte("\x07"))
	if !hasState(*eventsAgent, termActivityWaiting) {
		t.Fatalf("expected waiting after bell while an agent is running")
	}

	// Non-agent command: a bell is ignored.
	eventsPlain := captureEvents(t)
	plainBlock := "test-bell-plain"
	ResetTermActivity(plainBlock)
	FeedTermActivity(plainBlock, cmdStartSeq("ls"))
	FeedTermActivity(plainBlock, []byte("\x07"))
	if hasState(*eventsPlain, termActivityWaiting) {
		t.Fatalf("a bell during a non-agent command should not produce waiting")
	}
}

func TestTermActivity_OscTerminatorNotTreatedAsBell(t *testing.T) {
	events := captureEvents(t)
	blockId := "test-osc-term"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("claude"))
	startLen := len(*events)
	// An OSC 7 cwd report ends in BEL; that terminator must NOT register as a bell.
	FeedTermActivity(blockId, []byte("\x1b]7;file://localhost/tmp\x07"))
	for _, e := range (*events)[startLen:] {
		if e.State == termActivityWaiting {
			t.Fatalf("an OSC terminator BEL was misread as a terminal bell")
		}
	}
}

func TestTermActivity_WorkingThenDoneOnSustainedOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based; skipped in -short")
	}
	events := captureEvents(t)
	blockId := "test-working"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("claude"))
	// stream output continuously past the initial delay + sustain window
	for i := 0; i < 24; i++ {
		FeedTermActivity(blockId, []byte("streaming output line for the activity heuristic\r\n"))
		time.Sleep(100 * time.Millisecond)
	}
	if !hasState(*events, termActivityWorking) {
		t.Fatalf("expected 'working' after sustained output; got %+v", *events)
	}
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":0}\x07"))
	var done *baseds.TermActivityData
	for i := range *events {
		if (*events)[i].State == termActivityDone {
			done = &(*events)[i]
		}
	}
	if done == nil || !done.Visible {
		t.Fatalf("expected a visible 'done' after a command that showed the spinner; got %+v", done)
	}
}

func TestAgentKindForCommand(t *testing.T) {
	cases := map[string]string{
		"claude":                                "claude",
		"claude --dangerously-skip-permissions": "claude",
		`ANTHROPIC_API_KEY="x" claude`:          "claude",
		"env FOO=bar claude --print":            "claude",
		"gemini":                                "gemini",
		"codex --yolo":                          "codex",
		"claudes":                               "",
		"echo claude":                           "",
		"ls -la":                                "",
		"":                                      "",
	}
	for in, want := range cases {
		if got := agentKindForCommand(in); got != want {
			t.Errorf("agentKindForCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
