package permpolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

type fakeEntry struct {
	kind   string // "dir", "file", "link" or "junction"
	target string
}

// fakeFS is an in-memory Windows filesystem for the path layer. Keys are canonical-case paths; lookups ignore case.
type fakeFS struct{ entries map[string]fakeEntry }

type fakeInfo struct {
	name string
	mode fs.FileMode
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return i.mode }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fakeInfo) Sys() any           { return nil }

func newWin(entries map[string]fakeEntry, env map[string]string) *platform {
	return windowsPlatform(&fakeFS{entries: entries}, env)
}

func fkey(p string) string { return strings.ToLower(strings.TrimRight(p, `\`)) }

// find returns the canonical key and entry for p.
func (f *fakeFS) find(p string) (string, fakeEntry, bool) {
	want := fkey(p)
	for k, e := range f.entries {
		if fkey(k) == want {
			return k, e, true
		}
	}
	// a parent of some entry exists implicitly as a directory
	for k := range f.entries {
		if lk := strings.ToLower(k); len(lk) > len(want) && strings.HasPrefix(lk, want+`\`) {
			return k[:len(want)], fakeEntry{kind: "dir"}, true
		}
	}
	return "", fakeEntry{}, false
}

// canonVol is the stored-case volume when some entry lives under it.
func (f *fakeFS) canonVol(vol string) (string, bool) {
	lv := strings.ToLower(vol)
	for k := range f.entries {
		lk := strings.ToLower(k)
		if lk == lv || strings.HasPrefix(lk, lv+`\`) {
			return k[:len(vol)], true
		}
	}
	return "", false
}

func notExist(p string) error {
	return &fs.PathError{Op: "realpath", Path: p, Err: fs.ErrNotExist}
}

func (f *fakeFS) Resolve(p string) (string, error) {
	cur := p
	for hops := 0; ; hops++ {
		if hops > 40 {
			return "", fmt.Errorf("realpath %s: too many links", p)
		}
		vol, _, comps := winSplit(strings.ReplaceAll(cur, "/", `\`))
		cv, ok := f.canonVol(vol)
		if !ok {
			return "", notExist(p)
		}
		var done []string
		restarted := false
		for i, c := range comps {
			k, e, ok := f.find(winBuild(cv, true, append(append([]string{}, done...), c)))
			if !ok {
				return "", notExist(p)
			}
			if e.kind == "link" || e.kind == "junction" {
				cur = e.target
				if rest := comps[i+1:]; len(rest) > 0 {
					cur = strings.TrimRight(cur, `\`) + `\` + strings.Join(rest, `\`)
				}
				restarted = true
				break
			}
			_, _, kc := winSplit(k)
			done = append(done, kc[len(kc)-1])
		}
		if !restarted {
			return winBuild(cv, true, done), nil
		}
	}
}

func (f *fakeFS) Lstat(p string) (fs.FileInfo, error) {
	p = strings.ReplaceAll(p, "/", `\`)
	k, e, ok := f.find(p)
	if !ok {
		if vol, _, comps := winSplit(p); len(comps) == 0 {
			if _, ok := f.canonVol(vol); ok {
				return fakeInfo{name: vol, mode: os.ModeDir}, nil
			}
		}
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
	}
	info := fakeInfo{name: k}
	switch e.kind {
	case "link":
		info.mode = os.ModeSymlink
	case "junction":
		info.mode = os.ModeIrregular
	case "dir":
		info.mode = os.ModeDir
	}
	return info, nil
}

func (f *fakeFS) Readlink(p string) (string, error) {
	_, e, ok := f.find(strings.ReplaceAll(p, "/", `\`))
	if !ok {
		return "", &fs.PathError{Op: "readlink", Path: p, Err: fs.ErrNotExist}
	}
	if e.kind != "link" && e.kind != "junction" {
		return "", &fs.PathError{Op: "readlink", Path: p, Err: errors.New("not a link")}
	}
	return e.target, nil
}
