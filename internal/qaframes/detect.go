package qaframes

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Env is everything detection reads from the machine, injectable for tests.
type Env struct {
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
	Glob     func(string) ([]string, error)
	Getenv   func(string) string
	Home     string
	RepoRoot string
	GOOS     string
}

// OSEnv is the real machine.
func OSEnv(repoRoot string) Env {
	home, _ := os.UserHomeDir()
	return Env{LookPath: exec.LookPath, Stat: os.Stat, Glob: filepath.Glob, Getenv: os.Getenv, Home: home, RepoRoot: repoRoot, GOOS: runtime.GOOS}
}

// Browser is a usable node + playwright + chromium triple.
type Browser struct {
	Node          string `json:"node"`
	PlaywrightDir string `json:"playwrightDir"` // the node_modules directory holding the module
	PlaywrightPkg string `json:"playwrightPkg"` // playwright-core or playwright
	Chrome        string `json:"chrome"`
}

// NoBrowserError is the typed "cannot drive a browser here" result. Missing
// names every absent piece so the report says what to install.
type NoBrowserError struct{ Missing []string }

func (e *NoBrowserError) Error() string { return "no browser: " + strings.Join(e.Missing, "; ") }

// ErrNoBrowser matches any *NoBrowserError with errors.Is.
var ErrNoBrowser = errors.New("no browser")

func (e *NoBrowserError) Is(t error) bool { return t == ErrNoBrowser }

// DetectBrowser finds node, a playwright module and a Chromium-family
// executable. It never installs anything and never touches the network.
// override* are the flag values ("" = unset).
func DetectBrowser(env Env, overrideChrome, overridePWDir string) (Browser, error) {
	var b Browser
	var missing []string
	if p, err := env.LookPath("node"); err == nil {
		b.Node = p
	} else {
		missing = append(missing, "node is not on PATH")
	}
	pwDirs := []string{overridePWDir, env.Getenv("SMIND_QA_PLAYWRIGHT_DIR")}
	if env.RepoRoot != "" {
		pwDirs = append(pwDirs, filepath.Join(env.RepoRoot, "web", "node_modules"), filepath.Join(env.RepoRoot, "node_modules"))
	}
	for _, d := range pwDirs {
		if d == "" {
			continue
		}
		for _, pkg := range []string{"playwright-core", "playwright"} {
			if st, err := env.Stat(filepath.Join(d, pkg, "package.json")); err == nil && !st.IsDir() {
				b.PlaywrightDir, b.PlaywrightPkg = d, pkg
				break
			}
		}
		if b.PlaywrightDir != "" {
			break
		}
	}
	if b.PlaywrightDir == "" {
		missing = append(missing, "no playwright-core/playwright module (set --playwright-dir or SMIND_QA_PLAYWRIGHT_DIR to a node_modules that has it)")
	}
	b.Chrome = findChrome(env, overrideChrome)
	if b.Chrome == "" {
		missing = append(missing, "no Chromium/Chrome executable (set --chrome or SMIND_QA_CHROME, or install a Playwright chromium)")
	}
	if len(missing) > 0 {
		return Browser{}, &NoBrowserError{Missing: missing}
	}
	return b, nil
}

func findChrome(env Env, override string) string {
	var cands []string
	cands = append(cands, override, env.Getenv("SMIND_QA_CHROME"))
	exists := func(p string) bool {
		st, err := env.Stat(p)
		return err == nil && !st.IsDir()
	}
	cache := ""
	switch env.GOOS {
	case "darwin":
		cache = filepath.Join(env.Home, "Library", "Caches", "ms-playwright")
	case "windows":
		cache = filepath.Join(env.Home, "AppData", "Local", "ms-playwright")
	default:
		cache = filepath.Join(env.Home, ".cache", "ms-playwright")
	}
	if d := env.Getenv("PLAYWRIGHT_BROWSERS_PATH"); d != "" {
		cache = d
	}
	for _, pat := range []string{
		"chromium-*/chrome-mac/Chromium.app/Contents/MacOS/Chromium",
		"chromium-*/chrome-mac-arm64/Chromium.app/Contents/MacOS/Chromium",
		"chromium-*/chrome-linux/chrome",
		"chromium-*/chrome-linux64/chrome",
		"chromium-*/chrome-win/chrome.exe",
		"chromium_headless_shell-*/chrome-linux/headless_shell",
		"chromium_headless_shell-*/chrome-mac/headless_shell",
	} {
		if m, err := env.Glob(filepath.Join(cache, pat)); err == nil {
			for i := len(m) - 1; i >= 0; i-- { // newest revision last in lexical order
				cands = append(cands, m[i])
			}
		}
	}
	if env.GOOS == "darwin" {
		cands = append(cands, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := env.LookPath(name); err == nil {
			cands = append(cands, p)
		}
	}
	for _, c := range cands {
		if c != "" && exists(c) {
			return c
		}
	}
	return ""
}

// String is a one-line description for reports.
func (b Browser) String() string {
	return fmt.Sprintf("node=%s %s=%s chrome=%s", b.Node, b.PlaywrightPkg, b.PlaywrightDir, b.Chrome)
}
