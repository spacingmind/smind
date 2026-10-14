package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/pairing"
)

// offerDir returns a SMIND_HOME-style dir holding a relay connection whose
// host id is hostIDLen bytes long (the offer's size is driven by it).
func offerDir(t *testing.T, hostIDLen int) string {
	t.Helper()
	dir := t.TempDir()
	err := bridge.SaveConfig(dir, bridge.Config{
		RelayAddress: "relay.example.test:7400",
		WorkspaceID:  strings.Repeat("h", hostIDLen),
		SecretHex:    strings.Repeat("ab", 32),
		Fingerprint:  strings.Repeat("cd", 32),
	})
	if err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return dir
}

func offer(t *testing.T, dir string, tty bool, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runRelayOffer(args, dir, &out, &errOut, tty)
	return code, out.String(), errOut.String()
}

// urlOnly asserts stdout is exactly one pairing URL line and returns it.
func urlOnly(t *testing.T, stdout string) string {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout = %q, want exactly one line", stdout)
	}
	url := strings.TrimSuffix(stdout, "\n")
	if _, err := pairing.ParseURL(url); err != nil {
		t.Fatalf("stdout %q is not a pairing URL: %v", url, err)
	}
	return url
}

func TestRelayOfferURLOnlyOnStdout(t *testing.T) {
	dir := offerDir(t, 12)
	code, stdout, stderr := offer(t, dir, false)
	if code != 0 || stderr != "" {
		t.Fatalf("non-TTY offer = %d, stderr %q; want 0 and silent stderr", code, stderr)
	}
	url := urlOnly(t, stdout)

	// A TTY and --qr add the QR on stderr, never on stdout.
	for name, args := range map[string][]string{"tty": nil, "qr flag": {"--qr"}} {
		code, out2, errOut := offer(t, dir, name == "tty", args...)
		if code != 0 || out2 != stdout {
			t.Fatalf("%s: stdout = %q (code %d), want the same single URL line", name, out2, code)
		}
		want, err := pairing.EncodeQRFit(url)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, want.Text()) {
			t.Errorf("%s: stderr lacks the QR for the offer URL:\n%s", name, errOut)
		}
		if !strings.Contains(errOut, "mobile device or the desktop app") {
			t.Errorf("%s: stderr lacks the device wording:\n%s", name, errOut)
		}
	}
}

func TestRelayOfferQRFallsBackToLevelL(t *testing.T) {
	// Grow the host id until the offer URL reaches the 432-byte realistic
	// size (ADR-0031): too big for level M (412 bytes), fine at level L.
	var dir, url string
	for n := 8; n < 200; n++ {
		dir = offerDir(t, n)
		_, stdout, _ := offer(t, dir, false)
		if url = strings.TrimSpace(stdout); len(url) >= 432 {
			break
		}
	}
	if len(url) < 432 || len(url) > 440 {
		t.Fatalf("test setup: offer URL is %d bytes, want ~432", len(url))
	}
	if _, err := pairing.EncodeQR(url, pairing.ECMedium); err == nil {
		t.Fatal("test setup: offer fits at level M, so it would not exercise the fallback")
	}
	code, stdout, stderr := offer(t, dir, false, "--qr")
	if code != 0 || strings.TrimSpace(stdout) != url {
		t.Fatalf("offer = %d, stdout %q", code, stdout)
	}
	want, err := pairing.EncodeQRFit(url)
	if err != nil || want.Level() != pairing.ECLow {
		t.Fatalf("EncodeQRFit level = %v, err %v; want L", want.Level(), err)
	}
	if !strings.Contains(stderr, want.Text()) {
		t.Fatalf("stderr lacks the level-L QR:\n%s", stderr)
	}
}

func TestRelayOfferTooLongPrintsNoteAndExitsZero(t *testing.T) {
	dir := offerDir(t, 600)
	png := filepath.Join(t.TempDir(), "offer.png")
	code, stdout, stderr := offer(t, dir, true, "--png", png)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr %q", code, stderr)
	}
	urlOnly(t, stdout)
	if !strings.Contains(stderr, "too long for a QR code") {
		t.Fatalf("stderr lacks the too-long note:\n%s", stderr)
	}
	if _, err := os.Stat(png); !os.IsNotExist(err) {
		t.Fatalf("an unrenderable offer must not write a PNG (stat err = %v)", err)
	}
}

func TestRelayOfferPNGMode0600AndNoOverwrite(t *testing.T) {
	dir := offerDir(t, 12)
	path := filepath.Join(t.TempDir(), "offer.png")

	code, stdout, stderr := offer(t, dir, false, "--png", path)
	if code != 0 {
		t.Fatalf("--png = %d, stderr %q", code, stderr)
	}
	url := urlOnly(t, stdout)
	if strings.Contains(stderr, "\u2580") || strings.Contains(stderr, "\u2584") || strings.Contains(stderr, "\u2588") {
		t.Errorf("--png without a TTY or --qr must not also draw the terminal QR:\n%s", stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	want, _ := pairing.EncodeQRFit(url)
	if side := (want.Size() + 8) * offerPNGModuleSize; img.Bounds().Dx() != side {
		t.Errorf("PNG width = %d, want %d", img.Bounds().Dx(), side)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("PNG mode = %v, want 0600", info.Mode().Perm())
		}
	}

	// Refuses to overwrite; the file is untouched and the exit is non-zero.
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = offer(t, dir, false, "--png", path)
	if code == 0 || !strings.Contains(stderr, "--force") {
		t.Fatalf("overwrite without --force = %d, stderr %q; want failure naming --force", code, stderr)
	}
	if got, _ := os.ReadFile(path); string(got) != "keep" {
		t.Fatalf("file was modified without --force: %q", got)
	}
	// --force overwrites and tightens a loose existing file.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code, _, stderr = offer(t, dir, false, "--png", path, "--force"); code != 0 {
		t.Fatalf("--force = %d, stderr %q", code, stderr)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("--force did not rewrite the PNG")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("PNG mode after --force = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestRelayOfferWording(t *testing.T) {
	_, _, stderr := offer(t, offerDir(t, 12), true)
	for _, want := range []string{
		"mobile device or the desktop app",
		"Treat this link like a password",
		"smind relay host rotate",
		"end-to-end encrypted; the relay cannot read your code or messages",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("offer output lacks %q:\n%s", want, stderr)
		}
	}
	var help bytes.Buffer
	printRelayUsage(&help)
	flat := strings.Join(strings.Fields(help.String()), " ") // help wraps lines
	for _, want := range []string{"mobile device or the desktop app", "Treat the link like a password", "smind relay host rotate", "end-to-end encrypted; the relay cannot read your code or messages"} {
		if !strings.Contains(flat, want) {
			t.Errorf("relay help lacks %q", want)
		}
	}
	if !strings.Contains(relayLinkWarning, "Treat this link like a password") {
		t.Error("connect's closing message must carry the warning")
	}
}

func TestRelayOfferWithoutConnectionFails(t *testing.T) {
	code, stdout, stderr := offer(t, t.TempDir(), false)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "relay connect") {
		t.Fatalf("offer with no connection = %d %q %q", code, stdout, stderr)
	}
}

func TestRelayOfferRejectsBadFlags(t *testing.T) {
	dir := offerDir(t, 12)
	for _, args := range [][]string{{"extra"}, {"--force"}, {"--nope"}} {
		if code, stdout, _ := offer(t, dir, false, args...); code != 2 || stdout != "" {
			t.Errorf("offer %v = %d %q, want exit 2 and no stdout", args, code, stdout)
		}
	}
}

// The daemon key persists in dir, so two offers from one home are identical.
func TestRelayOfferIsStableAcrossRuns(t *testing.T) {
	dir := offerDir(t, 12)
	_, a, _ := offer(t, dir, false)
	_, b, _ := offer(t, dir, false)
	if a != b {
		t.Fatal("two offers from one home differ")
	}
}
