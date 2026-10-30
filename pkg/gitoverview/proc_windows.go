// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import "os/exec"

// ownProcessGroup is a no-op on Windows; the default cancel kills git only.
func ownProcessGroup(*exec.Cmd) {}
