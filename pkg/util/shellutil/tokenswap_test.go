// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package shellutil

import "testing"

func TestEncodeEnvVarsForPowerShell(t *testing.T) {
	tests := []struct {
		name     string
		envName  string
		envValue string
		want     string
	}{
		{
			name:     "ProgramFiles(x86)",
			envName:  "ProgramFiles(x86)",
			envValue: `C:\Program Files (x86)`,
			want:     "${env:ProgramFiles(x86)} = \"C:\\Program Files (x86)\"\n",
		},
		{
			name:     "CommonProgramFiles(x86)",
			envName:  "CommonProgramFiles(x86)",
			envValue: `C:\Program Files\Common Files (x86)`,
			want:     "${env:CommonProgramFiles(x86)} = \"C:\\Program Files\\Common Files (x86)\"\n",
		},
		{
			name:     "PATH",
			envName:  "PATH",
			envValue: `C:\Windows\System32`,
			want:     "${env:PATH} = \"C:\\Windows\\System32\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeEnvVarsForShell(ShellType_pwsh, map[string]string{tt.envName: tt.envValue})
			if err != nil {
				t.Fatalf("EncodeEnvVarsForShell() returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("EncodeEnvVarsForShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeEnvVarsForPowerShellRejectsUnrepresentableNames(t *testing.T) {
	names := []string{"", "invalid}name", "FOO`", "FOO=BAR", "FOO\nBAR", "FOO\rBAR", "FOO\x00BAR"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeEnvVarsForShell(ShellType_pwsh, map[string]string{name: "value"}); err == nil {
				t.Errorf("EncodeEnvVarsForShell() accepted unrepresentable name %q", name)
			}
		})
	}
}

func TestEncodeEnvVarsForBashRejectsWindowsNames(t *testing.T) {
	if _, err := EncodeEnvVarsForShell(ShellType_bash, map[string]string{"ProgramFiles(x86)": "value"}); err == nil {
		t.Errorf("EncodeEnvVarsForShell() accepted %q for bash", "ProgramFiles(x86)")
	}
}
