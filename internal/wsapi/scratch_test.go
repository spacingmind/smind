package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/scratch"
)

// scratchHarness is a minimal API (the secretsHarness shape) with a scratch
// Manager over a temp root attached through AttachScratch.
type scratchHarness struct {
	api  *API
	mgr  *scratch.Manager
	root string
}

func newScratchHarness(t *testing.T, live func(int64) bool) *scratchHarness {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	root := t.TempDir()
	m := scratch.New(root)
	api.AttachScratch(m, live)
	return &scratchHarness{api: api, mgr: m, root: root}
}

func (h *scratchHarness) call(t *testing.T, method string, rc *requestContext, params any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return h.api.hs[method](context.Background(), rc, raw)
}

// seed writes one n-byte file under scratch/<id>/f.txt.
func (h *scratchHarness) seed(t *testing.T, id int64, n int) string {
	t.Helper()
	dir := filepath.Join(h.root, strconv.FormatInt(id, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestScratchRPC_RefusedForRunScoped: an agent (run-scoped) caller gets
// forbidden on both methods, nothing is deleted, and neither method is on
// the run-scoped allowlist (AC6).
func TestScratchRPC_RefusedForRunScoped(t *testing.T) {
	h := newScratchHarness(t, func(int64) bool { return true })
	f := h.seed(t, 7, 5)
	agent := &requestContext{runScoped: true}
	for _, method := range []string{"scratch.usage", "scratch.clean"} {
		_, err := h.call(t, method, agent, nil)
		if err == nil || codedError(err).Code != codeForbidden {
			t.Errorf("%s: err = %v, want forbidden", method, err)
		}
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("scratch/7/f.txt removed by a refused call: %v", err)
	}
	for _, m := range RunScopedMethods {
		if m == "scratch.usage" || m == "scratch.clean" {
			t.Fatalf("%s is on the run-scoped allowlist", m)
		}
	}
}

// TestScratchRPC_RefusedForPlugin: a plugin session is denied by the
// never-list prefix, whatever the capability table says (AC6).
func TestScratchRPC_RefusedForPlugin(t *testing.T) {
	h := newScratchHarness(t, nil)
	reg := newPluginRegistry()
	reg.bind(h.api.hs, h.api.bus)
	sess := reg.Open(host.Plugin{ID: "spy", Capabilities: []string{CapTasksRead, CapEventsObserve, CapMcpRead, CapProfilesRead, "tasks.write"}}, func(string, any) error { return nil })
	defer sess.Close()
	for _, m := range []string{"scratch.usage", "scratch.clean"} {
		res, rerr := sess.Request(context.Background(), m, json.RawMessage(`{}`))
		if rerr == nil || res != nil {
			t.Errorf("%s answered a plugin session: %v", m, res)
			continue
		}
		if rerr.Code != host.ErrCodeForbidden || !strings.Contains(rerr.Message, "never available to plugins") {
			t.Errorf("%s: code %d %q, want the never-list refusal", m, rerr.Code, rerr.Message)
		}
	}
}

// TestScratchRPC_UsageAndClean: usage sizes every numeric task dir and
// flags the closed ones; clean sweeps exactly those (AC7).
func TestScratchRPC_UsageAndClean(t *testing.T) {
	h := newScratchHarness(t, func(id int64) bool { return id == 1 })
	live := h.seed(t, 1, 10)
	closed := h.seed(t, 2, 30)
	notes := filepath.Join(h.root, "notes")
	if err := os.WriteFile(notes, []byte("not a task dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := h.call(t, "scratch.usage", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(scratchUsageResult); got.TotalBytes != 40 || got.TotalFiles != 2 || got.Dirs != 2 ||
		got.ReclaimableDirs != 1 || got.ReclaimableBytes != 30 {
		t.Fatalf("usage = %+v", got)
	}

	res, err = h.call(t, "scratch.clean", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(scratchCleanResult); got.Files != 1 || got.Bytes != 30 || got.Dirs != 1 {
		t.Fatalf("clean = %+v", got)
	}
	if _, err := os.Stat(closed); !os.IsNotExist(err) {
		t.Fatalf("closed task's dir survived the clean: %v", err)
	}
	for _, p := range []string{live, notes} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must survive the clean: %v", p, err)
		}
	}
	res, err = h.call(t, "scratch.usage", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(scratchUsageResult); got.TotalBytes != 10 || got.Dirs != 1 || got.ReclaimableDirs != 0 {
		t.Fatalf("usage after clean = %+v", got)
	}
}
