package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
)

// runPermissionGrants is `smind permission grants ls|show|revoke`: the human's
// view of saved grants (docs/plans/active/saved-grants.md). The whole
// `smind permission` group is human-only (permpolicy.HumanOnlyCLI), so an agent
// shell cannot run these.
func runPermissionGrants(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, cmdPermissionUsage)
		return 2
	}
	verb := args[0]
	a, err := parseLearnArgs(args[1:], []string{"--project"}, []string{"--json"})
	if err != nil {
		fmt.Fprintf(stderr, "smind permission grants %s: %v\n%s\n", verb, err, cmdPermissionUsage)
		return 2
	}
	method, params := "permission.grants.list", map[string]any{}
	switch verb {
	case "ls":
		if len(a.pos) != 0 {
			fmt.Fprintf(stderr, "smind permission grants ls: unexpected %q\n%s\n", a.pos[0], cmdPermissionUsage)
			return 2
		}
		if v := a.vals["--project"]; v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 {
				fmt.Fprintf(stderr, "smind permission grants ls: --project must be a workspace id\n")
				return 2
			}
			params["workspaceId"] = id
		}
	case "show", "revoke":
		id, ok := grantIDArg(a.pos)
		if !ok {
			fmt.Fprintf(stderr, "smind permission grants %s: give one grant id\n%s\n", verb, cmdPermissionUsage)
			return 2
		}
		if verb == "show" {
			params["grantId"] = id
		} else {
			method, params = "permission.grants.revoke", map[string]any{"grantId": id}
		}
	default:
		fmt.Fprintf(stderr, "smind permission grants: unknown verb %q\n%s\n", verb, cmdPermissionUsage)
		return 2
	}

	call, closeFn, err := policyDial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closeFn()
	var raw json.RawMessage
	if err := call(ctx, method, params, &raw); err != nil {
		fmt.Fprintf(stderr, "smind permission grants %s: %v\n", verb, err)
		return 1
	}
	if a.bools["--json"] {
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") != nil {
			buf.Reset()
			buf.Write(raw)
		}
		fmt.Fprintln(stdout, buf.String())
		return 0
	}
	if verb == "revoke" {
		var res struct {
			Revoked bool `json:"revoked"`
		}
		_ = json.Unmarshal(raw, &res)
		if res.Revoked {
			fmt.Fprintf(stdout, "grant %s revoked; its command asks again from the next request\n", a.pos[0])
		} else {
			fmt.Fprintf(stdout, "grant %s was already revoked\n", a.pos[0])
		}
		return 0
	}
	var res struct {
		Grants []struct {
			ID         int64  `json:"id"`
			Workspace  int64  `json:"workspaceId"`
			Command    string `json:"command"`
			Folder     string `json:"folder"`
			CreatedAt  string `json:"createdAt"`
			ExpiresAt  string `json:"expiresAt"`
			CreatedBy  string `json:"createdBy"`
			UseCount   int    `json:"useCount"`
			Status     string `json:"status"`
			RevokedBy  string `json:"revokedBy"`
			LastUsedAt string `json:"lastUsedAt"`
		} `json:"grants"`
		Audit []struct {
			At        string `json:"at"`
			Kind      string `json:"kind"`
			Actor     string `json:"actor"`
			RunID     string `json:"runId"`
			RequestID string `json:"requestId"`
			Note      string `json:"note"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind permission grants %s: decode response: %v\n", verb, err)
		return 1
	}
	if len(res.Grants) == 0 {
		fmt.Fprintln(stdout, "no saved grants")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPROJECT\tEXPIRES\tUSES\tCOMMAND (exact)\tFOLDER")
	for _, g := range res.Grants {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%d\t%s\t%s\n", g.ID, g.Status, g.Workspace, g.ExpiresAt, g.UseCount, sanitizeForTerminal(g.Command), sanitizeForTerminal(g.Folder))
	}
	_ = tw.Flush()
	for _, r := range res.Audit {
		fmt.Fprintf(stdout, "  %s  %-16s %s %s %s\n", r.At, r.Kind, r.Actor, r.RunID, sanitizeForTerminal(r.Note))
	}
	return 0
}

func grantIDArg(pos []string) (int64, bool) {
	if len(pos) != 1 {
		return 0, false
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	return id, err == nil && id > 0
}
