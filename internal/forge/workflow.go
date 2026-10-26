package forge

import "context"

// WorkflowRun is one CI workflow run, forge-neutral (ADR-0044 section 2.2).
// Status is "completed" when the run has ended; Conclusion is then "success",
// "failure", "cancelled", "timed_out", ... and is empty while it runs.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Workflow   string `json:"workflow"`
	WorkflowID int64  `json:"workflowId"`
	Branch     string `json:"branch"`
	HeadSha    string `json:"headSha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Event      string `json:"event"`
	URL        string `json:"url"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// WorkflowRunQuery selects runs: one workflow (name or file) on one branch.
type WorkflowRunQuery struct {
	Workflow string
	Branch   string
	Limit    int
}

// WorkflowRunLister lists workflow runs of dir's repo with one call. It is a
// separate interface (a Provider may implement it) so the automation engine
// asks for exactly this capability and Provider keeps its shape.
type WorkflowRunLister interface {
	WorkflowRuns(ctx context.Context, dir string, q WorkflowRunQuery) ([]WorkflowRun, error)
}
