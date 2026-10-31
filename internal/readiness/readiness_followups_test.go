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
		{"smind web without lock", with(map[string]string{"web/package.json": `{"dependencies":{"x":"1"}}`}), StatusWarn, "web/package.json has no lockfile"},
		{"mobile package-lock", with(map[string]string{"mobile/package.json": `{"dependencies":{"x":"1"}}`, "mobile/package-lock.json": ""}), StatusPass, "mobile/package-lock.json"},
		{"npm-stub without deps", with(map[string]string{"npm-stub/package.json": `{"name":"smind","version":"0.0.1"}`}), StatusPass, "go.sum"},
		{"web devDependencies without lock", with(map[string]string{"web/package.json": `{"devDependencies":{"x":"1"}}`}), StatusWarn, "web/package.json has no lockfile"},
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

func TestTestCommand_WorkspacePackages(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string
		wantStatus      string
		wantEvidenceSub string
	}{
		{"landing before", map[string]string{
			"package.json":          `{"workspaces":["apps/*"],"scripts":{"check-types":"turbo check-types"}}`,
			"apps/web/package.json": `{"scripts":{"test":"bun test"}}`,
		}, StatusPass, "apps/web/package.json script test"},
		{"landing after", map[string]string{
			"package.json":          `{"workspaces":["apps/*"],"scripts":{"test":"turbo test"}}`,
			"apps/web/package.json": `{"scripts":{"test":"bun test"}}`,
		}, StatusPass, "package.json script test"},
		{"no test anywhere", map[string]string{
			"package.json":          `{"scripts":{"build":"x"}}`,
			"apps/web/package.json": `{"scripts":{"build":"y"}}`,
		}, StatusWarn, ""},
		{"npm placeholder", map[string]string{
			"package.json":          `{"workspaces":["apps/*"]}`,
			"apps/web/package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`,
		}, StatusWarn, ""},
		{"top-level web package", map[string]string{
			"web/package.json": `{"scripts":{"test":"vitest"}}`,
		}, StatusPass, "web/package.json script test"},
		{"test:unit key", map[string]string{
			"packages/x/package.json": `{"scripts":{"test:unit":"bun test"}}`,
		}, StatusPass, ""},
		{"nested too deep", map[string]string{
			"web/packages/ui/package.json": `{"scripts":{"test":"vitest"}}`,
		}, StatusWarn, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tc.files {
				put(t, dir, rel, body)
			}
			f := byID(Check(context.Background(), dir, Options{}))["test-command"]
			if f.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (evidence %q)", f.Status, tc.wantStatus, f.Evidence)
			}
			if tc.name == "landing after" {
				if f.Evidence != tc.wantEvidenceSub {
					t.Fatalf("evidence = %q, want exactly %q", f.Evidence, tc.wantEvidenceSub)
				}
				return
			}
			if !strings.Contains(f.Evidence, tc.wantEvidenceSub) {
				t.Fatalf("evidence = %q, want substring %q", f.Evidence, tc.wantEvidenceSub)
			}
		})
	}
}

func TestIgnores_Gitignore(t *testing.T) {
	tests := []struct {
		lines string
		name  string
		file  bool
		want  bool
	}{
		{".env", ".env", true, true},
		{".env*\n!.env.example", ".env", true, true},
		{".env\n!.env", ".env", true, false},
		{".env.*\n!.env.example", ".env", true, false},
		{"*.env", ".env", true, true},
		{"# .env", ".env", true, false},
		{"node_modules", "node_modules", false, true},
		{"node_modules/", "node_modules", false, true},
		{"/node_modules", "node_modules", false, true},
		{"**/node_modules", "node_modules", false, true},
		{"web/node_modules/", "node_modules", false, false},
		{"dist/**", "dist", false, true},
		{"dist/*", "dist", false, true},
		{"/dist", "dist", false, true},
		{"dist/", "dist", false, true},
		{"dist\n!dist", "dist", false, false},
		{"build/**\n!build/keep", "build", false, true},
		{"out ", "out", false, true},
		{"*.log", "dist", false, false},
		{"target\r\n", "target", false, true},
	}
	for _, tc := range tests {
		t.Run(strings.ReplaceAll(tc.lines, "\n", "|")+"/"+tc.name, func(t *testing.T) {
			lines := strings.Split(tc.lines, "\n")
			got := ignores(lines, tc.name)
			if tc.file {
				got = ignoresFile(lines, tc.name)
			}
			if got != tc.want {
				t.Fatalf("ignores(%q, %q) = %v, want %v", tc.lines, tc.name, got, tc.want)
			}
		})
	}
}

func TestCheckGitignore_Layouts(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string
		wantStatus      string
		wantEvidenceSub string
	}{
		{"landing", map[string]string{"package.json": "{}", ".gitignore": "node_modules\n.pnp\n\ndist\nbuild\n*.tsbuildinfo\n\n.env\n.env*.local\n"}, StatusPass, ""},
		{"smind", map[string]string{"go.mod": "module x\n", ".gitignore": "bin/\n/fakeagent\nweb/node_modules/\ninternal/server/dist/*\n!internal/server/dist/.gitkeep\n.env\n.env.*\n!.env.example\n"}, StatusPass, ""},
		{"missing env", map[string]string{"package.json": "{}", ".gitignore": "*.log\n"}, StatusWarn, ".env"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tc.files {
				put(t, dir, rel, body)
			}
			f := byID(Check(context.Background(), dir, Options{}))["gitignore"]
			if f.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (evidence %q)", f.Status, tc.wantStatus, f.Evidence)
			}
			if !strings.Contains(f.Evidence, tc.wantEvidenceSub) {
				t.Fatalf("evidence = %q, want substring %q", f.Evidence, tc.wantEvidenceSub)
			}
		})
	}
}
