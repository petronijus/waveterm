// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !linux

package gitoverview

// lowPriority is a no-op where no priority wrapper is known.
func lowPriority(argv []string) []string { return argv }
