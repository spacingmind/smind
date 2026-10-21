package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAttestationForTest_IsValidAndGuarded(t *testing.T) {
	now := time.Now()
	if !AttestationForTest(t, now).Valid(now) {
		t.Fatal("AttestationForTest is not valid")
	}
	defer func() {
		if recover() == nil {
			t.Error("a nil testing.TB did not panic: production code could mint an attestation")
		}
	}()
	_ = AttestationForTest(nil, now)
}

// TestAttestationForTest_NotUsedInProductionCode keeps the helper out of
// non-test files across the module.
func TestAttestationForTest_NotUsedInProductionCode(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	var bad []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "refs", "web", "desktop", "mobile":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || filepath.Base(p) == "testing.go" {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "AttestationForTest(") {
			bad = append(bad, p)
		}
		return nil
	})
	if len(bad) > 0 {
		t.Errorf("AttestationForTest used outside tests: %v", bad)
	}
}
