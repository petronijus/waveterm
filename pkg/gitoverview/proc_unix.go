// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package gitoverview

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the command in a new session — its own process
// group, with no controlling terminal for ssh to prompt on — and makes a
// context cancel kill the whole group, not just the direct child.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
