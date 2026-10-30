// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import "os/exec"

var (
	nicePath, _   = exec.LookPath("nice")
	ionicePath, _ = exec.LookPath("ionice")
)

// lowPriority runs the command at nice 19 in the idle I/O class, so a scan
// only uses CPU and disk nobody else wants. Children inherit both.
func lowPriority(argv []string) []string {
	if ionicePath != "" {
		argv = append([]string{ionicePath, "-c", "3"}, argv...)
	}
	if nicePath != "" {
		argv = append([]string{nicePath, "-n", "19"}, argv...)
	}
	return argv
}
