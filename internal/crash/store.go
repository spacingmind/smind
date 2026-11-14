package crash

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fsutil"
)

const (
	crashPrefix   = "crash-"
	crashSuffix   = ".json"
	tmpPrefix     = ".tmp-crash-"
	nameLayout    = "20060102T150405.000"
	siteMaxRunes  = 40
	tmpMaxAge     = time.Minute
	maxReportRead = 1 << 20 // doctor reads at most this much of a report
	dirMode       = 0o700
	fileMode      = 0o600
)

// store creates the directory, writes data to a temp file in it and renames
// the file into place, then rotates. It is called with r.mu held.
func (r *Recorder) store(data []byte, now time.Time, site string) (string, error) {
	dir := r.cfg.Dir
	// Look before touching: MkdirAll and Chmod both follow a link, so the
	// link check must come first or a planted link would get its target
	// chmod'ed (and written into).
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return "", err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return "", err
	}
	if fsutil.IsLink(fi.Mode()) {
		return "", errLinkDir
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("crash directory %s is not a directory", dir)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, dirMode); err != nil {
			return "", err
		}
	}
	// Two different panics at the same site in the same millisecond must not
	// overwrite each other: take the first free name.
	final := filepath.Join(dir, crashName(now, r.cfg.Source, site, 0))
	for n := 1; n < 100; n++ {
		if _, err := os.Lstat(final); os.IsNotExist(err) {
			break
		}
		final = filepath.Join(dir, crashName(now, r.cfg.Source, site, n))
	}
	r.tmpSeq++
	tmp := filepath.Join(dir, fmt.Sprintf("%s%d-%d", tmpPrefix, os.Getpid(), r.tmpSeq))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", err
	}
	r.rotate(now)
	return final, nil
}

// rotate keeps the newest cfg.MaxFiles reports, caps the directory at
// cfg.MaxDirBytes (oldest dropped first) and removes leftover temp files
// older than this call.
func (r *Recorder) rotate(now time.Time) {
	entries, err := os.ReadDir(r.cfg.Dir)
	if err != nil {
		return
	}
	var names []string
	sizes := make(map[string]int64)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		n := e.Name()
		// Another process (the CLI) may share this directory: only a temp file
		// that has been lying around is a leftover.
		if strings.HasPrefix(n, tmpPrefix) && info.ModTime().Before(now.Add(-tmpMaxAge)) {
			os.Remove(filepath.Join(r.cfg.Dir, n))
			continue
		}
		if isCrashName(n) {
			names = append(names, n)
			sizes[n] = info.Size()
		}
	}
	sort.Strings(names) // the name timestamp orders the files
	if len(names) > r.cfg.MaxFiles {
		for _, n := range names[:len(names)-r.cfg.MaxFiles] {
			os.Remove(filepath.Join(r.cfg.Dir, n))
			delete(sizes, n)
		}
		names = names[len(names)-r.cfg.MaxFiles:]
	}
	var total int64
	for _, n := range names {
		total += sizes[n]
	}
	for _, n := range names {
		if total <= r.cfg.MaxDirBytes {
			break
		}
		total -= sizes[n]
		os.Remove(filepath.Join(r.cfg.Dir, n))
	}
}

func isCrashName(name string) bool {
	return strings.HasPrefix(name, crashPrefix) && strings.HasSuffix(name, crashSuffix)
}

func crashName(now time.Time, source, site string, n int) string {
	suffix := ""
	if n > 0 {
		suffix = fmt.Sprintf("-%d", n)
	}
	return fmt.Sprintf("%s%s-%s-%s%s%s", crashPrefix, now.UTC().Format(nameLayout), source, site, suffix, crashSuffix)
}

func sanitizeSite(site string) string {
	site = strings.ToLower(site)
	var b strings.Builder
	for _, c := range site {
		if b.Len() >= siteMaxRunes {
			break
		}
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// nameTimestamp extracts the UTC timestamp from a crash file name.
func nameTimestamp(name string) (time.Time, bool) {
	rest := strings.TrimSuffix(strings.TrimPrefix(name, crashPrefix), crashSuffix)
	if i := strings.Index(rest, "-"); i >= 0 {
		rest = rest[:i]
	}
	t, err := time.Parse(nameLayout, rest)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Summary describes a crashes directory without changing it.
type Summary struct {
	Dir          string
	Count        int
	Newest       time.Time
	NewestSite   string
	NewestSource string
	Refused      bool
	Err          string
}

// List summarises the reports in dir. It is read-only: a missing directory
// is Count 0 with no error, a linked directory is refused, and no directory
// or file is ever created.
func List(dir string) Summary {
	s := Summary{Dir: dir}
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		s.Err = err.Error()
		return s
	}
	if fsutil.IsLink(fi.Mode()) {
		s.Refused = true
		return s
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.Err = err.Error()
		return s
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isCrashName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	s.Count = len(names)
	if len(names) == 0 {
		return s
	}
	sort.Strings(names)
	newest := names[len(names)-1]
	if t, ok := nameTimestamp(newest); ok {
		s.Newest = t
	}
	data, err := readNewest(filepath.Join(dir, newest))
	if err != nil {
		s.Err = err.Error()
		return s
	}
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		s.Err = err.Error()
		return s
	}
	s.NewestSite, s.NewestSource = rep.Site, rep.Source
	if t, err := time.Parse(time.RFC3339Nano, rep.Time); err == nil {
		s.Newest = t
	}
	return s
}

// readNewest reads at most maxReportRead bytes of a regular file. A planted
// FIFO (which would block `smind doctor` forever) or link is refused: the
// check is on Lstat, and the open follows nothing it has not checked.
func readNewest(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxReportRead))
}

// DesktopDir returns the crashes directory of the desktop shell under the
// platform's app-data location, or "" when the home cannot be found.
func DesktopDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "Application Support", "dev.spacingmind.desktop", "crashes")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return ""
		}
		return filepath.Join(appData, "dev.spacingmind.desktop", "crashes")
	default:
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "dev.spacingmind.desktop", "crashes")
	}
}
