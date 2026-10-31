package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/securefile"
)

// helloVector mirrors the ADR-0031 D9 device hello test vector's identity
// fields; it pins HostID to a published id instead of self-agreement.
type helloVector struct {
	SeedHex      string `json:"seed_hex"`
	DevicePubHex string `json:"device_pub_hex"`
	DeviceID     string `json:"device_id"`
}

func readVector(t *testing.T) helloVector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "relay", "devices", "testdata", "device_hello_vector.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v helloVector
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestHostIDMatchesDeviceIDVector proves the fleet host id is the same
// derivation as the relay's device id, on the published vector and on a
// second key, and that the encoding is the 26-char lowercase base32 form.
func TestHostIDMatchesDeviceIDVector(t *testing.T) {
	v := readVector(t)
	priv := ed25519.NewKeyFromSeed(mustHex(t, v.SeedHex))
	pub := priv.Public().(ed25519.PublicKey)
	if got := hex.EncodeToString(pub); got != v.DevicePubHex {
		t.Fatalf("seed does not derive the vector's public key: %s", got)
	}

	id := HostID(pub)
	if id != v.DeviceID {
		t.Errorf("HostID(pub) = %q, vector device_id = %q", id, v.DeviceID)
	}
	if id != devices.DeviceID(pub) {
		t.Errorf("HostID(pub) = %q, devices.DeviceID(pub) = %q", id, devices.DeviceID(pub))
	}
	if len(id) != 26 {
		t.Errorf("len(HostID) = %d, want 26", len(id))
	}
	for _, c := range id {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", c) {
			t.Errorf("HostID %q contains %q outside base32 [a-z2-7]", id, c)
		}
	}

	// A second fixed key (bytes 0..31) rather than a random one, so a
	// regression is reproducible from the failure message alone.
	second := ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))
	for i := range second {
		second[i] = byte(i)
	}
	if HostID(second) != devices.DeviceID(second) {
		t.Errorf("HostID(second) = %q, devices.DeviceID(second) = %q", HostID(second), devices.DeviceID(second))
	}
}

func keyPath(dir string) string { return filepath.Join(dir, KeyFile) }

func TestHostKeyFileModeAndCorruption(t *testing.T) {
	t.Run("created_0600", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := LoadOrCreate(dir); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(keyPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o777 != 0o600 {
			t.Errorf("host key mode = %o, want 0600", info.Mode().Perm())
		}
		private, err := securefile.IsPrivate(keyPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if !private {
			t.Error("securefile.IsPrivate(host key) = false, want true")
		}
	})

	t.Run("reload_same_identity", func(t *testing.T) {
		dir := t.TempDir()
		first, err := LoadOrCreate(dir)
		if err != nil {
			t.Fatal(err)
		}
		second, err := LoadOrCreate(dir)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if second.HostID() != first.HostID() || loaded.HostID() != first.HostID() {
			t.Errorf("reload changed the host id: first %q, LoadOrCreate %q, Load %q", first.HostID(), second.HostID(), loaded.HostID())
		}
		msg := []byte("smind host identity")
		if !Verify(loaded.Public(), msg, second.Sign(msg)) {
			t.Error("signature from the reloaded-by-LoadOrCreate key does not verify under the Load'ed public key")
		}
	})

	t.Run("loosened_mode_refused", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no POSIX mode bits on windows")
		}
		dir := t.TempDir()
		if _, err := LoadOrCreate(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(keyPath(dir), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); !errors.Is(err, ErrInsecure) {
			t.Errorf("Load() error = %v, want ErrInsecure", err)
		}
		if _, err := LoadOrCreate(dir); !errors.Is(err, ErrInsecure) {
			t.Errorf("LoadOrCreate() error = %v, want ErrInsecure", err)
		}
		info, err := os.Stat(keyPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o777 != 0o644 {
			t.Errorf("host key mode = %o, want it left at 0644 rather than fixed", info.Mode().Perm())
		}
	})

	t.Run("corrupt_refused_never_regenerated", func(t *testing.T) {
		badSeed := base64.RawURLEncoding.EncodeToString(make([]byte, 31))
		goodSeed := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
		cases := []struct {
			name string
			data string
		}{
			{"empty file", ""},
			{"garbage", "garbage\n"},
			{"bad base64", keyPrefix + " !!!not-base64!!!\n"},
			{"31 byte seed", keyPrefix + " " + badSeed + "\n"},
			{"wrong prefix", "smind-host-key-v0 " + goodSeed + "\n"},
			{"oversized", strings.Repeat("a", 600)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(keyPath(dir), []byte(tc.data), 0o600); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(keyPath(dir))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := LoadOrCreate(dir); !errors.Is(err, ErrCorrupt) {
					t.Errorf("LoadOrCreate() error = %v, want ErrCorrupt", err)
				}
				after, err := os.ReadFile(keyPath(dir))
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Errorf("corrupt host key was rewritten: before %q, after %q", before, after)
				}
			})
		}
	})

	t.Run("missing_is_not_found", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := Load(dir); !errors.Is(err, ErrNotFound) {
			t.Errorf("Load() error = %v, want ErrNotFound", err)
		}
		if _, err := os.Stat(keyPath(dir)); !os.IsNotExist(err) {
			t.Errorf("Load() created %s, want no file", keyPath(dir))
		}
	})

	t.Run("sign_verify_roundtrip_and_tamper", func(t *testing.T) {
		dir := t.TempDir()
		k, err := LoadOrCreate(dir)
		if err != nil {
			t.Fatal(err)
		}
		msg := []byte("payload")
		sig := k.Sign(msg)
		if !Verify(k.Public(), msg, sig) {
			t.Error("own signature does not verify")
		}
		if Verify(k.Public(), append(msg, '!'), sig) {
			t.Error("tampered message verified")
		}
		if Verify(k.Public(), msg, append(append([]byte(nil), sig...), 0)) {
			t.Error("tampered signature verified")
		}
		if Verify(k.Public()[:31], msg, sig) {
			t.Error("wrong-length public key verified")
		}
	})
}

// TestHostKeyConcurrentCreateOneIdentity: concurrent first use must converge
// on one identity, not on whichever goroutine's key won the file race.
func TestHostKeyConcurrentCreateOneIdentity(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k, err := LoadOrCreate(dir)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = k.HostID()
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d: %v", i, errs[i])
		}
	}
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Errorf("goroutine %d got host id %q, goroutine 0 got %q", i, ids[i], ids[0])
		}
	}
}
