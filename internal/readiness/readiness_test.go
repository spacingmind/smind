package readiness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byID(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, f := range fs {
		m[f.ID] = f
	}
	return m
}

func passingOpts() Options {
	return Options{HasSetupScript: true, TrackedEnv: func(context.Context) ([]string, error) { return []string{"main.go", ".env.example"}, nil }}
}

func goodRepo(t *testing.T) string {
	dir := t.TempDir()
	put(t, dir, "AGENTS.md", strings.Repeat("rules ", 60))
	put(t, dir, "Taskfile.yml", "version: '3'\ntasks:\n  test:\n    cmds: [go test ./...]\n  lint:\n    cmds: [go vet ./...]\n")
	put(t, dir, "go.mod", "module x\n")
	put(t, dir, "go.sum", "")
	put(t, dir, ".gitignore", ".env\nbin/\ndist\n")
	put(t, dir, ".github/workflows/ci.yml", "on: push\n")
	put(t, dir, "README.md", "# x\n")
	put(t, dir, "CONTRIBUTING.md", "hi\n")
	return dir
}

func TestReadiness_PassingRepoFixture(t *testing.T) {
	fs := Check(context.Background(), goodRepo(t), passingOpts())
	if len(fs) != 10 {
		t.Fatalf("findings = %d, want 10", len(fs))
	}
	for _, f := range fs {
		if f.Status == StatusWarn {
			t.Errorf("%s warns: %s", f.ID, f.Evidence)
		}
		if f.Status == StatusPass && f.Fix != "" {
			t.Errorf("%s passes but has a fix", f.ID)
		}
	}
	if byID(fs)["lockfiles"].Status != StatusPass || byID(fs)["tracked-env"].Status != StatusPass {
		t.Fatalf("lockfiles/tracked-env = %+v", byID(fs))
	}
}

func TestReadiness_EmptyRepo_AllWarnWithFix(t *testing.T) {
	fs := Check(context.Background(), t.TempDir(), Options{})
	warn := map[string]bool{"agent-instructions": true, "test-command": true, "lint-command": true, "setup-script": true,
		"gitignore": true, "ci-config": true, "docs": true}
	m := byID(fs)
	for id := range warn {
		f := m[id]
		if f.Status != StatusWarn {
			t.Errorf("%s status = %q, want warn", id, f.Status)
		}
		if f.Fix == "" || f.Evidence == "" || f.Title == "" {
			t.Errorf("%s lacks text: %+v", id, f)
		}
		for _, bad := range []string{"rm ", "sudo", "npm ", "git "} {
			if strings.Contains(f.Fix, bad) {
				t.Errorf("%s fix looks like a command: %q", id, f.Fix)
			}
		}
	}
	for _, id := range []string{"tracked-env", "lockfiles", "conventions"} {
		if m[id].Status != StatusInfo {
			t.Errorf("%s status = %q, want info", id, m[id].Status)
		}
	}
}

func TestReadiness_WarnNotPass(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "AGENTS.md", "tiny")
	put(t, dir, "package.json", `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`)
	put(t, dir, ".gitignore", "*.log\n")
	m := byID(Check(context.Background(), dir, Options{TrackedEnv: func(context.Context) ([]string, error) { return []string{"app/.env.local"}, nil }}))
	for _, id := range []string{"agent-instructions", "test-command", "gitignore", "tracked-env", "lockfiles", "setup-script"} {
		if m[id].Status != StatusWarn {
			t.Errorf("%s = %q (%s), want warn", id, m[id].Status, m[id].Evidence)
		}
	}
}

func TestReadiness_NeverExecutesRepoCode(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(t.TempDir(), "canary")
	hostile := "touch " + canary
	put(t, dir, "Taskfile.yml", "version: '3'\ntasks:\n  test:\n    cmds: ['"+hostile+"']\n  lint:\n    cmds: ['"+hostile+"']\n")
	put(t, dir, "Makefile", "test:\n\t"+hostile+"\nlint:\n\t"+hostile+"\n$(shell "+hostile+")\n")
	put(t, dir, "package.json", `{"scripts":{"test":"`+hostile+`","lint":"`+hostile+`","postinstall":"`+hostile+`"}}`)
	put(t, dir, ".envrc", hostile+"\n")
	put(t, dir, "setup.sh", "#!/bin/sh\n"+hostile+"\n")
	if err := os.Chmod(filepath.Join(dir, "setup.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := byID(Check(context.Background(), dir, Options{}))
	if m["test-command"].Status != StatusPass || m["lint-command"].Status != StatusPass {
		t.Fatalf("scripts were not even discovered: %+v", m)
	}
	if _, err := os.Stat(canary); err == nil {
		t.Fatal("repo code was executed: canary exists")
	}
}

func TestReadiness_SymlinkOutOfRepoNotFollowed(t *testing.T) {
	outside := t.TempDir()
	put(t, outside, "secret.md", "TOPSECRETMARKER "+strings.Repeat("x", 400))
	put(t, outside, "wf/ci.yml", "on: push\n")
	// Directories a symlink stands in for in the MIDDLE of a path (.github/workflows/ci.yml, .circleci/config.yml).
	put(t, outside, "workflows/ci.yml", "on: push\n")
	put(t, outside, "config.yml", "version: 2\n")
	put(t, outside, "gitignore", ".env\nnode_modules\ndist\n")
	dir := t.TempDir()
	for name, target := range map[string]string{
		"AGENTS.md":    filepath.Join(outside, "secret.md"),
		".gitignore":   filepath.Join(outside, "gitignore"),
		"Taskfile.yml": filepath.Join(outside, "secret.md"),
		".github":      outside,
		"docs":         outside,
		".circleci":    outside,
		"README.md":    filepath.Join(outside, "secret.md"),
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	fs := Check(context.Background(), dir, Options{})
	m := byID(fs)
	for _, id := range []string{"agent-instructions", "gitignore", "ci-config", "docs"} {
		if m[id].Status != StatusWarn {
			t.Errorf("%s = %q (%s), want warn (symlink not followed)", id, m[id].Status, m[id].Evidence)
		}
	}
	r := &repo{root: dir}
	if r.isDir(".github/workflows") || len(r.list(".github/workflows")) != 0 || r.isFile(".circleci/config.yml") {
		t.Error("a path through a symlinked directory was followed")
	}
	for _, f := range fs {
		if strings.Contains(f.Evidence+f.Title+f.Fix, "TOPSECRET") {
			t.Fatalf("outside content leaked: %+v", f)
		}
	}
}

func TestReadiness_BoundedReads(t *testing.T) {
	dir := t.TempDir()
	// A huge sparse package.json and a Taskfile whose test task sits past the cap.
	f, err := os.Create(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(50 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	put(t, dir, "Taskfile.yml", "# "+strings.Repeat("x", 2*maxFileBytes)+"\ntasks:\n  test:\n    cmds: [x]\n")
	for i := 0; i < 20000; i++ {
		put(t, dir, filepath.Join(".github/workflows", "w"+strings.Repeat("a", 3)+itoa(i)+".txt"), "")
	}
	start := time.Now()
	m := byID(Check(context.Background(), dir, Options{}))
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("check took %v", d)
	}
	if m["test-command"].Status != StatusWarn {
		t.Fatalf("test task beyond the read cap was seen: %+v", m["test-command"])
	}
	if m["ci-config"].Status != StatusWarn {
		t.Fatalf("ci-config = %+v", m["ci-config"])
	}
	if got := len((&repo{root: dir}).list(".github/workflows")); got > maxDirEntries {
		t.Fatalf("listed %d entries, cap %d", got, maxDirEntries)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

func TestReadiness_TrackedDotEnvWarns(t *testing.T) {
	ctx := context.Background()
	get := func(paths []string, err error) Finding {
		return byID(Check(ctx, t.TempDir(), Options{TrackedEnv: func(context.Context) ([]string, error) { return paths, err }}))["tracked-env"]
	}
	if f := get([]string{"a/.env", "x.go"}, nil); f.Status != StatusWarn || !strings.Contains(f.Evidence, "a/.env") || f.Fix == "" {
		t.Errorf("tracked .env: %+v", f)
	}
	if f := get([]string{".env.example", ".env.sample"}, nil); f.Status != StatusPass {
		t.Errorf("templates should pass: %+v", f)
	}
	if f := get(nil, errors.New("no git")); f.Status != StatusInfo || !strings.Contains(f.Evidence, "skipped") {
		t.Errorf("error should skip: %+v", f)
	}
	if f := byID(Check(ctx, t.TempDir(), Options{}))["tracked-env"]; f.Status != StatusInfo {
		t.Errorf("nil func should skip: %+v", f)
	}
}
