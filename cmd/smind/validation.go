package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

const cmdValidationUsage = `usage: smind validation <command>

  smind validation contract submit <taskId> <file> [--json]
                                    store <file> (YAML front matter + markdown) as the task's next contract version
  smind validation contract show <taskId> [--json]
                                    the task's current contract
  smind validation list <taskId> [--json]
                                    the task's validations, newest first, with the merge-ready label
  smind validation show <id> [--json]
                                    one validation: its verdict, evidence directory and merge-ready label

A contract is written BEFORE implementation (ADR-0045). "merge-ready" is a label for your decision: smind never merges.`

type validationIO struct {
	out, errOut io.Writer
	dial        func(context.Context) (daemonCaller, error)
	readFile    func(string) ([]byte, error)
}

func cmdValidation(args []string) int {
	return runValidation(args, validationIO{out: os.Stdout, errOut: os.Stderr, dial: dialDaemonCaller, readFile: os.ReadFile})
}

func runValidation(args []string, env validationIO) int {
	asJSON := false
	var rest []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	usage := func() int { fmt.Fprintln(env.errOut, cmdValidationUsage); return 2 }
	if len(rest) == 0 {
		return usage()
	}
	id := func(s string) (int64, bool) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			fmt.Fprintf(env.errOut, "validation: invalid id %q\n", s)
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
			fmt.Fprintf(env.errOut, "validation: %v\n", err)
			return nil, 1
		}
		if m, ok := out.(map[string]any); ok {
			return m, 0
		}
		return map[string]any{"items": out}, 0
	}
	emit := func(v any) { b, _ := marshalIndent(v); fmt.Fprintln(env.out, string(b)) }

	switch {
	case len(rest) == 4 && rest[0] == "contract" && rest[1] == "submit":
		task, ok := id(rest[2])
		if !ok {
			return 2
		}
		src, err := env.readFile(rest[3])
		if err != nil {
			fmt.Fprintf(env.errOut, "validation: %v\n", err)
			return 1
		}
		res, code := call("validation.contract.submit", map[string]any{"taskId": task, "source": "inline", "body": string(src)})
		if code != 0 {
			return code
		}
		if asJSON {
			emit(res)
		} else {
			fmt.Fprintf(env.out, "contract %v stored: task %v version %v sha256 %v\n", res["id"], res["taskId"], res["version"], res["sha256"])
		}
	case len(rest) == 3 && rest[0] == "contract" && rest[1] == "show":
		task, ok := id(rest[2])
		if !ok {
			return 2
		}
		res, code := call("validation.contract.get", map[string]any{"taskId": task})
		if code != 0 {
			return code
		}
		emit(res)
	case len(rest) == 2 && rest[0] == "list":
		task, ok := id(rest[1])
		if !ok {
			return 2
		}
		res, code := call("validation.list", map[string]any{"taskId": task})
		if code != 0 {
			return code
		}
		items, _ := res["items"].([]any)
		if asJSON {
			emit(items)
			return 0
		}
		if len(items) == 0 {
			fmt.Fprintln(env.out, "no validations")
		}
		for _, it := range items {
			m, _ := it.(map[string]any)
			fmt.Fprintln(env.out, validationLine(m))
		}
	case len(rest) == 2 && rest[0] == "show":
		n, ok := id(rest[1])
		if !ok {
			return 2
		}
		res, code := call("validation.get", map[string]any{"id": n})
		if code != 0 {
			return code
		}
		if asJSON {
			emit(res)
			return 0
		}
		fmt.Fprintln(env.out, validationLine(res))
		if v, ok := res["verdict"].(map[string]any); ok {
			fmt.Fprintf(env.out, "  summary: %v\n", v["summary"])
			if r, _ := v["blockedReason"].(string); r != "" {
				fmt.Fprintf(env.out, "  blocked: %s\n", r)
			}
		}
		if d, _ := res["evidenceDir"].(string); d != "" {
			fmt.Fprintf(env.out, "  evidence: %s\n", d)
		}
	default:
		return usage()
	}
	return 0
}

func validationLine(m map[string]any) string {
	sha, _ := m["commitSha"].(string)
	if len(sha) > 8 {
		sha = sha[:8]
	}
	ready := "not merge-ready"
	if r, _ := m["mergeReady"].(bool); r {
		ready = "merge-ready"
	} else if why, _ := m["notReadyReason"].(string); why != "" {
		ready += " (" + why + ")"
	}
	return fmt.Sprintf("validation %v · task %v · %v · commit %s · contract v%v · %s", m["id"], m["taskId"], m["status"], sha, m["contractVersion"], ready)
}

func marshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
