package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func treeTestNode(id int64, depth int, title string, tier *string, state string) taskTreeNode {
	return taskTreeNode{ID: id, Depth: depth, Title: title, Tier: tier, Run: taskTreeNodeRun{State: state}}
}

func TestRenderTaskTree(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	resetAt := now.Add(2*time.Hour + 3*time.Minute)
	str := func(s string) *string { return &s }
	usage := json.RawMessage(`{"subtree":{"costUsd":0.42}}`)
	nullUsage := json.RawMessage(`{"subtree":{"costUsd":null}}`)

	tests := []struct {
		name  string
		flat  taskTreeFlat
		lines []string
	}{
		{
			name:  "empty tree",
			flat:  taskTreeFlat{},
			lines: []string{"0 tasks"},
		},
		{
			name: "nesting, tier dash, cost",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				func() taskTreeNode {
					n := treeTestNode(1, 0, "Root", str("supervisor"), "running")
					n.Usage = usage
					return n
				}(),
				func() taskTreeNode {
					n := treeTestNode(2, 1, "Child", nil, "done")
					n.Usage = nullUsage
					return n
				}(),
				treeTestNode(3, 2, "Grandchild", str("coder"), "idle"),
			}},
			lines: []string{
				"3 tasks: 1 running, 0 waiting, 0 failed, 1 done",
				"#1 Root  supervisor  running  $0.42",
				"  #2 Child  -  done  cost n/a",
				"    #3 Grandchild  coder  idle  cost n/a",
			},
		},
		{
			name: "single task singular header",
			flat: taskTreeFlat{Nodes: []taskTreeNode{treeTestNode(7, 0, "Solo", str("leader"), "waiting_permission")}},
			lines: []string{
				"1 task: 0 running, 1 waiting, 0 failed, 0 done",
				"#7 Solo  leader  waiting_permission  cost n/a",
			},
		},
		{
			name: "failures and needs-continue",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				func() taskTreeNode {
					n := treeTestNode(1, 0, "Limited", str("coder"), "error")
					n.Run.FailureKind = "rate_limited"
					n.Run.Window = "5h"
					n.Run.ResetAt = resetAt.Format(time.RFC3339)
					return n
				}(),
				func() taskTreeNode {
					n := treeTestNode(2, 0, "Crashed", str("coder"), "error")
					n.Run.FailureKind = "crash"
					return n
				}(),
				treeTestNode(3, 0, "Plain", str("coder"), "error"),
				func() taskTreeNode {
					n := treeTestNode(4, 0, "Cut", str("coder"), "done")
					n.Run.Disposition = "needs-continue"
					return n
				}(),
			}},
			lines: []string{
				"4 tasks: 0 running, 0 waiting, 3 failed, 1 done",
				"#1 Limited  coder  error: " + rateLimitedText("5h", &resetAt, now) + "  cost n/a",
				"#2 Crashed  coder  error: crash  cost n/a",
				"#3 Plain  coder  error  cost n/a",
				"#4 Cut  coder  done (needs-continue)  cost n/a",
			},
		},
		{
			name: "unparseable resetAt",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				func() taskTreeNode {
					n := treeTestNode(1, 0, "Limited", nil, "error")
					n.Run.FailureKind = "rate_limited"
					n.Run.ResetAt = "garbage"
					return n
				}(),
			}},
			lines: []string{
				"1 task: 0 running, 0 waiting, 1 failed, 0 done",
				"#1 Limited  -  error: rate limited, reset time unknown  cost n/a",
			},
		},
		{
			name: "title truncation and newlines",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				treeTestNode(1, 0, strings.Repeat("é", 60), nil, "idle"),
				treeTestNode(2, 0, "two\nlines", nil, "idle"),
			}},
			lines: []string{
				"2 tasks: 0 running, 0 waiting, 0 failed, 0 done",
				"#1 " + strings.Repeat("é", 47) + "…  -  idle  cost n/a",
				"#2 two lines  -  idle  cost n/a",
			},
		},
		{
			name: "permissions singular and plural",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				func() taskTreeNode {
					n := treeTestNode(1, 0, "One", nil, "waiting_permission")
					n.Run.PendingPermissions = 1
					return n
				}(),
				func() taskTreeNode {
					n := treeTestNode(2, 0, "Two", nil, "waiting_permission")
					n.Run.PendingPermissions = 2
					return n
				}(),
			}},
			lines: []string{
				"2 tasks: 0 running, 2 waiting, 0 failed, 0 done",
				"#1 One  -  waiting_permission  1 permission  cost n/a",
				"#2 Two  -  waiting_permission  2 permissions  cost n/a",
			},
		},
		{
			name: "truncated note",
			flat: taskTreeFlat{Truncated: true, Nodes: []taskTreeNode{treeTestNode(1, 0, "A", nil, "done")}},
			lines: []string{
				"1 task: 0 running, 0 waiting, 0 failed, 1 done",
				"#1 A  -  done  cost n/a",
				"(tree truncated by the daemon; narrow it with --task)",
			},
		},
		{
			name: "subtree root at depth 2 renders at indent 0",
			flat: taskTreeFlat{Nodes: []taskTreeNode{
				treeTestNode(5, 2, "Sub root", nil, "running"),
				treeTestNode(6, 3, "Sub child", nil, "running"),
			}},
			lines: []string{
				"2 tasks: 2 running, 0 waiting, 0 failed, 0 done",
				"#5 Sub root  -  running  cost n/a",
				"  #6 Sub child  -  running  cost n/a",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderTaskTree(&buf, tt.flat, now)
			want := strings.Join(tt.lines, "\n") + "\n"
			if got := buf.String(); got != want {
				t.Fatalf("renderTaskTree() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}
