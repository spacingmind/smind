package scratch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTempEnv_Unix_Windows(t *testing.T) {
	if got, want := TempEnv("/tmp/x", "linux"), []string{"TMPDIR=/tmp/x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TempEnv(unix) = %v, want %v", got, want)
	}
	want := []string{"TMPDIR=C:\\t", "TMP=C:\\t", "TEMP=C:\\t"}
	if got := TempEnv("C:\\t", "windows"); !reflect.DeepEqual(got, want) {
		t.Fatalf("TempEnv(windows) = %v, want %v", got, want)
	}
}

func TestResolveSharedCaches_NoGo(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := ResolveSharedCaches(context.Background(), home,
		func(string) (string, error) { return "", os.ErrNotExist },
		func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("go must not be run when lookPath fails")
			return nil, nil
		},
		func(string) string { return "" },
	)
	if _, ok := c["GOMODCACHE"]; ok {
		t.Fatal("GOMODCACHE set without go on PATH")
	}
	want := filepath.Join(home, ".npm")
	if got, ok := c["npm_config_cache"]; !ok || got != want {
		t.Fatalf("npm_config_cache = %q, %v; want %q", got, ok, want)
	}
	if _, ok := c["BUN_INSTALL_CACHE_DIR"]; ok {
		t.Fatal("BUN_INSTALL_CACHE_DIR set without ~/.bun/install/cache")
	}
}

func TestResolveSharedCaches_FromGoEnv(t *testing.T) {
	c := ResolveSharedCaches(context.Background(), "",
		func(string) (string, error) { return "/usr/local/go/bin/go", nil },
		func(context.Context, string, ...string) ([]byte, error) {
			return []byte("/m\n/c\n"), nil
		},
		func(string) string { return "" },
	)
	if c["GOMODCACHE"] != "/m" {
		t.Fatalf("GOMODCACHE = %q, want /m", c["GOMODCACHE"])
	}
	if c["GOCACHE"] != "/c" {
		t.Fatalf("GOCACHE = %q, want /c", c["GOCACHE"])
	}
	if len(c) != 2 {
		t.Fatalf("caches = %v, want only the two go env entries", c)
	}
}

func TestResolveSharedCaches_DoesNotOverrideExisting(t *testing.T) {
	c := ResolveSharedCaches(context.Background(), "",
		func(string) (string, error) { return "/usr/local/go/bin/go", nil },
		func(context.Context, string, ...string) ([]byte, error) {
			return []byte("/m\n/c\n"), nil
		},
		func(name string) string {
			if name == "GOCACHE" {
				return "x"
			}
			return ""
		},
	)
	if c["GOMODCACHE"] != "/m" {
		t.Fatalf("GOMODCACHE = %q, want /m", c["GOMODCACHE"])
	}
	if _, ok := c["GOCACHE"]; ok {
		t.Fatalf("GOCACHE = %q, want unset (already present in env)", c["GOCACHE"])
	}
}

func TestSharedCaches_Entries_Sorted(t *testing.T) {
	c := SharedCaches{
		"npm_config_cache":      "/n",
		"BUN_INSTALL_CACHE_DIR": "/b",
		"GOCACHE":               "/c",
	}
	want := []string{"BUN_INSTALL_CACHE_DIR=/b", "GOCACHE=/c", "npm_config_cache=/n"}
	if got := c.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries() = %v, want %v", got, want)
	}
	if got := SharedCaches(nil).Entries(); got != nil {
		t.Fatalf("Entries() on empty = %v, want nil", got)
	}
}
