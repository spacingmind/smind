package readiness

import (
	"context"
	"strings"
	"testing"
)

func TestLint_TypecheckAloneIsNotLint(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string
		wantStatus      string
		wantEvidenceSub string
	}{
		{"check-types script", map[string]string{"package.json": `{"scripts":{"check-types":"tsc --noEmit"}}`}, StatusWarn, "type checking alone"},
		{"typecheck script", map[string]string{"package.json": `{"scripts":{"typecheck":"tsc"}}`}, StatusWarn, ""},
		{"check-types plus lint", map[string]string{"package.json": `{"scripts":{"check-types":"tsc","lint":"eslint ."}}`}, StatusPass, "package.json script lint"},
		{"format script", map[string]string{"package.json": `{"scripts":{"format":"prettier -w ."}}`}, StatusPass, ""},
		{"check with biome", map[string]string{"package.json": `{"scripts":{"check":"biome check ."}}`}, StatusPass, ""},
		{"check with tsc", map[string]string{"package.json": `{"scripts":{"check":"tsc --noEmit"}}`}, StatusWarn, ""},
		{"taskfile check-types", map[string]string{"Taskfile.yml": "version: '3'\ntasks:\n  check-types:\n    cmds: [tsc]\n"}, StatusWarn, ""},
		{"taskfile lint", map[string]string{"Taskfile.yml": "version: '3'\ntasks:\n  lint:\n    cmds: [golangci-lint run]\n"}, StatusPass, "Taskfile.yml has a lint task"},
		{"makefile check-types", map[string]string{"Makefile": "check-types:\n\ttsc\n"}, StatusWarn, ""},
		{"makefile vet", map[string]string{"Makefile": "vet:\n\tgo vet ./...\n"}, StatusPass, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tc.files {
				put(t, dir, rel, body)
			}
			f := byID(Check(context.Background(), dir, Options{}))["lint-command"]
			if f.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (evidence %q)", f.Status, tc.wantStatus, f.Evidence)
			}
			if !strings.Contains(f.Evidence, tc.wantEvidenceSub) {
				t.Fatalf("evidence = %q, want substring %q", f.Evidence, tc.wantEvidenceSub)
			}
		})
	}
}
