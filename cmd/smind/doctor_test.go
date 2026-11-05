package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubDoctor(t *testing.T, result any, dialErr error) {
	t.Helper()
	old := doctorDial
	doctorDial = func(context.Context) (wsCaller, func(), error) {
		if dialErr != nil {
			return nil, nil, dialErr
		}
		return func(_ context.Context, method string, _ any, out any) error {
			if method != "doctor.sessions" {
				t.Errorf("method %q", method)
			}
			raw, _ := json.Marshal(result)
			return json.Unmarshal(raw, out)
		}, func() {}, nil
	}
	t.Cleanup(func() { doctorDial = old })
}

// doctorHome points HOME and SMIND_HOME at fresh temp dirs with a ~/.claude.
func doctorHome(t *testing.T) (home, smindHome string) {
	t.Helper()
	home, smindHome = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SMIND_HOME", smindHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func runDoctorCmd(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = runDoctor(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestDoctorPrintsWarnAndFix(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "[warn]") || !strings.Contains(out, "cleanupPeriodDays") || !strings.Contains(out, "  fix: ") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func snapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			sum := ""
			if !d.IsDir() {
				b, _ := os.ReadFile(p)
				sum = fmt.Sprintf("%x", sha256.Sum256(b))
			}
			snap[p] = fmt.Sprintf("%s|%d|%s", info.ModTime(), info.Size(), sum)
			return nil
		})
	}
	return snap
}

func TestDoctorNeverWritesAnything(t *testing.T) {
	home, smindHome := doctorHome(t)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"cleanupPeriodDays": 7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(smindHome, "providers", "x", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubDoctor(t, doctorSessions{}, nil)
	before := snapshot(t, home, smindHome)
	runDoctorCmd()
	runDoctorCmd("--json")
	after := snapshot(t, home, smindHome)
	if len(before) != len(after) {
		t.Fatalf("file set changed: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("%s changed", p)
		}
	}
}

func TestDoctorJSON(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, doctorSessions{Runs: 3, WithRef: 2, Readable: 1, Gone: 1}, nil)
	code, out, _ := runDoctorCmd("--json")
	var got struct {
		Findings []map[string]any `json:"findings"`
		Sessions *doctorSessions  `json:"sessions"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("code %d out %q", code, out)
	}
	if len(got.Findings) == 0 || got.Sessions == nil || got.Sessions.Readable != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestDoctorSessionsLineAndDaemonDown(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, doctorSessions{Runs: 5, WithRef: 4, Readable: 3, Gone: 1}, nil)
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "sessions: 3 of 4 runs with a provider session can still be rebuilt (1 already gone)") ||
		!strings.Contains(out, `Run "smind doctor" again after changing the setting`) {
		t.Fatalf("code %d out %q", code, out)
	}
	stubDoctor(t, nil, errors.New("no daemon"))
	code, out, _ = runDoctorCmd()
	if code != 0 || !strings.Contains(out, "sessions: the daemon is not running") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestDoctorUsage(t *testing.T) {
	doctorHome(t)
	code, _, errOut := runDoctorCmd("--bogus")
	if code != 2 || !strings.Contains(errOut, "smind doctor") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}
