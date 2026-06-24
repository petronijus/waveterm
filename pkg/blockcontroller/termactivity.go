// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

// Backend terminal command-activity detection.
//
// The "working" spinner / "done" checkmark tab indicators used to be computed in
// the frontend TermWrap, which only exists while a terminal's tab is the active
// tab. Switching to another tab unmounts that view, so a command running in a
// background tab produced no indicator. This moves detection to the backend, which
// already sees every byte of PTY output for every block regardless of which tab is
// mounted. We scan that stream for the shell-integration OSC 16162 command markers
// (C/D/A/R) plus live output volume, run the same "sustained output ⇒ working"
// heuristic the frontend used, and publish per-block Event_TermActivity transitions.
// The frontend turns those into focus-aware tab badges and OS notifications.
//
// This file is intentionally focus-agnostic: it reports what the terminal is doing,
// not whether the user is looking at it. "Don't badge the tab you're looking at" is
// a presentation decision and stays in the frontend.

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wps"
)

// Activity heuristic timings — must stay in sync with the documented behavior the
// frontend used (see the old osc-handlers.ts CmdActivity* constants).
const (
	cmdActivityDelay     = 1200 * time.Millisecond // ignore the first burst so quick commands don't flash
	cmdActivityIdle      = 2500 * time.Millisecond // output quiet this long ⇒ "done thinking" / idle
	cmdActivitySustain   = 700 * time.Millisecond  // output must flow this long continuously before we call it "working"
	cmdActivityGap       = 1000 * time.Millisecond // a quiet gap longer than this ends the continuous stretch
	cmdActivityWorkBytes = 512                     // after a "waiting" bell, a stretch must carry at least this many bytes to count as real work resuming
	maxOscBufLen         = 8192                    // cap a single OSC payload so malformed input can't grow unbounded
)

// Event_TermActivity state values.
const (
	termActivityWorking  = "working"
	termActivityThinking = "thinking"
	termActivityWaiting  = "waiting"
	termActivityDone     = "done"
	termActivityNone     = "none"
)

// scanner modes for the incremental control-sequence parser
const (
	scanNormal = iota
	scanEsc
	scanOsc
	scanOscEsc
	scanString // DCS/APC/PM/SOS string payload — skipped so an embedded BEL isn't read as a terminal bell
	scanStringEsc
)

const (
	byteEsc = 0x1b
	byteBel = 0x07
	byteST  = 0x5c // the '\' that, after ESC, forms the ST string terminator
)

var claudeCodeRegex = regexp.MustCompile(`^claude\b`)

// Interactive AI coding agents whose turn-done "your turn" signal (bell / OSC 9) we
// surface as a distinct "waiting for you" state. Mirrors AgentCommandRegexes in the
// frontend.
var agentCommandRegexes = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"claude", claudeCodeRegex},
	{"gemini", regexp.MustCompile(`^gemini\b`)},
	{"codex", regexp.MustCompile(`^codex\b`)},
}

var envCmdPrefixRegex = regexp.MustCompile(`^env\s+`)
var envVarPrefixRegex = regexp.MustCompile(`^(?:\w+=(?:"[^"]*"|'[^']*'|\S+)\s+)*`)

func normalizeCmd(cmd string) string {
	s := strings.TrimSpace(cmd)
	s = envCmdPrefixRegex.ReplaceAllString(s, "")
	s = envVarPrefixRegex.ReplaceAllString(s, "")
	return s
}

func agentKindForCommand(cmd string) string {
	if cmd == "" {
		return ""
	}
	normalized := normalizeCmd(cmd)
	for _, a := range agentCommandRegexes {
		if a.re.MatchString(normalized) {
			return a.kind
		}
	}
	return ""
}

type termActivityTracker struct {
	lock    sync.Mutex
	blockId string

	// incremental scanner state
	scanMode int
	oscBuf   []byte

	// command activity state machine
	running      bool
	outputDriven bool // spinner came from raw output, not a shell-integration command start
	startTs      time.Time
	visible      bool      // spinner currently "on"
	everShown    bool      // spinner shown at least once this command
	waiting      bool      // agent "your turn" state
	activeSince  time.Time // start of the current continuous-output stretch (zero ⇒ none)
	lastOutputTs time.Time
	stretchBytes int
	command      string
	agentKind    string

	idleTimer *time.Timer
	idleGen   int // guards stale idle-timer callbacks

	curState string // last published state

	outbox []baseds.TermActivityData
}

func makeTermActivityTracker(blockId string) *termActivityTracker {
	return &termActivityTracker{blockId: blockId, scanMode: scanNormal, curState: termActivityNone}
}

var (
	activityTrackersLock sync.Mutex
	activityTrackers     = make(map[string]*termActivityTracker)
)

func getActivityTracker(blockId string) *termActivityTracker {
	activityTrackersLock.Lock()
	defer activityTrackersLock.Unlock()
	t := activityTrackers[blockId]
	if t == nil {
		t = makeTermActivityTracker(blockId)
		activityTrackers[blockId] = t
	}
	return t
}

// FeedTermActivity scans a chunk of raw PTY output for a block and publishes any
// resulting activity-state transitions. Safe to call from the per-block read loop.
func FeedTermActivity(blockId string, data []byte) {
	if len(data) == 0 {
		return
	}
	getActivityTracker(blockId).processBytes(data)
}

// ResetTermActivity tears down a block's activity tracker (on block destroy,
// controller replacement, or shell restart), clearing any lingering indicator.
func ResetTermActivity(blockId string) {
	activityTrackersLock.Lock()
	t := activityTrackers[blockId]
	delete(activityTrackers, blockId)
	activityTrackersLock.Unlock()
	if t == nil {
		return
	}
	t.lock.Lock()
	t.stopIdleTimer()
	var out []baseds.TermActivityData
	if t.curState != termActivityNone {
		t.curState = termActivityNone
		out = []baseds.TermActivityData{{BlockId: t.blockId, State: termActivityNone}}
	}
	t.lock.Unlock()
	publishActivity(out)
}

func (t *termActivityTracker) processBytes(data []byte) {
	t.lock.Lock()
	for _, b := range data {
		t.scanByte(b)
	}
	t.markOutput(len(data))
	out := t.outbox
	t.outbox = nil
	t.lock.Unlock()
	publishActivity(out)
}

// scanByte advances the control-sequence parser one byte. Assumes t.lock held.
func (t *termActivityTracker) scanByte(b byte) {
	switch t.scanMode {
	case scanNormal:
		switch b {
		case byteEsc:
			t.scanMode = scanEsc
		case byteBel:
			t.handleBell()
		}
	case scanEsc:
		switch b {
		case ']':
			t.scanMode = scanOsc
			t.oscBuf = t.oscBuf[:0]
		case 'P', 'X', '^', '_': // DCS, SOS, PM, APC
			t.scanMode = scanString
		case byteEsc:
			// stay in scanEsc
		default:
			t.scanMode = scanNormal
		}
	case scanOsc:
		switch b {
		case byteBel:
			t.completeOsc()
		case byteEsc:
			t.scanMode = scanOscEsc
		default:
			if len(t.oscBuf) < maxOscBufLen {
				t.oscBuf = append(t.oscBuf, b)
			} else {
				t.scanMode = scanNormal // overlong/malformed — give up on this OSC
			}
		}
	case scanOscEsc:
		if b == byteST {
			t.completeOsc()
		} else {
			t.scanMode = scanNormal
			t.scanByte(b) // the ESC began a new sequence; reprocess this byte
		}
	case scanString:
		switch b {
		case byteEsc:
			t.scanMode = scanStringEsc
		case byteBel:
			t.scanMode = scanNormal // string terminated; do NOT treat as a bell
		}
	case scanStringEsc:
		if b == byteST {
			t.scanMode = scanNormal
		} else {
			t.scanMode = scanNormal
			t.scanByte(b)
		}
	}
}

// completeOsc parses a finished OSC payload (without the "ESC]" prefix or
// terminator) and dispatches the ones we care about. Assumes t.lock held.
func (t *termActivityTracker) completeOsc() {
	payload := string(t.oscBuf)
	t.oscBuf = t.oscBuf[:0]
	t.scanMode = scanNormal
	sep := strings.IndexByte(payload, ';')
	prefix := payload
	rest := ""
	if sep >= 0 {
		prefix = payload[:sep]
		rest = payload[sep+1:]
	}
	switch prefix {
	case "16162":
		t.handleOsc16162(rest)
	case "9":
		t.handleOsc9(rest)
	}
}

func (t *termActivityTracker) handleOsc16162(data string) {
	if data == "" {
		return
	}
	parts := strings.SplitN(data, ";", 2)
	command := parts[0]
	jsonStr := ""
	if len(parts) > 1 {
		jsonStr = parts[1]
	}
	switch command {
	case "C":
		var d struct {
			Cmd64 string `json:"cmd64"`
		}
		if jsonStr != "" {
			_ = json.Unmarshal([]byte(jsonStr), &d)
		}
		t.startCommand(d.Cmd64)
	case "D":
		var d struct {
			ExitCode *int `json:"exitcode"`
		}
		if jsonStr != "" {
			_ = json.Unmarshal([]byte(jsonStr), &d)
		}
		t.finishCommand(d.ExitCode)
	case "A":
		// next prompt drawn — finalize if "D" never fired (no-op if it already did)
		t.finishCommand(nil)
	case "R":
		t.cancelCommand()
	}
}

func (t *termActivityTracker) handleOsc9(data string) {
	if data == "4" || strings.HasPrefix(data, "4;") {
		return // ConEmu/Windows-Terminal progress protocol, not a notification
	}
	t.markWaiting()
}

// handleBell is a standalone terminal BEL (not an OSC terminator). Interactive AI
// agents ring it to signal "your turn".
func (t *termActivityTracker) handleBell() {
	t.markWaiting()
}

func decodeCmd64(cmd64 string) string {
	if cmd64 == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(cmd64)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func (t *termActivityTracker) startCommand(cmd64 string) {
	t.stopIdleTimer()
	t.running = true
	t.outputDriven = false
	t.startTs = time.Now()
	t.visible = false
	t.everShown = false
	t.waiting = false
	t.activeSince = time.Time{}
	t.lastOutputTs = time.Time{}
	t.stretchBytes = 0
	t.command = decodeCmd64(cmd64)
	t.agentKind = agentKindForCommand(t.command)
	// Show the working spinner the instant a command starts (shell-integration C
	// marker), not only once the output heuristic trips — so even quick/quiet commands
	// get an indicator. This also replaces any leftover done badge from the last command.
	t.setState(termActivityWorking)
}

func (t *termActivityTracker) finishCommand(exitCode *int) {
	// Finalize on a command-end (D) marker if we were tracking a command (C fired) OR
	// we showed an output-driven spinner — the latter covers shells where preexec/C is
	// broken but the precmd D marker still fires, so a command with output still gets a
	// ✓/✗. A bare prompt with no activity is ignored, and a second A after D no-ops.
	if !t.running && !t.everShown {
		return
	}
	t.running = false
	t.outputDriven = false
	t.stopIdleTimer()
	// End the current output stretch so the marker's own bytes (and the prompt redraw
	// that follows) don't immediately re-trip the spinner over the ✓ we're about to set.
	t.activeSince = time.Time{}
	t.stretchBytes = 0
	// A shell-integration-tracked command (C→D) always gets a done badge, even if it
	// was quick or silent — the user wants a ✓/✗ after every command, not only after
	// long ones.
	visible := true
	t.visible = false
	t.everShown = false
	t.waiting = false
	durMs := int64(0)
	if !t.startTs.IsZero() {
		durMs = time.Since(t.startTs).Milliseconds()
	}
	agentKind := t.agentKind
	t.agentKind = ""
	// Always emit "done" for a real (shell-integration-tracked) command end. Visible
	// tells the frontend whether to show a badge; DurationMs lets it duration-gate the
	// OS notification even for a long but silent command that never showed a spinner.
	t.curState = termActivityDone
	t.outbox = append(t.outbox, baseds.TermActivityData{
		BlockId:    t.blockId,
		State:      termActivityDone,
		Visible:    visible,
		ExitCode:   exitCode,
		AgentKind:  agentKind,
		Command:    t.command,
		DurationMs: durMs,
	})
}

func (t *termActivityTracker) cancelCommand() {
	t.stopIdleTimer()
	t.running = false
	t.outputDriven = false
	t.visible = false
	t.everShown = false
	t.waiting = false
	t.stretchBytes = 0
	t.agentKind = ""
	t.setState(termActivityNone)
}

// markOutput is called once per output chunk with the chunk length. It runs the
// "sustained output ⇒ working" heuristic. Assumes t.lock held.
func (t *termActivityTracker) markOutput(n int) {
	now := time.Now()
	// For a shell-integration-tracked command (C marker fired) skip the first burst so a
	// quick command doesn't flash. With no C marker (e.g. bash preexec is broken in the
	// user's shell) we drive the spinner purely off output.
	if t.running && now.Sub(t.startTs) < cmdActivityDelay {
		return
	}
	if t.activeSince.IsZero() || now.Sub(t.lastOutputTs) > cmdActivityGap {
		t.activeSince = now
		t.stretchBytes = 0
	}
	t.lastOutputTs = now
	t.stretchBytes += n
	// While in the bell-driven "waiting for you" state an agent TUI keeps repainting
	// its idle prompt — small bursts that must NOT look like work. Only a stretch
	// carrying real volume flips us back to the spinner.
	workVolumeOk := !t.waiting || t.stretchBytes >= cmdActivityWorkBytes
	if !t.visible && workVolumeOk && now.Sub(t.activeSince) >= cmdActivitySustain {
		t.waiting = false
		t.visible = true
		t.everShown = true
		if !t.running {
			t.outputDriven = true // no command boundary; the idle timer will end it
		}
		t.setState(termActivityWorking)
	}
	if t.visible {
		t.armIdleTimer()
	}
}

func (t *termActivityTracker) markWaiting() {
	if !t.running {
		return // no command active — a bare-shell bell, not an agent waiting
	}
	if t.agentKind == "" {
		return // scoped to AI agents' "your turn" signal, not arbitrary program bells
	}
	t.stopIdleTimer()
	t.waiting = true
	t.visible = false
	t.activeSince = time.Time{}
	t.stretchBytes = 0
	t.setState(termActivityWaiting)
}

func (t *termActivityTracker) armIdleTimer() {
	t.idleGen++
	gen := t.idleGen
	if t.idleTimer != nil {
		t.idleTimer.Stop()
	}
	t.idleTimer = time.AfterFunc(cmdActivityIdle, func() {
		t.lock.Lock()
		if gen != t.idleGen {
			t.lock.Unlock()
			return // superseded by newer output or a state change
		}
		t.idleTimer = nil
		t.activeSince = time.Time{}
		if !t.visible {
			t.lock.Unlock()
			return
		}
		t.visible = false
		if t.outputDriven {
			// No shell-integration command to wait on — output stopped, so the activity
			// is over: clear the spinner instead of leaving a "thinking" badge stuck on.
			t.outputDriven = false
			t.setState(termActivityNone)
		} else {
			t.setState(termActivityThinking) // command still running, output just paused
		}
		out := t.outbox
		t.outbox = nil
		t.lock.Unlock()
		publishActivity(out)
	})
}

func (t *termActivityTracker) stopIdleTimer() {
	t.idleGen++
	if t.idleTimer != nil {
		t.idleTimer.Stop()
		t.idleTimer = nil
	}
}

// setState queues a state transition for publishing. De-duplicates so repeated
// signals in the same state don't spam events. Assumes t.lock held.
func (t *termActivityTracker) setState(state string) {
	if t.curState == state {
		return
	}
	t.curState = state
	t.outbox = append(t.outbox, baseds.TermActivityData{
		BlockId:   t.blockId,
		State:     state,
		AgentKind: t.agentKind,
		Command:   t.command,
	})
}

// publishActivity is a package var so tests can capture emitted events instead of
// routing them through the broker. Besides the Event_TermActivity stream (which the
// frontend uses for focus-aware OS notifications), it sets the tab's working/done
// badge straight from the backend so the indicator shows on every tab — active,
// background, or not-yet-opened — without depending on a live renderer.
var publishActivity = func(events []baseds.TermActivityData) {
	for _, ev := range events {
		wps.Broker.Publish(wps.WaveEvent{
			Event:  wps.Event_TermActivity,
			Scopes: []string{waveobj.MakeORef(waveobj.OType_Block, ev.BlockId).String()},
			Data:   ev,
		})
		publishActivityBadge(ev)
	}
}

const activityBadgePriority = 5

var (
	activityBadgeIdsLock sync.Mutex
	activityBadgeIds     = map[string]string{} // blockId -> stable uuidv7 badge id
)

func activityBadgeId(blockId string) string {
	activityBadgeIdsLock.Lock()
	defer activityBadgeIdsLock.Unlock()
	id, ok := activityBadgeIds[blockId]
	if !ok {
		id = uuid.Must(uuid.NewV7()).String()
		activityBadgeIds[blockId] = id
	}
	return id
}

// publishBadgeEvent is a package var so tests can capture emitted badge events.
var publishBadgeEvent = func(oref string, be baseds.BadgeEvent) {
	wps.Broker.Publish(wps.WaveEvent{
		Event:  wps.Event_Badge,
		Scopes: []string{oref},
		Data:   be,
	})
}

// publishActivityBadge maps a per-block activity state to a tab badge and publishes
// it. It clears-by-id first so the set always lands (the badge store only overwrites
// a strictly-higher badge; reusing the id + clearing sidesteps that). Activity badges
// are pidlinked so the frontend's focus-clear leaves them alone — they persist until
// the block's next state change replaces them.
func publishActivityBadge(ev baseds.TermActivityData) {
	oref := waveobj.MakeORef(waveobj.OType_Block, ev.BlockId).String()
	badgeId := activityBadgeId(ev.BlockId)
	var badge *baseds.Badge
	switch ev.State {
	case termActivityWorking, termActivityThinking:
		// "thinking" = command still running but output paused — keep the spinner so a
		// long command's indicator doesn't blink out when it goes quiet. pidlinked so
		// focusing the running tab doesn't clear the live spinner (replaced on done/none).
		badge = &baseds.Badge{BadgeId: badgeId, Icon: "spinner+spin", Color: "var(--accent-color)", Priority: activityBadgePriority, PidLinked: true}
	case termActivityWaiting:
		badge = &baseds.Badge{BadgeId: badgeId, Icon: "comment-dots", Color: "#fbbf24", Priority: activityBadgePriority}
	case termActivityDone:
		if ev.Visible {
			// pidlinked so focusing the tab where the command just finished doesn't
			// instantly wipe the ✓/✗ before you see it (the frontend clears non-pidlinked
			// badges on focus, and "done" lands on the tab you're already looking at). It
			// stays until the next command in that block replaces it.
			if ev.ExitCode == nil || *ev.ExitCode == 0 {
				badge = &baseds.Badge{BadgeId: badgeId, Icon: "circle-check", Color: "var(--success-color)", Priority: activityBadgePriority, PidLinked: true}
			} else {
				badge = &baseds.Badge{BadgeId: badgeId, Icon: "circle-xmark", Color: "var(--error-color)", Priority: activityBadgePriority, PidLinked: true}
			}
		}
	}
	// Single event per transition: a set with the stable badgeid updates the badge in
	// place (the store applies same-id updates), and a clear-by-id removes it. Avoids
	// the clear-then-set broker race that wiped the badge the instant it appeared.
	if badge != nil {
		publishBadgeEvent(oref, baseds.BadgeEvent{ORef: oref, Badge: badge})
	} else {
		publishBadgeEvent(oref, baseds.BadgeEvent{ORef: oref, ClearById: badgeId})
	}
}
