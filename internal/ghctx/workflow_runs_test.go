package ghctx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

const workflowRunsJSON = `[
 {"databaseId":300,"workflowName":"Windows full suite","workflowDatabaseId":77,"headBranch":"develop","headSha":"aaaaaaa1","status":"completed","conclusion":"failure","event":"schedule","url":"https://github.com/o/r/actions/runs/300","createdAt":"2026-10-27T19:17:00Z","updatedAt":"2026-10-27T19:50:00Z"},
 {"databaseId":299,"workflowName":"Windows full suite","workflowDatabaseId":77,"headBranch":"develop","headSha":"bbbbbbb2","status":"IN_PROGRESS","conclusion":"","event":"schedule","url":"https://github.com/o/r/actions/runs/299","createdAt":"2026-10-26T19:17:00Z","updatedAt":"2026-10-26T19:17:00Z"},
 {"databaseId":298,"workflowName":"x","headBranch":"develop","headSha":"c","status":"completed","conclusion":"success","url":"http://evil.example/run/298"}
]`

func TestWorkflowRuns_ArgvAndParse(t *testing.T) {
	var got []string
	c := New(WithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		got = args
		return workflowRunsJSON, nil
	}))
	runs, err := c.WorkflowRuns(context.Background(), "/ws", forge.WorkflowRunQuery{Workflow: "Windows full suite", Branch: "develop", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := "run list --workflow=Windows full suite --branch=develop --limit 5 --json " + workflowRunFields
	if strings.Join(got, " ") != want {
		t.Fatalf("argv = %q", strings.Join(got, " "))
	}
	if len(runs) != 2 { // the http:// URL row is dropped
		t.Fatalf("runs = %+v", runs)
	}
	if r := runs[0]; r.ID != 300 || r.Conclusion != "failure" || r.Status != "completed" || r.WorkflowID != 77 || r.HeadSha != "aaaaaaa1" {
		t.Fatalf("run 0 = %+v", r)
	}
	if runs[1].Status != "in_progress" {
		t.Fatalf("status not lower-cased: %q", runs[1].Status)
	}
}

func TestWorkflowRuns_RejectsFlagLikeAndControlInput(t *testing.T) {
	called := false
	c := New(WithRunner(func(context.Context, string, ...string) (string, error) { called = true; return "[]", nil }))
	for _, q := range []forge.WorkflowRunQuery{
		{Workflow: "--json=x", Branch: "develop"}, {Workflow: "w", Branch: "-x"}, {Workflow: "", Branch: "b"},
		{Workflow: "w\nx", Branch: "b"}, {Workflow: "w", Branch: ""},
	} {
		if _, err := c.WorkflowRuns(context.Background(), "/ws", q); err == nil {
			t.Errorf("%+v accepted", q)
		}
	}
	if called {
		t.Fatal("gh ran for a rejected query")
	}
}

func TestWorkflowRuns_ErrorsMapped(t *testing.T) {
	c := New(WithRunner(func(context.Context, string, ...string) (string, error) {
		return "", errors.New("gh: HTTP 401: Bad credentials")
	}))
	_, err := c.WorkflowRuns(context.Background(), "/ws", forge.WorkflowRunQuery{Workflow: "w", Branch: "b"})
	var fe *forge.Error
	if !errors.As(err, &fe) || fe.Code != forge.CodeUnauthenticated {
		t.Fatalf("err = %v", err)
	}
	c = New(WithRunner(func(context.Context, string, ...string) (string, error) { return "not json", nil }))
	if _, err := c.WorkflowRuns(context.Background(), "/ws", forge.WorkflowRunQuery{Workflow: "w", Branch: "b"}); err == nil {
		t.Fatal("bad json accepted")
	}
}
