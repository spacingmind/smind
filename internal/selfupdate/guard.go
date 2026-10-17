package selfupdate

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// Refusal is a deliberate "not updating" outcome (a guard, not a failure):
// one-line reason, nothing changed on disk.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func refuse(format string, a ...any) error { return &Refusal{Reason: fmt.Sprintf(format, a...)} }

// DesktopOwned reports why exe belongs to the desktop app rather than to the
// user: inside the app bundle (macOS: the bundled sidecar under
// `*.app/Contents/`), inside the managed-daemon directory (macOS
// `.../managed-daemon/bin/smind`, Windows `%LOCALAPPDATA%\smind\bin\`), or next
// to the app's own executable (Windows bundles `smind.exe` beside the app's `smind-desktop.exe`, Tauri's ${MAINBINARYNAME}).
// The app updates that daemon itself (ADR-0013/0030); replacing it under the
// app would desync its recorded identity. goos selects path semantics so the
// Windows rules are unit-testable anywhere.
func DesktopOwned(exe, goos string, getenv func(string) string, exists func(string) bool) (string, bool) {
	slash := strings.ReplaceAll(exe, "\\", "/")
	norm := strings.ToLower(slash)
	if strings.Contains(norm, ".app/contents/") {
		return "the running smind is inside the desktop app bundle; the app updates its own daemon", true
	}
	for _, el := range strings.Split(norm, "/") {
		if el == "managed-daemon" {
			return "the running smind is the desktop app's managed daemon; the app updates it", true
		}
	}
	if goos == "windows" {
		if local := getenv("LOCALAPPDATA"); local != "" {
			managed := path.Join(strings.ToLower(strings.ReplaceAll(local, "\\", "/")), "smind", "bin")
			if path.Dir(norm) == managed {
				return "the running smind.exe is in the desktop app's managed-daemon directory; the app updates it", true
			}
		}
		// The app's executable is its Cargo name, NOT "sMind.exe": that differs from the
		// sidecar "smind.exe" only by case, so on NTFS (case-insensitive) it "exists"
		// next to every smind.exe and every standalone install was refused.
		if exists(path.Join(path.Dir(slash), "smind-desktop.exe")) {
			return "the running smind.exe sits next to the sMind desktop app; the app updates its own daemon", true
		}
	}
	return "", false
}

// InContainer reports whether this process runs inside a container image,
// where the right update is pulling a new image (ADR-0031 D5).
func InContainer(goos string, getenv func(string) string, exists func(string) bool) bool {
	if goos != "linux" {
		return false
	}
	if getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	return exists("/.dockerenv") || exists("/run/.containerenv")
}

// CheckWritable proves the new binary can be created next to exe by creating
// and removing a probe file there, and names the cause when it cannot. smind
// never elevates itself.
func CheckWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".smind.write-check.*")
	if err != nil {
		return refuse("%s is not writable (%v); re-run as a user who owns it, or reinstall with SMIND_INSTALL_DIR set to a directory you own (smind never elevates itself)", dir, unwrapPathErr(err))
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

func unwrapPathErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}
