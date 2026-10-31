package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// cmdStewardUsage ends with the one rule the steward surface lives under
// (ADR-0047 §9): a review is advice for the Human, who reads it and merges.
const cmdStewardUsage = `usage: smind steward <command>

  smind steward review <taskId> [--sha <40 hex>] [--json]
                                    ask for a read-only review of the task's head commit (or the given sha)
  smind steward list [<taskId>] [--limit N] [--json]
                                    the reviews, newest first, optionally one task's only
  smind steward show <reviewId> [--json]
                                    one review: its verdict, summary and gates
  smind steward cancel <reviewId> [--json]
                                    stop an open review

A review is advice: smind never merges; you read it and you merge.`

type stewardIO struct {
	out, errOut io.Writer
	dial        func(context.Context) (daemonCaller, error)
}

func cmdSteward(args []string) int {
	return runSteward(args, stewardIO{out: os.Stdout, errOut: os.Stderr, dial: dialDaemonCaller})
}

var stewardShaRe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func runSteward(args []string, env stewardIO) int {
	usage := func() int { fmt.Fprintln(env.errOut, cmdStewardUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	sub, rest := args[0], args[1:]
	asJSON, sha := false, ""
	var limit int64
	var pos []string
	for i := 0; i < len(rest); i++ {
		switch a := rest[i]; a {
		case "--json":
			asJSON = true
		case "--sha":
			i++
			if i >= len(rest) {
				fmt.Fprintln(env.errOut, "steward: --sha needs a 40-hex commit sha")
				return 2
			}
			sha = rest[i]
		case "--limit":
			i++
			if i >= len(rest) {
				fmt.Fprintln(env.errOut, "steward: --limit needs a positive number")
				return 2
			}
			n, err := strconv.ParseInt(rest[i], 10, 64)
			if err != nil || n <= 0 {
				fmt.Fprintf(env.errOut, "steward: invalid limit %q\n", rest[i])
				return 2
			}
			limit = n
		default:
			if strings.HasPrefix(a, "--") {
				return usage()
			}
			pos = append(pos, a)
		}
	}
	idArg := func(s, what string) (int64, bool) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			fmt.Fprintf(env.errOut, "steward: invalid %s %q\n", what, s)
			return 0, false
		}
		return n, true
	}
	call := func(method string, params any) (map[string]any, int) {
		c, err := env.dial(context.Background())
		if err != nil {
			fmt.Fprintln(env.errOut, err)
			return nil, 1
		}
		defer c.Close()
		var out any
		if err := c.Call(context.Background(), method, params, &out); err != nil {
			fmt.Fprintf(env.errOut, "steward: %v\n", err)
			return nil, 1
		}
		if m, ok := out.(map[string]any); ok {
			return m, 0
		}
		return map[string]any{"items": out}, 0
	}
	emit := func(v any) { b, _ := marshalIndent(v); fmt.Fprintln(env.out, string(b)) }
	line := func(res map[string]any) {
		if asJSON {
			emit(res)
			return
		}
		fmt.Fprintln(env.out, stewardLine(res))
	}

	switch sub {
	case "review":
		if len(pos) != 1 {
			return usage()
		}
		task, ok := idArg(pos[0], "taskId")
		if !ok {
			return 2
		}
		params := map[string]any{"taskId": task}
		if sha != "" {
			if !stewardShaRe.MatchString(sha) {
				fmt.Fprintf(env.errOut, "steward: invalid sha %q (want 40 hex)\n", sha)
				return 2
			}
			params["commitSha"] = sha
		}
		res, code := call("steward.review", params)
		if code != 0 {
			return code
		}
		line(res)
	case "list":
		if len(pos) > 1 || sha != "" {
			return usage()
		}
		params := map[string]any{}
		if len(pos) == 1 {
			task, ok := idArg(pos[0], "taskId")
			if !ok {
				return 2
			}
			params["taskId"] = task
		}
		if limit > 0 {
			params["limit"] = limit
		}
		res, code := call("steward.list", params)
		if code != 0 {
			return code
		}
		items, _ := res["items"].([]any)
		if asJSON {
			emit(items)
			return 0
		}
		if len(items) == 0 {
			fmt.Fprintln(env.out, "no reviews")
		}
		for _, it := range items {
			m, _ := it.(map[string]any)
			fmt.Fprintln(env.out, stewardLine(m))
		}
	case "show", "cancel":
		if len(pos) != 1 || sha != "" || limit > 0 {
			return usage()
		}
		rid, ok := idArg(pos[0], "reviewId")
		if !ok {
			return 2
		}
		method := "steward.get"
		if sub == "cancel" {
			method = "steward.cancel"
		}
		res, code := call(method, map[string]any{"reviewId": rid})
		if code != 0 {
			return code
		}
		line(res)
		if sub == "show" && !asJSON {
			if s, _ := res["summary"].(string); s != "" {
				fmt.Fprintf(env.out, "  summary: %s\n", s)
			}
			if gates, ok := res["gates"].([]any); ok {
				for _, g := range gates {
					gm, _ := g.(map[string]any)
					fmt.Fprintf(env.out, "  gate %v:%v\n", gm["id"], gm["state"])
				}
			}
		}
	default:
		return usage()
	}
	return 0
}

// stewardLine renders one review as `#<id> task <taskId> <verdict or status>
// head <head7> <stale-or-fresh> mergeReady=<bool>`. A missing field prints as
// `-`, so an older daemon's result shape does not crash the CLI.
func stewardLine(m map[string]any) string {
	f := func(k string) string {
		v, ok := m[k]
		if !ok || v == nil {
			return "-"
		}
		s, isStr := v.(string)
		if isStr && s == "" {
			return "-"
		}
		if k == "headSha" && len(s) > 7 {
			return s[:7]
		}
		return fmt.Sprintf("%v", v)
	}
	what := f("verdict")
	if what == "-" {
		what = f("status")
	}
	stale := "fresh"
	if s, _ := m["stale"].(string); s != "" {
		stale = "stale (" + s + ")"
	}
	return fmt.Sprintf("#%s task %s %s head %s %s mergeReady=%s", f("id"), f("taskId"), what, f("headSha"), stale, f("mergeReady"))
}
