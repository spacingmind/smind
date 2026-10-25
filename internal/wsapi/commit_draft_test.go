package wsapi

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/smind/internal/commitdraft"
)

type fakeDrafter struct {
	calls atomic.Int32
	res   CommitDraft
	err   error
}

func (f *fakeDrafter) DraftCommitMessage(_ context.Context, _ int64) (CommitDraft, error) {
	f.calls.Add(1)
	return f.res, f.err
}

func TestWSAPI_DraftCommitMessage_RunScopedRefused_ZeroModelCalls(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	d := &fakeDrafter{res: CommitDraft{Message: "feat: x", Model: "m"}}
	e.api.SetCommitDrafter(d)
	params := map[string]any{"taskId": e.seatTask.ID}

	// Over a real run-scoped connection: refused, never reaches the drafter.
	resp := callWS(t, e.identified(t), "task.draftCommitMessage", params)
	if resp.Error == nil || (resp.Error.Code != "forbidden" && resp.Error.Code != "refused") {
		t.Fatalf("agent response = %+v / %+v, want a refusal", resp.Result, resp.Error)
	}
	// The handler's own guard, with the dispatcher's method allow-list out of the way.
	raw, _ := json.Marshal(params)
	_, err := e.api.hs["task.draftCommitMessage"](context.Background(), &requestContext{runScoped: true}, raw)
	if c := codedError(err); c.Code != "refused" {
		t.Fatalf("handler error = %v (code %q), want refused", err, c.Code)
	}
	if n := d.calls.Load(); n != 0 {
		t.Fatalf("drafter calls = %d, want 0", n)
	}

	// A human gets the draft, with the typed shape.
	resp = callWS(t, e.admin, "task.draftCommitMessage", params)
	wantOK(t, resp, "human draft")
	var got CommitDraft
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Message != "feat: x" || got.Model != "m" || got.Omitted == nil {
		t.Fatalf("result = %s", resp.Result)
	}
	if n := d.calls.Load(); n != 1 {
		t.Fatalf("drafter calls = %d, want 1", n)
	}
}

func TestWSAPI_DraftCommitMessage_TypedCodes(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	api, _ := newGitTestAPI(t, wm, db, nil)
	h := api.hs["task.draftCommitMessage"]

	if _, err := h(context.Background(), &requestContext{}, json.RawMessage(`{"taskId":1}`)); codedError(err).Code != codeUnavailable {
		t.Fatalf("no drafter: %v", err)
	}
	d := &fakeDrafter{}
	api.SetCommitDrafter(d)
	if _, err := h(context.Background(), &requestContext{}, json.RawMessage(`{}`)); codedError(err).Code != codeInvalid {
		t.Fatalf("missing taskId: %v", err)
	}
	for _, want := range []*commitdraft.Error{
		commitdraft.ErrNothingStaged, commitdraft.ErrNoDraftProvider, commitdraft.ErrRateLimited,
		commitdraft.ErrBusy, commitdraft.ErrEmptyDraft, commitdraft.ErrDraftFailed,
	} {
		d.err = want
		_, err := h(context.Background(), &requestContext{}, json.RawMessage(`{"taskId":1}`))
		if got := codedError(err).Code; got != want.Code {
			t.Errorf("code = %q, want %q", got, want.Code)
		}
	}
}
