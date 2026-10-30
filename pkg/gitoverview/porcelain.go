// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package gitoverview

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// statusInfo is what one `git status --porcelain=v2 --branch -z` run tells us.
type statusInfo struct {
	NoCommits bool   // branch.oid is "(initial)"
	Branch    string // empty when detached
	Upstream  string // e.g. "origin/main"; empty when the branch tracks nothing
	HasAB     bool   // false with an upstream set means the upstream ref is gone
	Ahead     int
	Behind    int
	Changed   int // tracked entries with staged or unstaged changes
	Untracked int
	Conflicts int
}

func (s statusInfo) dirty() bool {
	return s.Changed+s.Untracked+s.Conflicts > 0
}

// parsePorcelainV2 parses NUL-separated porcelain v2 output with branch headers.
// Rename/copy entries ("2 ...") carry the original path as an extra NUL field.
func parsePorcelainV2(out []byte) (statusInfo, error) {
	var s statusInfo
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case '#':
			if err := s.parseHeader(string(f)); err != nil {
				return s, err
			}
		case '1':
			s.Changed++
		case '2':
			s.Changed++
			i++ // skip the original path
		case 'u':
			s.Conflicts++
		case '?':
			s.Untracked++
		case '!':
			// ignored files are never requested, but harmless if a config enables them
		default:
			return s, fmt.Errorf("unexpected porcelain entry %q", truncate(string(f), 40))
		}
	}
	return s, nil
}

func (s *statusInfo) parseHeader(line string) error {
	key, val, _ := strings.Cut(strings.TrimPrefix(line, "# "), " ")
	switch key {
	case "branch.oid":
		s.NoCommits = val == "(initial)"
	case "branch.head":
		if val != "(detached)" {
			s.Branch = val
		}
	case "branch.upstream":
		s.Upstream = val
	case "branch.ab":
		a, b, ok := strings.Cut(val, " ")
		if !ok {
			return fmt.Errorf("malformed branch.ab %q", val)
		}
		ahead, err1 := strconv.Atoi(strings.TrimPrefix(a, "+"))
		behind, err2 := strconv.Atoi(strings.TrimPrefix(b, "-"))
		if err1 != nil || err2 != nil {
			return fmt.Errorf("malformed branch.ab %q", val)
		}
		s.HasAB, s.Ahead, s.Behind = true, ahead, behind
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
