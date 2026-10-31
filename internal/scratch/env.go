package scratch

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TempEnv returns env entries pointing temp dirs at dir: TMPDIR, plus TMP
// and TEMP on Windows. Callers pass runtime.GOOS.
func TempEnv(dir, goos string) []string {
	env := []string{"TMPDIR=" + dir}
	if goos == "windows" {
		env = append(env, "TMP="+dir, "TEMP="+dir)
	}
	return env
}

// SharedCaches maps env var names (GOMODCACHE, GOCACHE,
// npm_config_cache, BUN_INSTALL_CACHE_DIR) to shared cache dirs, reused
// across per-task scratch dirs so builds don't redownload the world.
type SharedCaches map[string]string

// ResolveSharedCaches discovers the shared caches for home: the go module
// and build caches via `go env` (only for vars the environment doesn't
// already set), and the npm/bun caches when their conventional dirs
// exist. Failures are skipped silently: shared caches are an
// optimization, never a requirement.
func ResolveSharedCaches(ctx context.Context, home string, lookPath func(string) (string, error), run func(ctx context.Context, name string, args ...string) ([]byte, error), getenv func(string) string) SharedCaches {
	caches := SharedCaches{}
	if _, err := lookPath("go"); err == nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := run(ctx, "go", "env", "GOMODCACHE", "GOCACHE"); err == nil {
			setGoEnv(caches, string(out), getenv)
		}
	}
	if home != "" {
		addExistingDir(caches, "npm_config_cache", filepath.Join(home, ".npm"), getenv)
		addExistingDir(caches, "BUN_INSTALL_CACHE_DIR", filepath.Join(home, ".bun", "install", "cache"), getenv)
	}
	return caches
}

// setGoEnv reads `go env GOMODCACHE GOCACHE` output (one value per line,
// in argument order) into caches, never overriding an already-set var.
func setGoEnv(caches SharedCaches, out string, getenv func(string) string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i, name := range []string{"GOMODCACHE", "GOCACHE"} {
		if i >= len(lines) {
			return
		}
		if value := strings.TrimSpace(lines[i]); value != "" && getenv(name) == "" {
			caches[name] = value
		}
	}
}

func addExistingDir(caches SharedCaches, name, dir string, getenv func(string) string) {
	if getenv(name) != "" {
		return
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		caches[name] = dir
	}
}

// Entries returns the caches as sorted NAME=VALUE env entries; nil when
// empty.
func (c SharedCaches) Entries() []string {
	if len(c) == 0 {
		return nil
	}
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, name+"="+c[name])
	}
	return entries
}
