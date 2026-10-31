package fleet

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/securefile"
	"github.com/spacingmind/smind/internal/store"
)

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestFleetOffIsByteForByte(t *testing.T) {
	// (a) Off by default, and off means untouched: no directory entry
	// appears, no host key is created, no registry is opened.
	if config.Default().Fleet.Enabled {
		t.Fatal("the default config must keep the fleet off")
	}
	dir := t.TempDir()
	before := dirNames(t, dir)
	cfg := config.DefaultFleet()
	if _, err := LoadIdentity(dir, cfg); !errors.Is(err, ErrOff) {
		t.Fatalf("LoadIdentity while off: got %v, want ErrOff", err)
	}
	if after := dirNames(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatalf("LoadIdentity while off changed %v to %v", before, after)
	}
	if slices.Contains(dirNames(t, dir), identity.KeyFile) {
		t.Fatal("no host-key file may exist while the fleet is off")
	}
	if _, err := OpenRegistry(nil, cfg); !errors.Is(err, ErrOff) {
		t.Fatalf("OpenRegistry while off: got %v, want ErrOff", err)
	}

	// (b) A store opened while the fleet is off holds no fleet rows, and a
	// local task keeps a local (empty) origin.
	dbPath := filepath.Join(t.TempDir(), "smind.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	raw, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, table := range []string{
		"fleet_hosts", "fleet_links", "fleet_audit",
		"fleet_watches", "fleet_idem", "fleet_remote_tasks",
	} {
		var n int
		if err := raw.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s holds %d rows while the fleet is off", table, n)
		}
	}
	ws, err := st.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "local"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "local task"})
	if err != nil {
		t.Fatal(err)
	}
	host, trace, err := st.TaskOrigin(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" || trace != "" {
		t.Fatalf("a local task's origin = %q/%q, want empty/empty", host, trace)
	}

	// (c) Switched on, the identity is created once and is stable.
	home := filepath.Join(t.TempDir(), "fleet")
	on := config.DefaultFleet()
	on.Enabled = true
	k, err := LoadIdentity(home, on)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(home, identity.KeyFile)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("host-key mode = %o, want 600", perm)
		}
	}
	again, err := LoadIdentity(home, on)
	if err != nil {
		t.Fatal(err)
	}
	if again.HostID() != k.HostID() {
		t.Fatalf("second load returned %s, want the same host id %s", again.HostID(), k.HostID())
	}

	// (d) A corrupt host key is an incident, not a regenerated identity.
	// securefile.WriteFile, not os.WriteFile: on Windows only a restricted DACL
	// passes the private-file check that runs before the corrupt-seed check.
	corruptHome := filepath.Join(t.TempDir(), "fleet")
	if err := os.MkdirAll(corruptHome, 0o700); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(corruptHome, identity.KeyFile)
	if err := securefile.WriteFile(corruptPath, []byte("garbage\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(corruptHome, on); !errors.Is(err, identity.ErrCorrupt) {
		t.Fatalf("LoadIdentity with a corrupt host key: got %v, want identity.ErrCorrupt", err)
	}
	data, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "garbage\n" {
		t.Fatalf("the corrupt host key was rewritten to %q", data)
	}
}
