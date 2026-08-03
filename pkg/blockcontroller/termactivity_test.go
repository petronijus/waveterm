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

func TestIsServerCommand(t *testing.T) {
	cases := map[string]bool{
		// direct dev servers
		"shopify theme dev":           true,
		"shopify app dev --tunnel":    true,
		"npm run dev":                 true,
		"yarn dev":                    true,
		"pnpm start":                  true,
		"bun run watch":               true,
		"vite":                        true,
		"vite --host":                 true,
		"next dev":                    true,
		"nodemon server.js":           true,
		"rails server":                true,
		"python3 -m http.server 8000": true,
		"php artisan serve":           true,
		"php -S localhost:8000":       true,
		"streamlit run app.py":        true,
		"wrangler dev":                true,
		"docker compose up":           true,
		"docker-compose up":           true,
		`PORT=3000 npm run dev`:       true,
		// through a package-runner / exec wrapper (normalizeCmd strips it)
		"npx vite":                    true,
		"bundle exec rails s":         true,
		"poetry run uvicorn main:app": true,
		"python manage.py runserver":  true,
		"./manage.py runserver":       true,
		// not servers
		"claude":              false,
		"npm run build":       false,
		"npm run test":        false,
		"docker compose down": false,
		"deno task build":     false,
		"parcel build":        false,
		"go build ./...":      false,
		"ls -la":              false,
		"git status":          false,
		"":                    false,
	}
	for in, want := range cases {
		if got := isServerCommand(in); got != want {
			t.Errorf("isServerCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

// captureBadges runs the real state→badge mapping (publishActivityBadge) while
// capturing the emitted badge events, so a test can assert the working spinner and
// the done check/xmark are published. It bypasses the wps broker.
func captureBadges(t *testing.T) *[]baseds.BadgeEvent {
	t.Helper()
	var mu sync.Mutex
	var badges []baseds.BadgeEvent
	origBadge := publishBadgeEvent
	origAct := publishActivity
	publishBadgeEvent = func(oref string, be baseds.BadgeEvent) {
		mu.Lock()
		defer mu.Unlock()
		badges = append(badges, be)
	}
	publishActivity = func(evs []baseds.TermActivityData) {
		for _, ev := range evs {
			publishActivityBadge(ev)
		}
	}
	t.Cleanup(func() { publishBadgeEvent = origBadge; publishActivity = origAct })
	return &badges
}

func lastSetIcon(badges []baseds.BadgeEvent) string {
	icon := ""
	for _, b := range badges {
		if b.Badge != nil {
			icon = b.Badge.Icon
		}
	}
	return icon
}

// TestTermActivity_BadgeSpinnerOnStartAndCheckOnDone verifies the user-visible
// behavior: a quick shell-integration command (C then D, no output) still shows a
// spinner the instant it starts and a check when it finishes.
func TestTermActivity_BadgeSpinnerOnStartAndCheckOnDone(t *testing.T) {
	badges := captureBadges(t)
	blockId := "test-badge"
	ResetTermActivity(blockId)

	FeedTermActivity(blockId, cmdStartSeq("ls"))
	if got := lastSetIcon(*badges); got != "spinner+spin" {
		t.Fatalf("after command start, last set badge icon = %q, want spinner+spin; events=%+v", got, *badges)
	}

	exit0 := 0
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":0}\x07"))
	if got := lastSetIcon(*badges); got != "circle-check" {
		t.Fatalf("after command done, last set badge icon = %q, want circle-check; events=%+v", got, *badges)
	}

	// a non-zero exit shows the error mark
	ResetTermActivity(blockId)
	*badges = nil
	FeedTermActivity(blockId, cmdStartSeq("false"))
	_ = exit0
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":1}\x07"))
	if got := lastSetIcon(*badges); got != "circle-xmark" {
		t.Fatalf("after failed command, last set badge icon = %q, want circle-xmark", got)
	}
}

// TestTermActivity_ServerCommandNoBadge verifies a long-running dev server (detected by
// command) shows no tab spinner — it would otherwise spin forever, since a server never
// emits a command-done marker to clear it.
func TestTermActivity_ServerCommandNoBadge(t *testing.T) {
	badges := captureBadges(t)
	blockId := "test-server-badge"
	ResetTermActivity(blockId)

	FeedTermActivity(blockId, cmdStartSeq("shopify theme dev"))
	if got := lastSetIcon(*badges); got != "" {
		t.Fatalf("server command should set no spinner badge; got icon %q; events=%+v", got, *badges)
	}
	if len(*badges) == 0 {
		t.Fatalf("expected a clear badge event on server command start")
	}
	if last := (*badges)[len(*badges)-1]; last.Badge != nil {
		t.Fatalf("server command last badge event should be a clear (Badge nil); got %+v", last)
	}

	// contrast: an ordinary command still spins
	ResetTermActivity(blockId)
	*badges = nil
	FeedTermActivity(blockId, cmdStartSeq("ls"))
	if got := lastSetIcon(*badges); got != "spinner+spin" {
		t.Fatalf("ordinary command should spin; got %q", got)
	}
}

// TestTermActivity_OutputDrivenSpinner verifies the spinner shows from raw output
// even when no shell-integration command-start (C) marker ever arrives — the case
// where bash preexec is broken in the user's shell, so only D/A markers fire.
func TestTermActivity_OutputDrivenSpinner(t *testing.T) {
	events := captureEvents(t)
	blockId := "test-outputdriven"
	ResetTermActivity(blockId)
	// No cmdStartSeq — just sustained raw output for longer than cmdActivitySustain.
	deadline := time.Now().Add(cmdActivitySustain + 400*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("build output line ...\n"))
		time.Sleep(40 * time.Millisecond)
	}
	if !hasState(*events, termActivityWorking) {
		t.Fatalf("expected working spinner from sustained output with no C marker; got %+v", *events)
	}
	running, _, _ := trackerSnapshot(blockId)
	if running {
		t.Fatalf("output-driven activity must not set running=true (no real command tracked)")
	}

	// after output stops, the idle timer must mark it done (✓) — that lull is the only
	// "done" signal we get for output-only activity (e.g. an agent finishing a turn).
	*events = nil
	time.Sleep(cmdActivityDoneIdle + 400*time.Millisecond)
	if !hasState(*events, termActivityDone) {
		t.Fatalf("expected done (✓) after output went idle; got %+v", *events)
	}
}

// TestTermActivity_AgentQuietResolvesDoneNotThinking verifies a tracked agent command
// whose output stops (turn over, but no bell/OSC 9 and no agentstate hook ever arrives)
// resolves to a visible "done" instead of parking in "thinking" — which maps to the
// spinner badge and would otherwise spin for the life of the agent process.
func TestTermActivity_AgentQuietResolvesDoneNotThinking(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based; skipped in -short")
	}
	events := captureEvents(t)
	blockId := "test-agent-quiet"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("claude"))
	// stream past the initial delay + sustain window so the spinner turns on
	deadline := time.Now().Add(cmdActivityDelay + cmdActivitySustain + 500*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("agent streaming a turn...\n"))
		time.Sleep(50 * time.Millisecond)
	}
	if !hasState(*events, termActivityWorking) {
		t.Fatalf("expected working during the agent turn; got %+v", *events)
	}

	// turn ends: full silence, no bell, no D marker (agent keeps running)
	*events = nil
	time.Sleep(cmdActivityDoneIdle + 500*time.Millisecond)
	if hasState(*events, termActivityThinking) {
		t.Fatalf("a quiet agent must not park in thinking (perpetual spinner); got %+v", *events)
	}
	var done *baseds.TermActivityData
	for i := range *events {
		if (*events)[i].State == termActivityDone {
			done = &(*events)[i]
		}
	}
	if done == nil || !done.Visible || done.AgentKind != "claude" {
		t.Fatalf("expected visible done with agentkind claude after agent went quiet; got %+v", *events)
	}
	if done.DurationMs <= 0 {
		t.Fatalf("expected the done event to carry the turn duration, got %d", done.DurationMs)
	}
	if running, _, agentKind := trackerSnapshot(blockId); !running || agentKind != "claude" {
		t.Fatalf("agent is still running — tracker must keep running/agentKind, got running=%v agent=%q", running, agentKind)
	}

	// the idle prompt's repaint dribble must not re-trip the spinner over the ✓
	*events = nil
	deadline = time.Now().Add(cmdActivitySustain + 800*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("\x1b[2K\x1b[1G idle prompt repaint...\n"))
		time.Sleep(50 * time.Millisecond)
	}
	if hasState(*events, termActivityWorking) {
		t.Fatalf("post-turn idle dribble re-tripped the spinner; got %+v", *events)
	}

	// the eventual real D marker (agent exit) still finalizes with the exit code
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":0}\x07"))
	if running, _, _ := trackerSnapshot(blockId); running {
		t.Fatalf("expected not running after the agent's real D marker")
	}
}

// TestTermActivity_NonAgentQuietStillThinking pins the contrast: an ordinary running
// command (a build, a long test run) that pauses its output keeps the spinner via
// "thinking" — only agents resolve silence to done.
func TestTermActivity_NonAgentQuietStillThinking(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based; skipped in -short")
	}
	events := captureEvents(t)
	blockId := "test-nonagent-quiet"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, cmdStartSeq("cargo build"))
	deadline := time.Now().Add(cmdActivityDelay + cmdActivitySustain + 500*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("compiling...\n"))
		time.Sleep(50 * time.Millisecond)
	}
	*events = nil
	time.Sleep(cmdActivityIdle + 500*time.Millisecond)
	if hasState(*events, termActivityDone) {
		t.Fatalf("a paused non-agent command must not emit done; got %+v", *events)
	}
	if !hasState(*events, termActivityThinking) {
		t.Fatalf("expected thinking for a paused non-agent command; got %+v", *events)
	}
}

// TestTermActivity_CheckOnDoneWithoutCommandStart verifies a command that produced an
// output-driven spinner still gets a ✓ when the precmd (D) marker fires, even though
// no command-start (C) marker ever did (broken bash preexec).
func TestTermActivity_CheckOnDoneWithoutCommandStart(t *testing.T) {
	badges := captureBadges(t)
	blockId := "test-check-noC"
	ResetTermActivity(blockId)
	// sustained output, no C marker -> spinner
	deadline := time.Now().Add(cmdActivitySustain + 300*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("output...\n"))
		time.Sleep(40 * time.Millisecond)
	}
	if got := lastSetIcon(*badges); got != "spinner+spin" {
		t.Fatalf("expected spinner from output; got %q", got)
	}
	// command ends: only a D marker fires (precmd), no preceding C
	FeedTermActivity(blockId, []byte("\x1b]16162;D;{\"exitcode\":0}\x07"))
	if got := lastSetIcon(*badges); got != "circle-check" {
		t.Fatalf("expected circle-check after D marker on an output-driven command; got %q; events=%+v", got, *badges)
	}
}

// stubAgentProbe replaces the process-tree probe for the test's duration.
func stubAgentProbe(t *testing.T, kind string) {
	t.Helper()
	orig := probeAgentKind
	probeAgentKind = func(blockId string) string { return kind }
	t.Cleanup(func() { probeAgentKind = orig })
}

// TestTermActivity_BellWithProbedAgent verifies a bell flips to "waiting" for a block
// with no tracked command when the process-tree probe identifies a known agent —
// the durable-session / broken-preexec case where the C marker was never seen.
func TestTermActivity_BellWithProbedAgent(t *testing.T) {
	stubAgentProbe(t, "claude")
	events := captureEvents(t)
	blockId := "test-bell-probed"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, []byte("\x07"))
	if !hasState(*events, termActivityWaiting) {
		t.Fatalf("expected waiting after bell with probed agent; got %+v", *events)
	}
	_, _, agentKind := trackerSnapshot(blockId)
	if agentKind != "claude" {
		t.Fatalf("expected probed agentKind to stick, got %q", agentKind)
	}
}

// TestTermActivity_BellWithoutAgentStillIgnored verifies a bare-shell bell (no tracked
// command, probe finds nothing) stays ignored.
func TestTermActivity_BellWithoutAgentStillIgnored(t *testing.T) {
	stubAgentProbe(t, "")
	events := captureEvents(t)
	blockId := "test-bell-noagent"
	ResetTermActivity(blockId)
	FeedTermActivity(blockId, []byte("\x07"))
	if hasState(*events, termActivityWaiting) {
		t.Fatalf("a bell with no tracked or probed agent should not produce waiting")
	}
}

func TestSetExternalAgentState(t *testing.T) {
	events := captureEvents(t)
	blockId := "test-external-state"
	ResetTermActivity(blockId)

	if err := SetExternalAgentState(blockId, termActivityWaiting, "claude"); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if !hasState(*events, termActivityWaiting) {
		t.Fatalf("expected waiting event; got %+v", *events)
	}

	*events = nil
	if err := SetExternalAgentState(blockId, termActivityDone, "claude"); err != nil {
		t.Fatalf("done: %v", err)
	}
	var doneEv *baseds.TermActivityData
	for i := range *events {
		if (*events)[i].State == termActivityDone {
			doneEv = &(*events)[i]
		}
	}
	if doneEv == nil {
		t.Fatalf("expected done event; got %+v", *events)
	}
	if !doneEv.Visible || doneEv.AgentKind != "claude" {
		t.Fatalf("expected visible done with agentkind claude; got %+v", *doneEv)
	}
	// post-turn TUI dribble must not re-trip the spinner: waiting stays set
	tr := getActivityTracker(blockId)
	tr.lock.Lock()
	waiting := tr.waiting
	tr.lock.Unlock()
	if !waiting {
		t.Fatalf("expected waiting volume-gate to stay set after external done")
	}

	if err := SetExternalAgentState(blockId, "bogus", ""); err == nil {
		t.Fatalf("expected error for invalid state")
	}
}

// TestTermActivity_WaitingStickyAgainstOutput verifies an idle agent TUI's repaint
// dribble (continuous small chunks, no gap) can NOT flip waiting back to working —
// only user input releases the state.
func TestTermActivity_WaitingStickyAgainstOutput(t *testing.T) {
	events := captureEvents(t)
	blockId := "test-waiting-sticky"
	ResetTermActivity(blockId)
	if err := SetExternalAgentState(blockId, termActivityWaiting, "claude"); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	*events = nil
	// simulate the idle dribble: ~40B every 100ms for well past the sustain window
	deadline := time.Now().Add(cmdActivitySustain + 800*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("\x1b[2K\x1b[1G idle prompt repaint chunk...\n"))
		time.Sleep(50 * time.Millisecond)
	}
	if hasState(*events, termActivityWorking) {
		t.Fatalf("idle dribble must not flip waiting back to working; got %+v", *events)
	}

	// terminal auto-replies and arrow keys must not release waiting either
	FeedTermUserInput(blockId, []byte("\x1b[15;42R"))
	FeedTermUserInput(blockId, []byte("\x1b[A\x1b[B"))
	FeedTermUserInput(blockId, []byte{0x03})
	tr := getActivityTracker(blockId)
	tr.lock.Lock()
	waiting := tr.waiting
	tr.lock.Unlock()
	if !waiting {
		t.Fatalf("escape replies / arrows / ctrl-c must not release waiting")
	}

	// a real answer (printable + Enter) releases it; output may then drive again
	FeedTermUserInput(blockId, []byte("y\r"))
	tr.lock.Lock()
	waiting = tr.waiting
	tr.lock.Unlock()
	if waiting {
		t.Fatalf("printable input should release waiting")
	}
	*events = nil
	deadline = time.Now().Add(cmdActivitySustain + 800*time.Millisecond)
	for time.Now().Before(deadline) {
		FeedTermActivity(blockId, []byte("agent output resumes, plenty of bytes now\n"))
		time.Sleep(50 * time.Millisecond)
	}
	if !hasState(*events, termActivityWorking) {
		t.Fatalf("after input released waiting, sustained output should flip to working")
	}
}

// TestTermActivity_InputScannerChunkBoundary verifies a CSI reply split across input
// chunks isn't misread as printable payload (its parameter bytes are digits).
func TestTermActivity_InputScannerChunkBoundary(t *testing.T) {
	captureEvents(t)
	blockId := "test-input-chunks"
	ResetTermActivity(blockId)
	if err := SetExternalAgentState(blockId, termActivityWaiting, "claude"); err != nil {
		t.Fatalf("waiting: %v", err)
	}
	FeedTermUserInput(blockId, []byte("\x1b[15;"))
	FeedTermUserInput(blockId, []byte("42R"))
	tr := getActivityTracker(blockId)
	tr.lock.Lock()
	waiting := tr.waiting
	tr.lock.Unlock()
	if !waiting {
		t.Fatalf("split CSI reply must not release waiting")
	}
}
