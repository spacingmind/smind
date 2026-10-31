package readiness

import (
	"context"
	"os"
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

func TestLockfiles_NestedPackages(t *testing.T) {
	goFiles := map[string]string{"go.mod": "module x\nrequire a v1\n", "go.sum": ""}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range goFiles {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	ws := `{"workspaces":["apps/*","packages/config"]}`
	tests := []struct {
		name            string
		files           map[string]string
		wantStatus      string
		wantEvidenceSub string
	}{
		{"landing layout", map[string]string{"package.json": ws, "bun.lock": "", "apps/web/package.json": "{}"}, StatusPass, "bun.lock"},
		{"landing root lock missing", map[string]string{"package.json": ws, "apps/web/package.json": "{}"}, StatusWarn, "package.json has no lockfile"},
		{"smind layout", with(map[string]string{"web/package.json": `{"workspaces":["packages/*"]}`, "web/bun.lock": "", "web/packages/ui/package.json": "{}"}), StatusPass, "go.sum"},
		{"smind web without lock", with(map[string]string{"web/package.json": "{}"}), StatusWarn, "web/package.json has no lockfile"},
		{"mobile package-lock", with(map[string]string{"mobile/package.json": "{}", "mobile/package-lock.json": ""}), StatusPass, "mobile/package-lock.json"},
		{"go.mod without require", map[string]string{"go.mod": "module x\n"}, StatusInfo, "no package manifest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tc.files {
				put(t, dir, rel, body)
			}
			f := byID(Check(context.Background(), dir, Options{}))["lockfiles"]
			if f.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (evidence %q)", f.Status, tc.wantStatus, f.Evidence)
			}
			if !strings.Contains(f.Evidence, tc.wantEvidenceSub) {
				t.Fatalf("evidence = %q, want substring %q", f.Evidence, tc.wantEvidenceSub)
			}
		})
	}
	t.Run("symlinked dir not counted", func(t *testing.T) {
		dir, other := t.TempDir(), t.TempDir()
		put(t, other, "package.json", "{}")
		if err := os.Symlink(other, dir+"/web"); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		f := byID(Check(context.Background(), dir, Options{}))["lockfiles"]
		if f.Status != StatusInfo {
			t.Fatalf("status = %q, want info (evidence %q)", f.Status, f.Evidence)
		}
	})
}
