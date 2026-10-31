package runs

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

type fakeQuota struct {
	until map[string]time.Time
	calls atomic.Int32
}

func (f *fakeQuota) ExhaustedUntil(provider string, _ time.Time) (time.Time, bool) {
	f.calls.Add(1)
	u, ok := f.until[provider]
	return u, ok
}

func (f *fakeQuota) ExhaustedProviders(time.Time) map[string]time.Time {
	f.calls.Add(1)
	m := map[string]time.Time{}
	for k, v := range f.until {
		m[k] = v
	}
	return m
}

func TestProviderCooldown_WindowExhaustedBlocks(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	w := time.Now().Add(2 * time.Hour)
	e.reg.SetProviderQuota(&fakeQuota{until: map[string]time.Time{"glm": w}})
	got, ok := e.reg.activeProviderCooldown("glm")
	if !ok || !got.Equal(w) {
		t.Fatalf("activeProviderCooldown = %v, %v, want %v", got, ok, w)
	}
	if m := e.reg.ProviderCooldowns(); !m["glm"].Equal(w) {
		t.Fatalf("ProviderCooldowns = %v, want glm %v", m, w)
	}
	if _, ok := e.reg.activeProviderCooldown("claude"); ok {
		t.Fatal("other provider cooling")
	}
}

func TestProviderCooldown_LaterOfRowAndWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ row, win time.Duration }{{time.Hour, 3 * time.Hour}, {3 * time.Hour, time.Hour}} {
		e := newQueuedTestEnv(t, "")
		row := e.cool(t, "glm", tc.row)
		win := time.Now().Add(tc.win)
		e.reg.SetProviderQuota(&fakeQuota{until: map[string]time.Time{"glm": win}})
		want := row
		if tc.win > tc.row {
			want = win
		}
		got, ok := e.reg.activeProviderCooldown("glm")
		if !ok || !got.Equal(want) {
			t.Fatalf("row %v win %v: got %v, %v, want %v", tc.row, tc.win, got, ok, want)
		}
		if m := e.reg.ProviderCooldowns(); !m["glm"].Equal(want) {
			t.Fatalf("row %v win %v: ProviderCooldowns = %v, want %v", tc.row, tc.win, m, want)
		}
	}
}

func TestProviderCooldown_NoQuotaSetUnchanged(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	if _, ok := e.reg.activeProviderCooldown("glm"); ok {
		t.Fatal("cooling with no row")
	}
	if m := e.reg.ProviderCooldowns(); m == nil || len(m) != 0 {
		t.Fatalf("ProviderCooldowns = %v, want empty non-nil", m)
	}
	row := e.cool(t, "glm", time.Hour)
	got, ok := e.reg.activeProviderCooldown("glm")
	if !ok || !got.Equal(row) {
		t.Fatalf("got %v, %v, want row %v", got, ok, row)
	}
	if m := e.reg.ProviderCooldowns(); len(m) != 1 || !m["glm"].Equal(row) {
		t.Fatalf("ProviderCooldowns = %v", m)
	}
	e.reg.SetProviderQuota(nil)
	if _, ok := e.reg.activeProviderCooldown("glm"); !ok {
		t.Fatal("row lost after SetProviderQuota(nil)")
	}
}

func TestProviderCooldown_WindowPastIgnored(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.SetProviderQuota(&fakeQuota{until: map[string]time.Time{"glm": time.Now().Add(-time.Minute)}})
	if _, ok := e.reg.activeProviderCooldown("glm"); ok {
		t.Fatal("past window cooling")
	}
	if m := e.reg.ProviderCooldowns(); len(m) != 0 {
		t.Fatalf("ProviderCooldowns = %v, want empty", m)
	}
}

func TestProviderCooldown_CappedAtMax(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.SetProviderQuota(&fakeQuota{until: map[string]time.Time{"glm": time.Now().Add(24 * time.Hour)}})
	lo := time.Now().Add(store.MaxProviderCooldown)
	got, ok := e.reg.activeProviderCooldown("glm")
	hi := time.Now().Add(store.MaxProviderCooldown)
	if !ok || got.Before(lo) || got.After(hi) {
		t.Fatalf("got %v, %v, want within [%v, %v]", got, ok, lo, hi)
	}
	m := e.reg.ProviderCooldowns()
	if g := m["glm"]; g.Before(lo) || g.After(time.Now().Add(store.MaxProviderCooldown)) {
		t.Fatalf("ProviderCooldowns glm = %v, want ~now+max", g)
	}
}

func TestProviderCooldown_NonHumanSendFastFailsOnWindow(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	w := time.Now().Add(time.Hour)
	q := &fakeQuota{until: map[string]time.Time{"glm": w}}
	e.reg.SetProviderQuota(q)

	_, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	var ce *ProviderCooldownError
	if !errors.As(err, &ce) || ce.Provider != "glm" || !ce.Until.Equal(w) {
		t.Fatalf("agent send err = %v, want *ProviderCooldownError until %v", err, w)
	}
	if runs, _ := e.st.ListRecentRuns(100); len(runs) != 0 {
		t.Fatalf("runs = %d, want none", len(runs))
	}
	if q.calls.Load() == 0 {
		t.Fatal("quota never consulted")
	}

	out, err := e.send(0, "", store.ChatQueueSourceHuman, 0, 0)
	if err != nil || out.RunID == "" {
		t.Fatalf("human send = %+v err=%v, want a started run", out, err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}
