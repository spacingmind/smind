package steward

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// fakeRailStore is the RailStore fake: settable counts plus a record of what
// the rails asked for.
type fakeRailStore struct {
	byKey    map[ReviewKey]int64
	since    int
	perTask  int
	failures int

	keyCalls  []ReviewKey
	sinceArgs []time.Time
	taskCalls []int64
}

func (f *fakeRailStore) FindStewardReviewByKey(taskID int64, headSHA, contractSha, ciState string) (store.StewardReview, error) {
	key := ReviewKey{TaskID: taskID, HeadSHA: headSHA, ContractSha: contractSha, CIState: ciState}
	f.keyCalls = append(f.keyCalls, key)
	id, ok := f.byKey[key]
	if !ok {
		return store.StewardReview{}, sql.ErrNoRows
	}
	return store.StewardReview{ID: id, TaskID: taskID, HeadSHA: headSHA, ContractSha256: contractSha, CIState: ciState}, nil
}

func (f *fakeRailStore) CountStewardReviewsSince(since time.Time) (int, error) {
	f.sinceArgs = append(f.sinceArgs, since)
	return f.since, nil
}

func (f *fakeRailStore) CountStewardReviewsForTask(taskID int64) (int, error) {
	f.taskCalls = append(f.taskCalls, taskID)
	return f.perTask, nil
}

func (f *fakeRailStore) ConsecutiveStewardFailures() (int, error) {
	return f.failures, nil
}

func fixedConfig(cfg config.StewardConfig) func() (config.StewardConfig, error) {
	return func() (config.StewardConfig, error) { return cfg, nil }
}

func errConfig(err error) func() (config.StewardConfig, error) {
	return func() (config.StewardConfig, error) { return config.StewardConfig{}, err }
}

func newRails(st RailStore, cfg func() (config.StewardConfig, error)) *Rails {
	return &Rails{Store: st, Config: cfg}
}

func baseKey() ReviewKey {
	return ReviewKey{TaskID: 7, HeadSHA: "head-1", ContractSha: "contract-1", CIState: "success"}
}

func baseRequest() RailRequest {
	return RailRequest{Key: baseKey(), Trigger: TriggerHuman}
}

func enabledConfig() config.StewardConfig {
	return config.StewardConfig{Enabled: true}
}

func TestSwitches_EnabledFalseAndInvalidConfigOff(t *testing.T) {
	st := &fakeRailStore{}
	r := newRails(st, fixedConfig(config.StewardConfig{}))
	if r.Enabled() {
		t.Fatal("zero config must be off")
	}
	if err := r.Check(baseRequest()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("zero config: got %v, want ErrDisabled", err)
	}

	r = newRails(st, errConfig(errors.New("config.yaml: unknown field")))
	if r.Enabled() {
		t.Fatal("config error must count as off")
	}
	if err := r.Check(baseRequest()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("config error: got %v, want ErrDisabled", err)
	}

	r = newRails(st, fixedConfig(enabledConfig()))
	if !r.Enabled() {
		t.Fatal("enabled config must be on")
	}
	if err := r.Check(baseRequest()); err != nil {
		t.Fatalf("enabled config: got %v, want nil", err)
	}
}

func TestRails_DedupeSameHeadContractCI(t *testing.T) {
	key := baseKey()
	st := &fakeRailStore{byKey: map[ReviewKey]int64{key: 42}}
	r := newRails(st, fixedConfig(enabledConfig()))

	var dup *DuplicateError
	if err := r.Check(baseRequest()); !errors.As(err, &dup) {
		t.Fatalf("same key: got %v, want *DuplicateError", err)
	}
	if dup.ReviewID != 42 {
		t.Fatalf("duplicate id: got %d, want 42", dup.ReviewID)
	}
	if got, want := dup.Error(), "an identical review (#42) already exists for this head, contract and CI state"; got != want {
		t.Fatalf("duplicate message: got %q, want %q", got, want)
	}

	// Each part of the tuple changes the key, so each alone makes it a new
	// review.
	changedKeys := []struct {
		name string
		key  ReviewKey
	}{
		{"headSHA", ReviewKey{TaskID: key.TaskID, HeadSHA: "head-2", ContractSha: key.ContractSha, CIState: key.CIState}},
		{"contractSha", ReviewKey{TaskID: key.TaskID, HeadSHA: key.HeadSHA, ContractSha: "contract-2", CIState: key.CIState}},
		{"ciState", ReviewKey{TaskID: key.TaskID, HeadSHA: key.HeadSHA, ContractSha: key.ContractSha, CIState: "failure"}},
		{"taskID", ReviewKey{TaskID: 8, HeadSHA: key.HeadSHA, ContractSha: key.ContractSha, CIState: key.CIState}},
	}
	for _, c := range changedKeys {
		if err := r.Check(RailRequest{Key: c.key, Trigger: TriggerHuman}); err != nil {
			t.Fatalf("changed %s: got %v, want nil", c.name, err)
		}
	}

	if len(st.keyCalls) != 5 {
		t.Fatalf("dedupe lookups: got %d, want 5", len(st.keyCalls))
	}
	if st.keyCalls[0] != key {
		t.Fatalf("lookup key: got %+v, want %+v", st.keyCalls[0], key)
	}
	for i, c := range changedKeys {
		if st.keyCalls[i+1] != c.key {
			t.Fatalf("lookup %d: got %+v, want the exact changed tuple %+v", i+1, st.keyCalls[i+1], c.key)
		}
	}
}

func TestRails_MaxReviewsPerHour(t *testing.T) {
	r := newRails(&fakeRailStore{since: 5}, fixedConfig(enabledConfig()))
	if err := r.Check(baseRequest()); err != nil {
		t.Fatalf("5 this hour with default cap 6: got %v, want nil", err)
	}

	r = newRails(&fakeRailStore{since: 6}, fixedConfig(enabledConfig()))
	if err := r.Check(baseRequest()); !errors.Is(err, ErrHourlyCap) {
		t.Fatalf("6 this hour with default cap 6: got %v, want ErrHourlyCap", err)
	}

	r = newRails(&fakeRailStore{since: 2}, fixedConfig(config.StewardConfig{Enabled: true, MaxReviewsPerHour: 2}))
	if err := r.Check(baseRequest()); !errors.Is(err, ErrHourlyCap) {
		t.Fatalf("2 this hour with cap 2: got %v, want ErrHourlyCap", err)
	}
}

func TestRails_MaxReviewsPerTask(t *testing.T) {
	r := newRails(&fakeRailStore{perTask: 4}, fixedConfig(enabledConfig()))
	if err := r.Check(baseRequest()); err != nil {
		t.Fatalf("4 for the task with default cap 5: got %v, want nil", err)
	}

	r = newRails(&fakeRailStore{perTask: 5}, fixedConfig(enabledConfig()))
	if err := r.Check(baseRequest()); !errors.Is(err, ErrTaskCap) {
		t.Fatalf("5 for the task with default cap 5: got %v, want ErrTaskCap", err)
	}
	if len(r.Store.(*fakeRailStore).taskCalls) == 0 || r.Store.(*fakeRailStore).taskCalls[0] != baseKey().TaskID {
		t.Fatalf("per-task count: got %v, want task %d", r.Store.(*fakeRailStore).taskCalls, baseKey().TaskID)
	}
}

func TestRails_BudgetCeilingRefused(t *testing.T) {
	r := newRails(&fakeRailStore{}, fixedConfig(enabledConfig()))
	req := baseRequest()

	req.BudgetUSD = 5.01
	if err := r.Check(req); !errors.Is(err, ErrBudgetCeiling) {
		t.Fatalf("budget 5.01 over the default ceiling 5: got %v, want ErrBudgetCeiling", err)
	}
	req.BudgetUSD = 5
	if err := r.Check(req); err != nil {
		t.Fatalf("budget 5 at the ceiling: got %v, want nil", err)
	}
	req.MaxMinutes = 61
	if err := r.Check(req); !errors.Is(err, ErrBudgetCeiling) {
		t.Fatalf("61 minutes over the ceiling 60: got %v, want ErrBudgetCeiling", err)
	}
	req.MaxMinutes = -1
	if err := r.Check(req); !errors.Is(err, ErrBudgetCeiling) {
		t.Fatalf("negative minutes: got %v, want ErrBudgetCeiling", err)
	}
	req.MaxMinutes, req.BudgetUSD = 20, -1
	if err := r.Check(req); !errors.Is(err, ErrBudgetCeiling) {
		t.Fatalf("negative budget: got %v, want ErrBudgetCeiling", err)
	}

	r = newRails(&fakeRailStore{}, fixedConfig(config.StewardConfig{Enabled: true, MaxBudgetUsd: 2}))
	req.BudgetUSD, req.MaxMinutes = 3, 20
	if err := r.Check(req); !errors.Is(err, ErrBudgetCeiling) {
		t.Fatalf("budget 3 over maxBudgetUsd 2: got %v, want ErrBudgetCeiling", err)
	}
}

func TestRails_AutoPauseAfterThreeCrashes(t *testing.T) {
	r := newRails(&fakeRailStore{failures: 2}, fixedConfig(enabledConfig()))
	auto := baseRequest()
	auto.Trigger = TriggerAuto
	if err := r.Check(auto); err != nil {
		t.Fatalf("2 crashes: got %v, want nil", err)
	}

	r = newRails(&fakeRailStore{failures: 3}, fixedConfig(enabledConfig()))
	if err := r.Check(auto); !errors.Is(err, ErrPaused) {
		t.Fatalf("3 crashes, auto: got %v, want ErrPaused", err)
	}
	if err := r.Check(baseRequest()); err != nil {
		t.Fatalf("3 crashes, human: got %v, want nil (a human trigger is never paused)", err)
	}
}

func TestRails_OrderDisabledBeforeEverything(t *testing.T) {
	key := baseKey()
	st := &fakeRailStore{
		byKey:   map[ReviewKey]int64{key: 42},
		since:   99,
		perTask: 99,
	}
	r := newRails(st, fixedConfig(config.StewardConfig{}))
	auto := baseRequest()
	auto.Trigger = TriggerAuto
	auto.BudgetUSD = 99
	if err := r.Check(auto); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled with duplicate and caps hit: got %v, want ErrDisabled", err)
	}
	if len(st.keyCalls) != 0 || len(st.sinceArgs) != 0 || len(st.taskCalls) != 0 {
		t.Fatal("a disabled switch must not touch the store")
	}
}

func TestRails_FakeNowForHourWindow(t *testing.T) {
	fixed := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	st := &fakeRailStore{}
	r := &Rails{Store: st, Config: fixedConfig(enabledConfig()), Now: func() time.Time { return fixed }}
	if err := r.Check(baseRequest()); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	if len(st.sinceArgs) != 1 {
		t.Fatalf("hourly count calls: got %d, want 1", len(st.sinceArgs))
	}
	if want := fixed.Add(-time.Hour); !st.sinceArgs[0].Equal(want) {
		t.Fatalf("since argument: got %v, want exactly %v", st.sinceArgs[0], want)
	}
}

func TestEffectiveBudget(t *testing.T) {
	limits := config.StewardLimits{BudgetUSD: 1.5, MaxMinutes: 20}
	if usd, minutes := EffectiveBudget(RailRequest{BudgetUSD: 2, MaxMinutes: 30}, limits); usd != 2 || minutes != 30 {
		t.Fatalf("request values: got %g / %d, want 2 / 30", usd, minutes)
	}
	if usd, minutes := EffectiveBudget(RailRequest{}, limits); usd != 1.5 || minutes != 20 {
		t.Fatalf("zero request falls back to limits: got %g / %d, want 1.5 / 20", usd, minutes)
	}
	if usd, minutes := EffectiveBudget(RailRequest{BudgetUSD: -1, MaxMinutes: -5}, limits); usd != 1.5 || minutes != 20 {
		t.Fatalf("negative request falls back to limits: got %g / %d, want 1.5 / 20", usd, minutes)
	}
}
