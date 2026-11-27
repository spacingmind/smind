package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_config_audit_file_is_private_and_rotates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := filepath.Join(home, auditFileName)

	// A pre-existing 0644 file is tightened on the next append.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendAudit(AuditEntry{Key: "plugins.enabled", Old: "false", New: "true", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("pre-existing file mode = %v, want 0600", st.Mode().Perm())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := AppendAudit(AuditEntry{Key: "plugins.enabled", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("new file mode = %v, want 0600", st.Mode().Perm())
	}

	// Lower the limit so the third line forces exactly one rotation: the
	// first two lines end up in .1, the third in a fresh current file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	old := auditMaxBytes
	auditMaxBytes = 150
	t.Cleanup(func() { auditMaxBytes = old })
	for i := 0; i < 3; i++ {
		if err := AppendAudit(AuditEntry{Key: "plugins.enabled", Outcome: "ok", Caller: "human", Channel: "loopback"}); err != nil {
			t.Fatal(err)
		}
	}
	var total int
	for _, p := range []string{path, path + ".1"} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", p, st.Mode().Perm())
		}
		b, _ := os.ReadFile(p)
		total += strings.Count(string(b), "\n")
	}
	if total != 3 {
		t.Fatalf("lines across both files = %d, want 3", total)
	}
}

func Test_config_audit_never_contains_a_secret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := AppendAudit(AuditEntry{Key: "server.passwordHash", Old: "hunter2", New: "hunter3", Outcome: "refused"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, auditFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"hunter2", "hunter3"} {
		if strings.Contains(string(b), s) {
			t.Errorf("audit file contains %q", s)
		}
	}
	if !strings.Contains(string(b), "redacted") {
		t.Error("expected <redacted> marker")
	}
}
