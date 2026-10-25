package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// cmdTaskCommitMessageUsage is printed on any argument error in cmdTaskCommitMessage.
const cmdTaskCommitMessageUsage = "usage: smind task commit-message <taskId>"

// commitDraftView is the task.draftCommitMessage result (ADR-0043).
type commitDraftView struct {
	Message   string   `json:"message"`
	Model     string   `json:"model"`
	Truncated bool     `json:"truncated"`
	Omitted   []string `json:"omitted"`
}

// cmdTaskCommitMessage prints an AI-drafted commit message for the task's
// STAGED diff to stdout. Notes go to stderr. It never stages or commits.
func cmdTaskCommitMessage(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, cmdTaskCommitMessageUsage)
		return 2
	}
	id, err := parseInt64(args[0])
	if err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "task commit-message: invalid taskId %q\n", args[0])
		fmt.Fprintln(os.Stderr, cmdTaskCommitMessageUsage)
		return 2
	}
	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var res commitDraftView
	if err := client.Call(context.Background(), "task.draftCommitMessage", map[string]any{"taskId": id}, &res); err != nil {
		fmt.Fprintf(os.Stderr, "task commit-message: %v\n", err)
		return 1
	}
	fmt.Println(res.Message)
	if res.Truncated {
		fmt.Fprintln(os.Stderr, "note: the staged diff was truncated to fit the budget")
	}
	if len(res.Omitted) > 0 {
		fmt.Fprintf(os.Stderr, "note: sent by name only (likely secrets): %s\n", strings.Join(res.Omitted, ", "))
	}
	return 0
}
