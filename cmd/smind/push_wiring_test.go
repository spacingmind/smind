package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/push"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsapi"
)

type fakePushAPI struct {
	calls int
	opts  wsapi.PushOptions
}

func (f *fakePushAPI) SetPush(o wsapi.PushOptions) { f.calls++; f.opts = o }

func openPushTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTaskInfoFor(t *testing.T) {
	s := openPushTestStore(t)
	ws, err := s.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "root", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "child", Status: "open", ParentTaskID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	sys, _, err := s.CreateSystemTask(ws.ID, "sys", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	look := taskInfoFor(s)
	if got, ok := look(root.ID); !ok || got != (push.TaskInfo{Title: "root", Root: true}) {
		t.Fatalf("root = %+v, %v", got, ok)
	}
	if got, ok := look(child.ID); !ok || got != (push.TaskInfo{Title: "child", Root: false}) {
		t.Fatalf("child = %+v, %v", got, ok)
	}
	if got, ok := look(sys.ID); !ok || !got.System {
		t.Fatalf("system = %+v, %v", got, ok)
	}
	if _, ok := look(99999); ok {
		t.Fatal("missing task found")
	}
}

func TestWirePushDisabledByConfig(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	api := &fakePushAPI{}
	rt := wirePush(api, bridge.NewHub(bridge.Options{}), openPushTestStore(t), config.PushConfig{Enabled: false})
	if api.calls != 1 || api.opts.Enabled || api.opts.Notifier != nil {
		t.Fatalf("SetPush calls = %d, opts = %+v", api.calls, api.opts)
	}
	if rt.notifier != nil || rt.registry != nil {
		t.Fatalf("runtime = %+v", rt)
	}
	rt.Close()
}

func TestWirePushCorruptRegistryDisablesPush(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := push.RegistryPath(config.Dir())
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakePushAPI{}
	rt := wirePush(api, bridge.NewHub(bridge.Options{}), openPushTestStore(t), config.PushConfig{Enabled: true})
	if api.calls != 1 || api.opts.Enabled {
		t.Fatalf("SetPush calls = %d, opts = %+v", api.calls, api.opts)
	}
	if rt.notifier != nil || rt.registry != nil {
		t.Fatalf("runtime = %+v", rt)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "not json" {
		t.Fatalf("registry file changed: %q, %v", got, err)
	}
}

func TestWirePushEnabledStartsNotifier(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	api := &fakePushAPI{}
	rt := wirePush(api, bridge.NewHub(bridge.Options{}), openPushTestStore(t), config.PushConfig{Enabled: true})
	if rt.notifier == nil || rt.registry == nil {
		t.Fatalf("runtime = %+v", rt)
	}
	if !api.opts.Enabled || api.opts.Notifier != rt.notifier || api.opts.Registry == nil || api.opts.Approved == nil {
		t.Fatalf("opts = %+v", api.opts)
	}
	rt.Close()
	rt.Close()
	var nilRT *pushRuntime
	nilRT.Close()
}
