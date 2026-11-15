package browser

import (
	"errors"
	"strings"
	"testing"
)

func fakeDetector(goos string, files map[string]bool, path map[string]string) detector {
	return detector{
		goos:   goos,
		isFile: func(p string) bool { return files[p] },
		lookPath: func(n string) (string, error) {
			if p, ok := path[n]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		getenv: func(k string) string {
			if k == "ProgramFiles" {
				return `C:\Program Files`
			}
			return ""
		},
	}
}

func TestDetectChrome(t *testing.T) {
	const mac = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	d := fakeDetector("darwin", map[string]bool{mac: true, "/custom/chrome": true}, nil)
	if p, err := d.detect(""); err != nil || p != mac {
		t.Fatalf("standard location: %q %v", p, err)
	}
	if p, err := d.detect("/custom/chrome"); err != nil || p != "/custom/chrome" {
		t.Fatalf("explicit path: %q %v", p, err)
	}
	if _, err := d.detect("/missing/chrome"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a bad explicit path must not fall back silently: %v", err)
	}
	l := fakeDetector("linux", nil, map[string]string{"chromium-browser": "/opt/bin/chromium-browser"})
	if p, err := l.detect(""); err != nil || p != "/opt/bin/chromium-browser" {
		t.Fatalf("PATH name: %q %v", p, err)
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		_, err := fakeDetector(goos, nil, nil).detect("")
		var ue *UnavailableError
		if !errors.Is(err, ErrUnavailable) || !errors.As(err, &ue) || !strings.Contains(ue.InstallStep, "Install") {
			t.Fatalf("%s: none found must be ErrUnavailable with the install step, got %v", goos, err)
		}
	}
	if len(fakeDetector("windows", nil, nil).locations()) == 0 {
		t.Fatal("windows must list locations from ProgramFiles")
	}
}
