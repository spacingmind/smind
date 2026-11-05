// Package runstats measures the payload distribution of a smind database for
// ADR-0053 (skeleton run storage). It is read-only by construction and prints
// only counts, sizes and percentiles: never event text, prompts or run ids.
package runstats

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/spacingmind/smind/internal/config"
)

// Open opens exactly the database file at path, read-only and immutable. It
// refuses anything that could be a live database: a query string or file: URI,
// the live smind.db of this home (or of the real ~/.smind), any file directly
// in a smind home, and a database with a non-empty write-ahead log next to it
// (a snapshot taken with VACUUM INTO has none). The measurement is meant for a
// copy; it must never be able to lock or modify the daemon's file.
func Open(path string) (*sql.DB, error) {
	abs, err := checkPath(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Path: filepath.ToSlash(abs)}
	dsn := "file:" + u.EscapedPath() + "?mode=ro&immutable=1&_pragma=query_only(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(abs), err)
	}
	// One connection: a single read transaction over the copy.
	db.SetMaxOpenConns(1)
	return db, nil
}

func checkPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("run-stats: --db <path> is required")
	}
	if strings.ContainsAny(path, "?#") || strings.HasPrefix(path, "file:") || strings.Contains(path, ":memory:") {
		return "", errors.New("run-stats: --db must be a plain file path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("run-stats: %w", err)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("run-stats: %w", err)
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("run-stats: --db is not a regular file")
	}
	for _, home := range liveHomes() {
		if filepath.Dir(resolved) == home {
			return "", errors.New("run-stats: refusing a file inside a smind home; copy it elsewhere first (VACUUM INTO)")
		}
		if live, err := os.Stat(filepath.Join(home, "smind.db")); err == nil && os.SameFile(st, live) {
			return "", errors.New("run-stats: refusing the live smind.db; measure a copy")
		}
	}
	if wal, err := os.Stat(resolved + "-wal"); err == nil && wal.Size() > 0 {
		return "", errors.New("run-stats: the database has a non-empty -wal file (not a clean snapshot); refusing")
	}
	return resolved, nil
}

// liveHomes lists the resolved smind home directories that must not be read:
// the one in effect and the real ~/.smind.
func liveHomes() []string {
	cands := []string{config.Dir()}
	if h, err := os.UserHomeDir(); err == nil {
		cands = append(cands, config.DirFor(h))
	}
	var out []string
	for _, c := range cands {
		if abs, err := filepath.Abs(c); err == nil {
			if r, err := filepath.EvalSymlinks(abs); err == nil {
				abs = r
			}
			out = append(out, abs)
		}
	}
	return out
}
