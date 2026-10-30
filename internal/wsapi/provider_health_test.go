package wsapi

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

// specOf returns a built-in provider's spec straight from the registry, the
// same table the daemon's runner reads.
func specOf(t *testing.T, id string) providers.Spec {
	t.Helper()
	spec, ok := providers.New().Get(id)
	if !ok {
		t.Fatalf("no built-in provider %q", id)
	}
	return spec
}

// writeFakeExe puts a fake executable named name on PATH dir: it prints
// output and exits with code. A shell script on unix; on Windows a .cmd
// shim (what npm actually installs), which LookPath finds via PATHEXT.
func writeFakeExe(t *testing.T, dir, name, output string, code int) {
	t.Helper()
	var file, content string
	if runtime.GOOS == "windows" {
		file = filepath.Join(dir, name+".cmd")
		content = fmt.Sprintf("@echo off\r\necho %s\r\nexit /b %d\r\n", output, code)
	} else {
		file = filepath.Join(dir, name)
		content = fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", output, code)
	}
	if err := os.WriteFile(file, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

// TestProviderHealth_MissingBinaryNamed proves a missing binary is reported
// by name, with the spec's install hint appended -- the "what do I do about
// it" part of the diagnostic. Not parallel: it replaces $PATH (t.Setenv).
func TestProviderHealth_MissingBinaryNamed(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SMIND_PATH_SOURCE", "")
	spec := specOf(t, providers.IDCursor)

	res := testProviderSpec(spec)
	if res.OK {
		t.Fatalf("ok = true, want false (nothing on PATH): detail = %q", res.Detail)
	}
	if !strings.Contains(res.Detail, `"cursor-agent" not found on PATH`) {
		t.Errorf("detail = %q, want it to name cursor-agent", res.Detail)
	}
	if !strings.Contains(res.Detail, spec.Health.Install) {
		t.Errorf("detail = %q, want the install hint %q", res.Detail, spec.Health.Install)
	}
}

// TestProviderHealth_LoginMessagePerProvider proves every cli built-in with
// a fake binary on PATH comes back ok, its detail carrying its own Health
// text: the login note exactly when one is configured, the not-verified
// note exactly when Unverified, and opencode (which has neither) carrying
// neither. Not parallel: it replaces $PATH (t.Setenv).
func TestProviderHealth_LoginMessagePerProvider(t *testing.T) {
	ids := []string{providers.IDOpenCode, providers.IDCopilot, providers.IDCline, providers.IDGoose, providers.IDCursor}
	dir := t.TempDir()
	for _, id := range ids {
		spec := specOf(t, id)
		writeFakeExe(t, dir, spec.HealthCommand[0], "tool 2026.10.30", 0)
	}
	t.Setenv("PATH", dir)

	for _, id := range ids {
		spec := specOf(t, id)
		res := testProviderSpec(spec)
		if !res.OK {
			t.Errorf("%s: ok = false, want true: detail = %q", id, res.Detail)
			continue
		}
		if spec.Health.Login != "" && !strings.Contains(res.Detail, spec.Health.Login) {
			t.Errorf("%s: detail = %q, want the login note %q", id, res.Detail, spec.Health.Login)
		}
		if got := strings.Contains(res.Detail, "not verified against a real login"); got != spec.Health.Unverified {
			t.Errorf("%s: not-verified note present = %v, want %v: detail = %q", id, got, spec.Health.Unverified, res.Detail)
		}
	}
	if d := testProviderSpec(specOf(t, providers.IDOpenCode)).Detail; strings.Contains(d, "login:") || strings.Contains(d, "not verified") {
		t.Errorf("opencode detail = %q, want no login or not-verified note", d)
	}
	for _, id := range []string{providers.IDCopilot, providers.IDCursor} {
		if d := testProviderSpec(specOf(t, id)).Detail; !strings.Contains(d, "paid") {
			t.Errorf("%s detail = %q, want it to say the plan is paid", id, d)
		}
	}
}

// TestProviderHealth_TooOldVersion proves a dated build like
// "2025.09.18-7ae6800" is parsed down to 2025.09.18 and gated against
// cursor's MinVersion: found, but reported too old with the version it
// needs. Not parallel: it replaces $PATH (t.Setenv).
func TestProviderHealth_TooOldVersion(t *testing.T) {
	dir := t.TempDir()
	writeFakeExe(t, dir, "cursor-agent", "2025.09.18-7ae6800", 0)
	t.Setenv("PATH", dir)

	res := testProviderSpec(specOf(t, providers.IDCursor))
	if res.OK {
		t.Fatalf("ok = true, want false (2025.09.18 < MinVersion): detail = %q", res.Detail)
	}
	for _, want := range []string{"too old", "2026.01.01"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("detail = %q, want it to contain %q", res.Detail, want)
		}
	}
}

// TestProviderHealth_VersionCommandFailureStillOK proves the version check
// is best effort: a binary that is found but exits 1 printing nothing keeps
// ok true. Not parallel: it replaces $PATH (t.Setenv).
func TestProviderHealth_VersionCommandFailureStillOK(t *testing.T) {
	dir := t.TempDir()
	writeFakeExe(t, dir, "cursor-agent", "", 1)
	t.Setenv("PATH", dir)

	res := testProviderSpec(specOf(t, providers.IDCursor))
	if !res.OK {
		t.Fatalf("ok = false, want true (version check is best effort): detail = %q", res.Detail)
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"2025.09.18", "2026.01.01", -1},
		{"2026.10.22", "2026.01.01", 1},
		{"1.2", "1.2.0", 0},
		{"1.10", "1.9", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
