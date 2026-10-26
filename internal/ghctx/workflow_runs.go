package ghctx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/forge"
)

// workflowRunFields is the --json field list of `gh run list`.
const workflowRunFields = "databaseId,workflowName,workflowDatabaseId,headBranch,headSha,status,conclusion,event,url,createdAt,updatedAt"

// maxWorkflowRuns caps one list call.
const maxWorkflowRuns = 30

type ghRunJSON struct {
	DatabaseID         int64  `json:"databaseId"`
	WorkflowName       string `json:"workflowName"`
	WorkflowDatabaseID int64  `json:"workflowDatabaseId"`
	HeadBranch         string `json:"headBranch"`
	HeadSha            string `json:"headSha"`
	Status             string `json:"status"`
	Conclusion         string `json:"conclusion"`
	Event              string `json:"event"`
	URL                string `json:"url"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

// WorkflowRuns lists the newest runs of one workflow on one branch with one
// `gh run list` call (ADR-0044 section 2.2). The workflow and branch reach argv
// as `--flag=value` after a leading-dash check, never through a shell; the
// result is not cached (the automation engine owns the cadence and the call
// budget). A run whose URL is not https is dropped.
func (c *Client) WorkflowRuns(ctx context.Context, dir string, q forge.WorkflowRunQuery) ([]forge.WorkflowRun, error) {
	if q.Workflow == "" || q.Branch == "" || strings.HasPrefix(q.Workflow, "-") || strings.HasPrefix(q.Branch, "-") ||
		strings.ContainsAny(q.Workflow+q.Branch, "\x00\n\r") {
		return nil, fmt.Errorf("invalid arguments")
	}
	limit := q.Limit
	if limit <= 0 || limit > maxWorkflowRuns {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	out, err := c.run(ctx, dir, "run", "list", "--workflow="+q.Workflow, "--branch="+q.Branch,
		"--limit", fmt.Sprint(limit), "--json", workflowRunFields)
	if err != nil {
		return nil, c.mapRunError(ctx, err)
	}
	var rows []ghRunJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, forge.NewError(forge.CodeError, "parse gh run list output: "+err.Error())
	}
	runs := make([]forge.WorkflowRun, 0, len(rows))
	for _, r := range rows {
		if r.DatabaseID <= 0 || !strings.HasPrefix(r.URL, "https://") {
			continue
		}
		runs = append(runs, forge.WorkflowRun{
			ID: r.DatabaseID, Workflow: r.WorkflowName, WorkflowID: r.WorkflowDatabaseID, Branch: r.HeadBranch, HeadSha: r.HeadSha,
			Status: strings.ToLower(r.Status), Conclusion: strings.ToLower(r.Conclusion), Event: r.Event, URL: r.URL,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return runs, nil
}
