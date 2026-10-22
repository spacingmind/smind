package push

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func testReg(deviceID, token string) Registration {
	return Registration{
		DeviceID:     deviceID,
		Provider:     "expo",
		Token:        token,
		Platform:     "android",
		Key:          testKey(),
		Kinds:        []string{"permission", "done"},
		RegisteredAt: time.Unix(1790000000, 0).UTC(),
	}
}

func mustOpenRegistry(t *testing.T, path string) *Registry {
	t.Helper()
	r, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryPutGetPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-registrations.json")
	reg := testReg("dev-a", "tok-a")
	if err := mustOpenRegistry(t, path).Put(reg); err != nil {
		t.Fatal(err)
	}
	got, ok := mustOpenRegistry(t, path).Get("dev-a")
	if !ok {
		t.Fatal("registration missing after reopen")
	}
	if !bytes.Equal(got.Key, reg.Key) {
		t.Fatalf("key = %x, want %x", got.Key, reg.Key)
	}
	if !got.RegisteredAt.Equal(reg.RegisteredAt) {
		t.Fatalf("registered_at = %v, want %v", got.RegisteredAt, reg.RegisteredAt)
	}
	if got.DeviceID != reg.DeviceID || got.Provider != reg.Provider ||
		got.Token != reg.Token || got.Platform != reg.Platform ||
		!slices.Equal(got.Kinds, reg.Kinds) {
		t.Fatalf("registration = %+v, want %+v", got, reg)
	}
}

func TestRegistryFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX mode bits on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "push-registrations.json")
	if err := mustOpenRegistry(t, path).Put(testReg("dev-a", "tok-a")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestRegistryCorruptFileIsError(t *testing.T) {
	for name, content := range map[string]string{
		"not json":        "not json",
		"unknown version": `{"v":2,"registrations":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "push-registrations.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenRegistry(path); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error = %v, want ErrCorrupt", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Fatalf("corrupt file was modified: %q", got)
			}
		})
	}
}

func TestRegistryTokenTakeover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-registrations.json")
	r := mustOpenRegistry(t, path)
	if err := r.Put(testReg("dev-a", "tok-t")); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(testReg("dev-b", "tok-t")); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("dev-a"); ok {
		t.Fatal("dev-a still registered after its token was taken over")
	}
	if _, ok := r.Get("dev-b"); !ok {
		t.Fatal("dev-b missing after takeover")
	}
	reopened := mustOpenRegistry(t, path)
	if _, ok := reopened.Get("dev-a"); ok {
		t.Fatal("dev-a survived on disk after token takeover")
	}
	if _, ok := reopened.Get("dev-b"); !ok {
		t.Fatal("dev-b missing on disk after takeover")
	}
}

func TestRegistryPutReplacesSameDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-registrations.json")
	r := mustOpenRegistry(t, path)
	if err := r.Put(testReg("dev-a", "tok-1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(testReg("dev-a", "tok-2")); err != nil {
		t.Fatal(err)
	}
	got := mustOpenRegistry(t, path).List()
	if len(got) != 1 || got[0].Token != "tok-2" {
		t.Fatalf("registrations = %+v, want exactly one with token tok-2", got)
	}
}

func TestRegistryRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-registrations.json")
	r := mustOpenRegistry(t, path)
	if err := r.Put(testReg("dev-a", "tok-a")); err != nil {
		t.Fatal(err)
	}
	ok, err := r.Remove("dev-a")
	if err != nil || !ok {
		t.Fatalf("Remove = (%v, %v), want (true, nil)", ok, err)
	}
	if _, ok := mustOpenRegistry(t, path).Get("dev-a"); ok {
		t.Fatal("dev-a survived on disk after Remove")
	}
	ok, err = r.Remove("dev-a")
	if err != nil || ok {
		t.Fatalf("Remove of absent = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestRegistryRemoveToken(t *testing.T) {
	r := mustOpenRegistry(t, filepath.Join(t.TempDir(), "push-registrations.json"))
	if err := r.Put(testReg("dev-a", "tok-1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(testReg("dev-b", "tok-2")); err != nil {
		t.Fatal(err)
	}
	n, err := r.RemoveToken("tok-1")
	if err != nil || n != 1 {
		t.Fatalf("RemoveToken = (%d, %v), want (1, nil)", n, err)
	}
	if _, ok := r.Get("dev-a"); ok {
		t.Fatal("dev-a survived RemoveToken")
	}
	if _, ok := r.Get("dev-b"); !ok {
		t.Fatal("RemoveToken removed the wrong device")
	}
	if n, err := r.RemoveToken("tok-nope"); err != nil || n != 0 {
		t.Fatalf("RemoveToken of unknown = (%d, %v), want (0, nil)", n, err)
	}
}

func TestRegistryFailedWriteKeepsLiveState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	r := mustOpenRegistry(t, filepath.Join(dir, "push-registrations.json"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(testReg("dev-a", "tok-a")); err == nil {
		t.Fatal("Put succeeded under a read-only directory")
	}
	if _, ok := r.Get("dev-a"); ok {
		t.Fatal("live state changed despite the failed write")
	}
}

func TestRegistryClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push-registrations.json")
	r := mustOpenRegistry(t, path)
	if err := r.Put(testReg("dev-a", "tok-a")); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(testReg("dev-b", "tok-b")); err != nil {
		t.Fatal(err)
	}
	if err := r.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("List after Clear = %+v, want none", got)
	}
	if got := mustOpenRegistry(t, path).List(); len(got) != 0 {
		t.Fatalf("reopened List after Clear = %+v, want none", got)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := mustOpenRegistry(t, filepath.Join(t.TempDir(), "push-registrations.json"))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("dev-%d", i)
			for j := range 20 {
				if err := r.Put(testReg(id, fmt.Sprintf("tok-%d-%d", i, j))); err != nil {
					t.Errorf("put %s: %v", id, err)
					return
				}
				r.Get(id)
				r.List()
				if j%5 == 0 {
					if _, err := r.Remove(id); err != nil {
						t.Errorf("remove %s: %v", id, err)
						return
					}
				}
				if _, err := r.RemoveToken(fmt.Sprintf("tok-%d-%d", i, j)); err != nil {
					t.Errorf("remove token: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestRegistryGetReturnsCopy(t *testing.T) {
	r := mustOpenRegistry(t, filepath.Join(t.TempDir(), "push-registrations.json"))
	if err := r.Put(testReg("dev-a", "tok-a")); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get("dev-a")
	got.Key[0] ^= 0xff
	again, _ := r.Get("dev-a")
	if !bytes.Equal(again.Key, testKey()) {
		t.Fatalf("stored key changed to %x", again.Key)
	}
}
