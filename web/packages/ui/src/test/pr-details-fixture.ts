import type { CheckItem, PRDetails, ReviewThread } from "@/lib/types";

export const thread = (over: Partial<ReviewThread> = {}): ReviewThread => ({
  id: "t1",
  isResolved: false,
  isOutdated: false,
  path: "src/a.go",
  line: 12,
  comments: [{ author: "alice", createdAt: "2026-10-23T00:00:00Z", body: "please rename", bodyTruncated: false }],
  commentsTruncated: false,
  ...over,
});

export const check = (over: Partial<CheckItem> = {}): CheckItem => ({
  id: "run:1:job:2",
  name: "build",
  state: "failure",
  status: "completed",
  conclusion: "failure",
  url: "https://github.com/o/r/actions/runs/1/job/2",
  ...over,
});

export const prDetails = (over: Partial<PRDetails> = {}): PRDetails => ({
  taskId: 1,
  number: 7,
  repo: "o/r",
  url: "https://github.com/o/r/pull/7",
  title: "Add thing",
  state: "open",
  draft: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  body: "",
  bodyTruncated: false,
  checks: { state: "failure", passed: 0, failed: 1, pending: 0, total: 1, items: [check()], truncated: false },
  reviews: { approvals: 0, required: null, items: [], awaiting: [], truncated: false },
  threads: { items: [thread()], truncated: false },
  fetchedAt: "2026-10-23T00:00:00Z",
  stale: "",
  refreshAvailableAt: "",
  ...over,
});
