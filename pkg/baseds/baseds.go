// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// used for shared datastructures
package baseds

type LinkId int32

const NoLinkId = 0

type RpcInputChType struct {
	MsgBytes      []byte
	IngressLinkId LinkId
}

type Badge struct {
	BadgeId   string  `json:"badgeid"` // must be a uuidv7
	Icon      string  `json:"icon"`
	Color     string  `json:"color,omitempty"`
	Priority  float64 `json:"priority"`
	PidLinked bool    `json:"pidlinked,omitempty"`
}

type BadgeEvent struct {
	ORef      string `json:"oref"`
	Clear     bool   `json:"clear,omitempty"`
	ClearAll  bool   `json:"clearall,omitempty"`
	ClearById string `json:"clearbyid,omitempty"`
	Badge     *Badge `json:"badge,omitempty"`
}

// TermActivityData reports a terminal block's current command-activity state,
// derived on the backend from the PTY output stream (OSC 16162 command markers +
// live output volume). It lives on the backend — not in the per-tab terminal view —
// so it keeps working for background tabs whose view is unmounted. The frontend
// turns this into focus-aware tab badges and OS notifications.
//
// State values:
//   - "working":  a command is running and producing sustained output (spinner). Live.
//   - "thinking": running but output went quiet (came-to-rest "done thinking"). Attention.
//   - "waiting":  an AI agent (claude/gemini/codex) signalled "your turn" (bell/OSC 9). Attention.
//   - "done":     the command finished (carries ExitCode). Attention.
//   - "none":     no command activity (quick/silent command, reset, or cleared).
type TermActivityData struct {
	BlockId    string `json:"blockid"`
	State      string `json:"state"`
	Visible    bool   `json:"visible,omitempty"`    // spinner was shown at least once this command (badge-worthy)
	ExitCode   *int   `json:"exitcode,omitempty"`   // set when State=="done"
	AgentKind  string `json:"agentkind,omitempty"`  // which AI agent is running, if any
	Command    string `json:"command,omitempty"`    // last command string (for notification body)
	DurationMs int64  `json:"durationms,omitempty"` // command duration so far (for notification duration-gating)
}
