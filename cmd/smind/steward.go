package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// cmdStewardUsage ends with the one rule the steward surface lives under
// (ADR-0047 §9): a review is advice for the Human, who reads it and merges.
const cmdStewardUsage = `usage: smind steward <command>

  smind steward review <taskId> [--sha <40 hex>] [--axis standards|spec] [--json]
                                    ask for a read-only review of the task's head commit (or the given sha)
                                    --axis re-runs only the named pass; the other axis is reported as not run and the review can never be accepted
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
	var axes []string
	var pos []string
	for i := 0; i < len(rest); i++ {
		switch a := rest[i]; a {
		case "--json":
			asJSON = true
		case "--axis":
			i++
			if i >= len(rest) {
				fmt.Fprintln(env.errOut, "steward: --axis needs standards or spec")
				return 2
			}
			switch rest[i] {
			case "standards", "spec":
			default:
				fmt.Fprintf(env.errOut, "steward: invalid axis %q (want standards or spec)\n", rest[i])
				return 2
			}
			if !slices.Contains(axes, rest[i]) {
				axes = append(axes, rest[i])
			}
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
		if len(axes) > 0 {
			params["axes"] = axes
		}
		res, code := call("steward.review", params)
		if code != 0 {
			return code
		}
		line(res)
	case "list":
		if len(pos) > 1 || sha != "" || len(axes) > 0 {
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
		if len(pos) != 1 || sha != "" || limit > 0 || len(axes) > 0 {
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
				fmt.Fprintf(env.out, "  summary: %s\n", stewardText(s))
			}
			if gates, ok := res["gates"].([]any); ok {
				for _, g := range gates {
					gm, _ := g.(map[string]any)
					fmt.Fprintf(env.out, "  gate %v:%v\n", gm["id"], gm["state"])
				}
			}
			if passes, ok := res["axes"].([]any); ok && len(passes) > 0 {
				stewardPrintAxes(env.out, res, passes)
			}
		}
	default:
		return usage()
	}
	return 0
}

// stewardPrintAxes renders a two-axis review's extra blocks for `smind
// steward show`: one line per pass (its verdict or status, plus the model),
// the spec criteria under the spec pass, then the findings tagged by axis.
// Cost is never printed (ADR-0054: cost shows only in the Usage overview),
// and every field is read tolerantly: a missing field prints `-`, a
// malformed element prints nothing.
func stewardPrintAxes(w io.Writer, res map[string]any, axes []any) {
	fmt.Fprintln(w, "  axes:")
	for _, a := range axes {
		am, _ := a.(map[string]any)
		if am == nil {
			continue
		}
		fmt.Fprintf(w, "    %s: %s%s\n", stewardField(am, "axis"), stewardAxisState(am), stewardAxisModel(am))
		if ax, _ := am["axis"].(string); ax == "spec" {
			for _, c := range stewardSlice(am["criteria"]) {
				cm, _ := c.(map[string]any)
				if cm == nil {
					continue
				}
				fmt.Fprintf(w, "      %s %s: %s\n", stewardField(cm, "id"), stewardField(cm, "status"), stewardEvidence(cm["evidence"]))
			}
		}
	}
	fmt.Fprintln(w, "  findings:")
	for _, f := range stewardSlice(res["findings"]) {
		fm, _ := f.(map[string]any)
		if fm == nil {
			continue
		}
		l := fmt.Sprintf("    [%s] %s:%s [%s] %s (check: %s)",
			stewardField(fm, "axis"), stewardField(fm, "file"), stewardField(fm, "line"),
			stewardField(fm, "severity"), stewardField(fm, "why"), stewardField(fm, "check"))
		if r := stewardField(fm, "rule"); r != "-" {
			l += " rule: " + r
		}
		fmt.Fprintln(w, l)
	}
}

// stewardAxisState words a pass's state: a done pass shows its seat verdict
// in lowercase, a skipped pass says why when it can, a blocked pass names
// the reason it died; anything else shows the raw status.
func stewardAxisState(am map[string]any) string {
	status, _ := am["status"].(string)
	reason, _ := am["blockedReason"].(string)
	switch status {
	case "done":
		if v, _ := am["seatVerdict"].(string); v != "" {
			return strings.ToLower(v)
		}
		return "-"
	case "skipped":
		if reason == "no_spec" {
			return "skipped (no spec available)"
		}
		return "skipped"
	case "blocked":
		if reason == "" {
			reason = "-"
		}
		return "did not complete (" + reason + ")"
	}
	if status == "" {
		return "-"
	}
	return status
}

func stewardAxisModel(am map[string]any) string {
	if m := stewardField(am, "model"); m != "-" {
		return " model " + m
	}
	return ""
}

// stewardText makes seat- and PR-derived text safe to print: tabs and line
// breaks become spaces (so a value cannot forge another output line) and every
// other control character, ESC included, is dropped (so it cannot move the
// cursor or inject ANSI).
func stewardText(s string) string {
	return sanitizeForTerminal(strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s))
}

// stewardField reads a result field the way stewardLine does: a missing,
// nil or empty one prints as `-`. The text is made terminal-safe.
func stewardField(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return "-"
	}
	if s, isStr := v.(string); isStr && s == "" {
		return "-"
	}
	return stewardText(fmt.Sprintf("%v", v))
}

// stewardEvidence flattens criterion evidence to one line of at most 120
// runes.
func stewardEvidence(v any) string {
	s, _ := v.(string)
	if s == "" {
		return "-"
	}
	r := []rune(stewardText(s))
	if len(r) > 120 {
		r = r[:120]
	}
	return string(r)
}

func stewardSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// stewardLine renders one review as `#<id> task <taskId> <verdict or status>
// head <head7> <stale-or-fresh> mergeReady=<bool>`. A missing field prints as
// `-`, so an older daemon's result shape does not crash the CLI. The start
// result's `reviewId`/`commitSha` are read as id/headSha aliases.
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
		if (k == "headSha" || k == "commitSha") && len(s) > 7 {
			return s[:7]
		}
		return fmt.Sprintf("%v", v)
	}
	pick := func(k, alt string) string {
		if s := f(k); s != "-" {
			return s
		}
		return f(alt)
	}
	stale := "fresh"
	if s, _ := m["stale"].(string); s != "" {
		stale = "stale (" + s + ")"
	}
	return fmt.Sprintf("#%s task %s %s head %s %s mergeReady=%s",
		pick("id", "reviewId"), f("taskId"), pick("verdict", "status"), pick("headSha", "commitSha"), stale, f("mergeReady"))
}
