// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
)

var agentStateCmd = &cobra.Command{
	Use:   "agentstate waiting|done",
	Short: "report an AI agent's activity state for this terminal",
	Long: `Report an AI agent's activity state for this terminal block, driving the tab
activity badge: "waiting" shows the needs-attention badge (agent is waiting for
your input), "done" shows the turn-finished checkmark.

Intended to be called from agent lifecycle hooks running inside a Wave terminal,
e.g. Claude Code's Notification/Stop hooks or codex's notify program.`,
	Args:                  cobra.ExactArgs(1),
	RunE:                  agentStateRun,
	PreRunE:               preRunSetupRpcClient,
	Example:               "  wsh agentstate waiting --agent claude\n  wsh agentstate done --agent claude",
	DisableFlagsInUseLine: true,
}

var agentStateAgent string

func init() {
	rootCmd.AddCommand(agentStateCmd)
	agentStateCmd.Flags().StringVar(&agentStateAgent, "agent", "", "agent kind reporting the state (e.g. claude, codex, gemini)")
}

func agentStateRun(cmd *cobra.Command, args []string) (rtnErr error) {
	defer func() {
		sendActivity("agentstate", rtnErr == nil)
	}()
	state := args[0]
	if state != "waiting" && state != "done" {
		OutputHelpMessage(cmd)
		return fmt.Errorf("invalid state %q (want waiting or done)", state)
	}
	fullORef, err := resolveBlockArg()
	if err != nil {
		return err
	}
	err = wshclient.SetTermAgentStateCommand(RpcClient, wshrpc.CommandSetTermAgentStateData{
		BlockId: fullORef.OID,
		State:   state,
		Agent:   agentStateAgent,
	}, &wshrpc.RpcOpts{Timeout: 2000})
	if err != nil {
		return fmt.Errorf("setting agent state: %w", err)
	}
	return nil
}
