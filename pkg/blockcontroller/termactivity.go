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
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
	"github.com/wavetermdev/waveterm/pkg/wps"
)

// Activity heuristic timings — must stay in sync with the documented behavior the
// frontend used (see the old osc-handlers.ts CmdActivity* constants).
const (
	cmdActivityDelay    = 1200 * time.Millisecond // ignore the first burst so quick commands don't flash
	cmdActivityIdle     = 2500 * time.Millisecond // output quiet this long ⇒ "done thinking" / idle (command-tracked)
	cmdActivityDoneIdle = 4000 * time.Millisecond // output-only: quiet this long ⇒ "done" ✓ — long enough to ride out an agent's mid-turn pauses without flickering
	cmdActivitySustain  = 700 * time.Millisecond  // output must flow this long continuously before we call it "working"
	cmdActivityGap      = 1000 * time.Millisecond // a quiet gap longer than this ends the continuous stretch
	maxOscBufLen        = 8192                    // cap a single OSC payload so malformed input can't grow unbounded
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
	scanInCsi // input-side: CSI parameter/intermediate bytes until a final byte
	scanInSs3 // input-side: single byte after ESC O (SS3 function keys)
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

// Package-runner / exec wrappers that precede the real command. Stripping them lets
// `npx vite`, `bundle exec rails s`, `poetry run uvicorn` classify by what they actually
// run, so both agent and server detection see through the wrapper.
var runnerPrefixRegex = regexp.MustCompile(`^(?:(?:npx|bunx)(?:\s+(?:-y|--yes))?|(?:pnpm|yarn|bun)\s+(?:exec|dlx)|bundle\s+exec|(?:poetry|pipenv|uv|pdm|rye|hatch)\s+run)\s+`)

func normalizeCmd(cmd string) string {
	s := strings.TrimSpace(cmd)
	s = envCmdPrefixRegex.ReplaceAllString(s, "")
	s = envVarPrefixRegex.ReplaceAllString(s, "")
	for {
		stripped := runnerPrefixRegex.ReplaceAllString(s, "")
		if stripped == s {
			break
		}
		s = stripped
	}
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

// Long-running dev servers / file watchers: their normal state is "running forever" and
// they never emit a command-done marker, so the working spinner would otherwise spin for
// the entire life of the server. Detected by command (mirroring agentCommandRegexes) so a
// running server leaves the tab indicator clean instead of pinning a perpetual spinner.
// Agents and ordinary commands are unaffected.
var serverCommandRegexes = []*regexp.Regexp{
	// JS/TS: most projects wrap the dev server in a package.json script
	regexp.MustCompile(`^(npm|pnpm|yarn|bun)\s+(run\s+)?(dev|start|serve|watch|preview|storybook)\b`),
	// ...and the dev servers / watchers when invoked directly
	regexp.MustCompile(`^vite\b`),
	regexp.MustCompile(`^(next|nuxt|astro|remix|gatsby|docusaurus|redwood|blitz)\s+(dev|develop|start)\b`),
	regexp.MustCompile(`^ng\s+serve\b`),
	regexp.MustCompile(`^vue-cli-service\s+serve\b`),
	regexp.MustCompile(`^(webpack-dev-server|webpack\s+serve)\b`),
	regexp.MustCompile(`^parcel\s+(serve|watch)\b`),
	regexp.MustCompile(`^(nodemon|ts-node-dev|pm2-dev)\b`),
	regexp.MustCompile(`^tsx\s+watch\b`),
	regexp.MustCompile(`^node\s+--watch\b`),
	regexp.MustCompile(`^(http-server|live-server|json-server|browser-sync|serve)\b`),
	regexp.MustCompile(`^(storybook\s+dev|start-storybook)\b`),
	regexp.MustCompile(`^expo\s+start\b`),
	regexp.MustCompile(`^deno\s+(task\s+(dev|start|serve|watch)|run\b.*--watch)`),
	// cloud / dev-platform local emulators & tunnels
	regexp.MustCompile(`^(wrangler|netlify|vercel|supabase|firebase|convex|encore)\s+(dev|serve|start|emulators)\b`),
	regexp.MustCompile(`^shopify\s+(theme|app|hydrogen)\s+dev\b`),
	// Python
	regexp.MustCompile(`^python[0-9.]*\s+-m\s+(http\.server|uvicorn|flask|gunicorn)\b`),
	regexp.MustCompile(`^(?:python[0-9.]*\s+|\./)?manage\.py\s+runserver\b`),
	regexp.MustCompile(`^(uvicorn|gunicorn|hypercorn|daphne|granian)\b`),
	regexp.MustCompile(`^flask\s+run\b`),
	regexp.MustCompile(`^fastapi\s+(dev|run)\b`),
	regexp.MustCompile(`^streamlit\s+run\b`),
	// Ruby
	regexp.MustCompile(`^rails\s+(server|s)\b`),
	regexp.MustCompile(`^(puma|rackup|thin|unicorn)\b`),
	regexp.MustCompile(`^foreman\s+start\b`),
	regexp.MustCompile(`^(jekyll|hugo|mkdocs)\s+serve(r)?\b`),
	// PHP
	regexp.MustCompile(`^php\s+-S\b`),
	regexp.MustCompile(`^php\s+artisan\s+serve\b`),
	regexp.MustCompile(`^symfony\s+(server:start|serve)\b`),
	// Go / infra
	regexp.MustCompile(`^air\b`),
	regexp.MustCompile(`^caddy\s+run\b`),
	regexp.MustCompile(`^docker[- ]compose\s+up\b`),
}

func isServerCommand(cmd string) bool {
	if cmd == "" {
		return false
	}
	normalized := normalizeCmd(cmd)
	for _, re := range serverCommandRegexes {
		if re.MatchString(normalized) {
			return true
		}
	}
	return false
}

type termActivityTracker struct {
	lock    sync.Mutex
	blockId string

	// incremental scanner state
	scanMode int
	oscBuf   []byte

	// input-side escape parser state (chunk boundaries must not split sequences)
	inScanMode int

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

	// debug-only throttled output logging (term:activitydebug)
	lastFeedLogTs  time.Time
	bytesSinceLog  int
	chunksSinceLog int

	outbox []baseds.TermActivityData
}

// activityDebugEnabled reports whether the term:activitydebug setting is on. Cheap
// (the watcher returns a cached *FullConfigType) so it's fine to gate hot-path logs.
func activityDebugEnabled() bool {
	w := wconfig.GetWatcher()
	if w == nil {
		return false
	}
	return w.GetFullConfig().Settings.TermActivityDebug
}

// dbg emits a single tagged diagnostic line when term:activitydebug is on. The
// "[tabactivity]" tag (under wavesrv's "[wavesrv]" prefix) lands in waveapp.log and is
// grep-friendly. Assumes t.lock held by the caller (it only reads tracker fields the
// caller already owns). blk is shortened so lines stay scannable.
func (t *termActivityTracker) dbg(format string, args ...any) {
	if !activityDebugEnabled() {
		return
	}
	log.Printf("[tabactivity] blk=%s "+format, append([]any{shortBlk(t.blockId)}, args...)...)
}

func shortBlk(blockId string) string {
	if len(blockId) > 8 {
		return blockId[:8]
	}
	return blockId
}

func truncCmd(cmd string) string {
	cmd = strings.ReplaceAll(cmd, "\n", " ")
	if len(cmd) > 60 {
		return cmd[:60] + "…"
	}
	return cmd
}

func exitCodeStr(ec *int) string {
	if ec == nil {
		return "nil"
	}
	return strconv.Itoa(*ec)
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

// FeedTermUserInput inspects user input bound for a block's pty. A deliberate
// keypress — printable text, Enter, Tab — is what actually ends an agent's
// "waiting for you" state (the user answered), so it releases the sticky waiting
// flag and lets the output heuristic take over again. The terminal's automatic
// escape-sequence replies (DSR/DA/OSC responses), arrow-key browsing and bare
// control chords don't count. Safe to call from the input paths.
func FeedTermUserInput(blockId string, data []byte) {
	if len(data) == 0 {
		return
	}
	getActivityTracker(blockId).feedUserInput(data)
}

func (t *termActivityTracker) feedUserInput(data []byte) {
	t.lock.Lock()
	defer t.lock.Unlock()
	// always run the scanner — it must track sequence state even while not waiting,
	// or a chunk boundary inside a sequence would desync the parser
	acted := t.scanInputForUserAction(data)
	if !acted || !t.waiting {
		return
	}
	t.waiting = false
	t.dbg("user input -> waiting released (state stays %q until output/markers move it)", t.curState)
}

// scanInputForUserAction advances the input-side escape parser across the chunk and
// reports whether it carried a deliberate keypress: printable bytes (incl. UTF-8 and
// bracketed-paste payload), CR/LF or Tab outside escape sequences. Arrow keys, focus
// events and query replies are complete CSI/SS3/OSC sequences and don't count —
// browsing a menu or the terminal answering a query must not release the waiting
// state; confirming with Enter or typing text does. Assumes t.lock held.
func (t *termActivityTracker) scanInputForUserAction(data []byte) bool {
	acted := false
	for _, b := range data {
		switch t.inScanMode {
		case scanNormal:
			switch {
			case b == byteEsc:
				t.inScanMode = scanEsc
			case b == '\r' || b == '\n' || b == '\t':
				acted = true
			case b >= 0x20 && b != 0x7f: // printable ASCII; >0x7f covers UTF-8 bytes
				acted = true
			}
		case scanEsc:
			switch b {
			case '[':
				t.inScanMode = scanInCsi
			case 'O':
				t.inScanMode = scanInSs3
			case ']':
				t.inScanMode = scanOsc
			case 'P', 'X', '^', '_':
				t.inScanMode = scanString
			default:
				// Alt+key chord — a keypress, but not an answer
				t.inScanMode = scanNormal
			}
		case scanInCsi:
			if b >= 0x40 && b <= 0x7e {
				t.inScanMode = scanNormal
			}
		case scanInSs3:
			t.inScanMode = scanNormal
		case scanOsc:
			switch b {
			case byteBel:
				t.inScanMode = scanNormal
			case byteEsc:
				t.inScanMode = scanOscEsc
			}
		case scanOscEsc, scanStringEsc:
			if b == byteST {
				t.inScanMode = scanNormal
			} else if b != byteEsc {
				t.inScanMode = scanNormal
			}
		case scanString:
			switch b {
			case byteBel:
				t.inScanMode = scanNormal
			case byteEsc:
				t.inScanMode = scanStringEsc
			}
		}
	}
	return acted
}

// SetExternalAgentState applies an explicit agent-reported activity state pushed
// in-band via wsh (`wsh agentstate` — wired to agent lifecycle hooks like Claude
// Code's Notification/Stop or codex's notify). Explicit signals beat the output
// heuristics: "waiting" shows the needs-attention badge immediately, "done" shows
// the turn-finished checkmark even though the agent process keeps running.
func SetExternalAgentState(blockId string, state string, agent string) error {
	t := getActivityTracker(blockId)
	t.lock.Lock()
	switch state {
	case termActivityWaiting:
		t.stopIdleTimer()
		t.waiting = true
		t.visible = false
		t.activeSince = time.Time{}
		t.stretchBytes = 0
		if agent != "" {
			t.agentKind = agent
		}
		t.dbg("external agentstate -> waiting (agent=%q)", t.agentKind)
		t.setState(termActivityWaiting)
	case termActivityDone:
		t.stopIdleTimer()
		// waiting stays true: the agent idles at its prompt after a turn ends and its
		// TUI keeps repainting — the sticky waiting flag keeps that dribble from
		// re-tripping the spinner over the ✓ until the user types the next prompt
		// (feedUserInput releases it).
		t.waiting = true
		t.visible = false
		t.everShown = false
		t.outputDriven = false
		t.activeSince = time.Time{}
		t.stretchBytes = 0
		if agent != "" {
			t.agentKind = agent
		}
		durMs := int64(0)
		if !t.startTs.IsZero() {
			durMs = time.Since(t.startTs).Milliseconds()
		}
		t.startTs = time.Time{}
		t.curState = termActivityDone
		t.dbg("external agentstate -> done (agent=%q durMs=%d)", t.agentKind, durMs)
		t.outbox = append(t.outbox, baseds.TermActivityData{
			BlockId:    t.blockId,
			State:      termActivityDone,
			Visible:    true,
			AgentKind:  t.agentKind,
			Command:    t.command,
			DurationMs: durMs,
		})
	default:
		t.lock.Unlock()
		return fmt.Errorf("invalid agent state %q (want %q or %q)", state, termActivityWaiting, termActivityDone)
	}
	out := t.outbox
	t.outbox = nil
	t.lock.Unlock()
	publishActivity(out)
	return nil
}

// ResetTermActivity tears down a block's activity tracker (on block destroy,
// controller replacement, or shell restart), clearing any lingering indicator.
func ResetTermActivity(blockId string) {
	activityTrackersLock.Lock()
	t := activityTrackers[blockId]
	delete(activityTrackers, blockId)
	activityTrackersLock.Unlock()
	resetAgentProbeCache(blockId)
	if t == nil {
		return
	}
	t.lock.Lock()
	t.dbg("reset tracker (prevState=%s running=%v visible=%v)", t.curState, t.running, t.visible)
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
	t.dbg("osc-C start command=%q agent=%q", truncCmd(t.command), t.agentKind)
	if t.agentKind == "claude" {
		trackClaudeSession(t.blockId, t.command)
	}
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
		t.dbg("osc-D/A end ignored (no command tracked, never shown) exit=%s", exitCodeStr(exitCode))
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
	t.dbg("osc-D/A end -> done exit=%s durMs=%d command=%q (emit visible=%v)", exitCodeStr(exitCode), durMs, truncCmd(t.command), visible)
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
	t.dbg("osc-R cancel (was running=%v visible=%v)", t.running, t.visible)
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
	t.bytesSinceLog += n
	t.chunksSinceLog++
	t.maybeFeedLog(now)
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
	// While in the bell/hook-driven "waiting for you" state an agent TUI keeps
	// repainting its idle prompt — a continuous dribble (claude idles at ~350B/s in
	// one unbroken stretch) that no cumulative volume threshold can tell apart from
	// real work. Waiting is therefore sticky against output; only actual user input
	// (the answer the agent asked for) releases it — see feedUserInput.
	if !t.visible && !t.waiting && now.Sub(t.activeSince) >= cmdActivitySustain {
		t.visible = true
		t.everShown = true
		if !t.running {
			t.outputDriven = true // no command boundary; the idle timer will end it
			if t.startTs.IsZero() {
				t.startTs = t.activeSince // anchor duration to when this output run began
			}
		}
		t.dbg("spinner on (stretch=%dms bytes=%d running=%v outputDriven=%v)",
			now.Sub(t.activeSince).Milliseconds(), t.stretchBytes, t.running, t.outputDriven)
		t.setState(termActivityWorking)
	}
	if t.visible {
		t.armIdleTimer()
	}
}

// maybeFeedLog emits at most one throttled "output still flowing" line per second when
// term:activitydebug is on. This is the line that reveals a spinner kept alive by a TUI
// or agent dribbling output (each chunk re-arms the idle timer), which presents to the
// user as "spinner spinning but terminal idle". Assumes t.lock held.
func (t *termActivityTracker) maybeFeedLog(now time.Time) {
	if !activityDebugEnabled() {
		return
	}
	if t.lastFeedLogTs.IsZero() {
		t.lastFeedLogTs = now
		return
	}
	if now.Sub(t.lastFeedLogTs) < time.Second {
		return
	}
	sinceOut := int64(0)
	if !t.lastOutputTs.IsZero() {
		sinceOut = now.Sub(t.lastOutputTs).Milliseconds()
	}
	t.dbg("feed %dB/%dchunks in %dms state=%s running=%v outputDriven=%v visible=%v waiting=%v sinceLastOut=%dms",
		t.bytesSinceLog, t.chunksSinceLog, now.Sub(t.lastFeedLogTs).Milliseconds(),
		t.curState, t.running, t.outputDriven, t.visible, t.waiting, sinceOut)
	t.lastFeedLogTs = now
	t.bytesSinceLog = 0
	t.chunksSinceLog = 0
}

func (t *termActivityTracker) markWaiting() {
	if t.agentKind == "" {
		// No tracked agent command — the C marker never fired (broken preexec) or a
		// durable session outlived the wavesrv that saw it. The pty's process tree is
		// ground truth either way: identify the agent from the shell's descendants.
		// Bells from non-agent programs (bare shell, random TUIs) still get ignored.
		t.agentKind = probeAgentKind(t.blockId)
		if t.agentKind == "" {
			t.dbg("bell/osc9 ignored (no tracked or probed agent, running=%v command=%q)", t.running, truncCmd(t.command))
			return
		}
	}
	t.dbg("bell/osc9 -> waiting (agent=%q running=%v)", t.agentKind, t.running)
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
	// Output-only activity waits out a longer quiet window before calling it "done" so an
	// agent's mid-turn pauses don't flicker the spinner to a ✓ and back.
	idleDur := cmdActivityIdle
	if t.outputDriven {
		idleDur = cmdActivityDoneIdle
	}
	t.idleTimer = time.AfterFunc(idleDur, func() {
		t.lock.Lock()
		if gen != t.idleGen {
			t.lock.Unlock()
			return // superseded by newer output or a state change
		}
		t.idleTimer = nil
		t.activeSince = time.Time{}
		if !t.visible {
			t.dbg("idle-fire after %dms but not visible — no-op", idleDur.Milliseconds())
			t.lock.Unlock()
			return
		}
		t.visible = false
		if t.outputDriven {
			t.dbg("idle-fire after %dms -> done (output-only, no command boundary)", idleDur.Milliseconds())
			// Output stopped with no shell-integration command to bound it (broken C/D
			// markers, or an interactive agent like Claude between turns). The spinner has
			// already ridden out a longer quiet window (cmdActivityDoneIdle) without new
			// output, so treat this as "done" and show a ✓ — short mid-task pauses keep the
			// spinner instead of flickering. A new burst of output starts a fresh spinner,
			// and a later real D marker upgrades the ✓ to the true ✓/✗ exit status.
			t.outputDriven = false
			durMs := int64(0)
			if !t.startTs.IsZero() {
				durMs = time.Since(t.startTs).Milliseconds()
			}
			t.startTs = time.Time{}
			t.everShown = false
			t.curState = termActivityDone
			t.outbox = append(t.outbox, baseds.TermActivityData{
				BlockId:    t.blockId,
				State:      termActivityDone,
				Visible:    true,
				Command:    t.command,
				DurationMs: durMs,
			})
		} else {
			t.dbg("idle-fire after %dms -> thinking (command still running, output paused)", idleDur.Milliseconds())
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
	t.dbg("transition %s -> %s (running=%v outputDriven=%v visible=%v waiting=%v agent=%q)",
		t.curState, state, t.running, t.outputDriven, t.visible, t.waiting, t.agentKind)
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
		// A long-running dev server is the exception: it never ends, so leave badge nil
		// (clears any existing) and keep the tab clean rather than spinning forever.
		if !isServerCommand(ev.Command) {
			badge = &baseds.Badge{BadgeId: badgeId, Icon: "spinner+spin", Color: "var(--accent-color)", Priority: activityBadgePriority, PidLinked: true}
		}
	case termActivityWaiting:
		badge = &baseds.Badge{BadgeId: badgeId, Icon: "comment-dots", Color: "#fbbf24", Priority: activityBadgePriority}
	case termActivityDone:
		if ev.Visible {
			// Not pidlinked: the ✓/✗ is an attention cue for a tab you're NOT on. When you
			// focus the tab it clears (app.tsx focus-clear) shortly after — you've seen it.
			// On background tabs it persists until that block's next state change. (The
			// clear/set broker race that used to wipe it instantly is fixed in the badge
			// store's same-id update, so pidlinked is no longer needed to protect it.)
			if ev.ExitCode == nil || *ev.ExitCode == 0 {
				badge = &baseds.Badge{BadgeId: badgeId, Icon: "circle-check", Color: "var(--success-color)", Priority: activityBadgePriority}
			} else {
				badge = &baseds.Badge{BadgeId: badgeId, Icon: "circle-xmark", Color: "var(--error-color)", Priority: activityBadgePriority}
			}
		}
	}
	// Single event per transition: a set with the stable badgeid updates the badge in
	// place (the store applies same-id updates), and a clear-by-id removes it. Avoids
	// the clear-then-set broker race that wiped the badge the instant it appeared.
	if badge != nil {
		if activityDebugEnabled() {
			log.Printf("[tabactivity] blk=%s badge set icon=%s (state=%s)", shortBlk(ev.BlockId), badge.Icon, ev.State)
		}
		publishBadgeEvent(oref, baseds.BadgeEvent{ORef: oref, Badge: badge})
	} else {
		if activityDebugEnabled() {
			log.Printf("[tabactivity] blk=%s badge clear (state=%s)", shortBlk(ev.BlockId), ev.State)
		}
		publishBadgeEvent(oref, baseds.BadgeEvent{ORef: oref, ClearById: badgeId})
	}
}
