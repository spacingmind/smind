package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrUnavailable is returned (wrapped in *UnavailableError) when no system
// browser can be found. The browser is a system dependency: smind never
// installs or downloads one.
var ErrUnavailable = errors.New("browser: no browser installed")

// UnavailableError carries the exact step the person can take.
type UnavailableError struct{ InstallStep string }

func (e *UnavailableError) Error() string {
	return "no browser installed. " + e.InstallStep
}

func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

type detector struct {
	goos     string
	lookPath func(string) (string, error)
	isFile   func(string) bool
	isExec   func(string) bool
	getenv   func(string) string
}

func systemDetector() detector {
	return detector{
		goos:     runtime.GOOS,
		lookPath: exec.LookPath,
		getenv:   os.Getenv,
		isFile: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && st.Mode().IsRegular()
		},
		isExec: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && st.Mode().Perm()&0o111 != 0
		},
	}
}

// DetectChrome returns the path of a system Chrome, Chromium or Edge: the
// explicit path if one is configured (it must exist, there is no silent
// fallback from a bad configuration), else the standard install locations,
// else well-known names on PATH. It runs nothing and fetches nothing.
func DetectChrome(explicit string) (string, error) {
	return systemDetector().detect(explicit)
}

func (d detector) detect(explicit string) (string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) || !d.isFile(explicit) || (d.goos != "windows" && !d.isExec(explicit)) {
			return "", &UnavailableError{InstallStep: fmt.Sprintf("The configured browser %q is not an absolute path to an executable file. Fix the path or clear the setting.", explicit)}
		}
		return explicit, nil
	}
	for _, p := range d.locations() {
		if d.isFile(p) {
			return p, nil
		}
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge", "microsoft-edge-stable"} {
		if p, err := d.lookPath(n); err == nil && p != "" {
			return p, nil
		}
	}
	return "", &UnavailableError{InstallStep: installStep(d.goos)}
}

func (d detector) locations() []string {
	switch d.goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			base := d.getenv(env)
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "Chromium", "Application", "chrome.exe"),
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
		return out
	default:
		return []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium", "/usr/bin/microsoft-edge"}
	}
}

func installStep(goos string) string {
	switch goos {
	case "darwin":
		return "Install Google Chrome or Chromium (for example `brew install --cask google-chrome`), then restart the daemon."
	case "windows":
		return "Install Google Chrome or Microsoft Edge, then restart the daemon."
	default:
		return "Install Chromium or Google Chrome with your package manager (for example `apt install chromium`), then restart the daemon."
	}
}
