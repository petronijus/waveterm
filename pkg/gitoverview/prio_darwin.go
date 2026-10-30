// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import "os"

const taskpolicy = "/usr/sbin/taskpolicy"

var hasTaskpolicy = func() bool { _, err := os.Stat(taskpolicy); return err == nil }()

// lowPriority runs the command with the darwin background policy: lowest CPU
// and I/O priority, efficiency cores only on Apple silicon. Children (git's
// remote helpers) inherit it.
func lowPriority(argv []string) []string {
	if !hasTaskpolicy {
		return argv
	}
	return append([]string{taskpolicy, "-b"}, argv...)
}
