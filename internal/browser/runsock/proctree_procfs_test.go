package runsock

import (
	"strings"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    ProcInfo
		wantErr bool
	}{
		{
			name: "real-looking line",
			// state ppid pgrp session tty tpgid flags minflt cminflt majflt
			// cmajflt utime stime cutime cstime priority nice num_threads
			// itrealvalue starttime ... (field 22 of the file is starttime)
			line: "4321 (chrome) S 4300 4321 4321 0 -1 4194624 12345 0 12 3 45 67 8 9 10 0 1 20 987654321 123456789 0 0 0 0 0",
			want: ProcInfo{PPID: 4300, Start: 987654321},
		},
		{
			name: "comm with parens and spaces",
			line: "999 (web (helper) process) R 998 999 999 0 -1 4194624 1 0 0 0 2 3 0 0 10 0 4 0 556677 0 0 0 0 0",
			want: ProcInfo{PPID: 998, Start: 556677},
		},
		{
			name:    "too few fields",
			line:    "123 (sh) R 1",
			wantErr: true,
		},
		{
			name:    "missing close paren",
			line:    "1234 R 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 1 0 42 0 0 0 0 0",
			wantErr: true,
		},
		{
			name:    "non-numeric ppid",
			line:    "123 (sh) S x 123 123 0 -1 0 0 0 0 0 0 0 0 0 0 1 0 42 0 0 0 0 0",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcStat([]byte(tt.line))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseProcStat(%q) error = %v, want error: %v", tt.line, err, tt.wantErr)
			}
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "stat") {
					t.Errorf("parseProcStat(%q) error = %v, want an error mentioning \"stat\"", tt.line, err)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseProcStat(%q) = %+v, want %+v", tt.line, got, tt.want)
			}
		})
	}
}
