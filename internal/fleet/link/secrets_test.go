package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFleetSecretsFile0600AndRedacted(t *testing.T) {
	home := t.TempDir()
	sec := OpenSecrets(home)

	// No file until the first Put.
	if _, err := os.Stat(filepath.Join(home, SecretsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fleet-secrets exists before Put: %v", err)
	}

	const pw, relay = "CANARY-PW-1", "CANARY-RELAY-1"
	if err := sec.Put("host-1", Entry{Password: pw, RelaySecret: relay}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, SecretsFile)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("fleet-secrets mode = %o, want 600", got)
		}
	}

	got, ok, err := sec.Get("host-1")
	if err != nil || !ok || got.Password != pw || got.RelaySecret != relay {
		t.Fatalf("Get = %+v, %v, %v; want round-trip", got, ok, err)
	}

	// No formatting verb prints the values.
	e, _, _ := sec.Get("host-1")
	for _, s := range []string{fmt.Sprintf("%v", e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e), fmt.Sprintf("%s", e), fmt.Sprintf("%v %v", &e, []Entry{e})} {
		if strings.Contains(s, pw) || strings.Contains(s, relay) || strings.Contains(s, "CANARY") {
			t.Errorf("printed entry leaks: %q", s)
		}
	}

	// A second handle over the same directory sees the same values.
	again, ok, err := OpenSecrets(home).Get("host-1")
	if err != nil || !ok || again.Password != pw || again.RelaySecret != relay {
		t.Fatalf("second Secrets Get = %+v, %v, %v", again, ok, err)
	}
	if _, ok, _ := OpenSecrets(home).Get("host-2"); ok {
		t.Fatal("unknown host reported as present")
	}

	if err := sec.Delete("host-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sec.Get("host-1"); err != nil || ok {
		t.Fatalf("Get after Delete = %v, %v; want gone", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "{}" {
		t.Fatalf("file after last Delete = %q, want {}", data)
	}

	// A loosened file is refused, not quietly read or tightened.
	if runtime.GOOS != "windows" {
		if err := sec.Put("host-2", Entry{Password: pw}); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := sec.Get("host-2"); !errors.Is(err, ErrInsecure) {
			t.Fatalf("Get on a 0644 file = %v, want ErrInsecure", err)
		}
		if err := sec.Put("host-3", Entry{Password: pw}); !errors.Is(err, ErrInsecure) {
			t.Fatalf("Put on a 0644 file = %v, want ErrInsecure", err)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("hunter2"); got != "[redacted]" {
		t.Fatalf("Redact(secret) = %q", got)
	}
	if got := Redact(""); got != "" {
		t.Fatalf("Redact(\"\") = %q, want empty", got)
	}
}
