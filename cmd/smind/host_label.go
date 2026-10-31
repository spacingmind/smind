package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// hostLabelsResult is the fleet.host.labels answer.
type hostLabelsResult struct {
	Host   string            `json:"host"`
	HostID string            `json:"hostId"`
	Labels map[string]string `json:"labels"`
}

// parseLabelPair splits k:v at the first colon; both sides must be non-empty.
func parseLabelPair(s string) (k, v string, ok bool) {
	k, v, found := strings.Cut(s, ":")
	if !found || k == "" || v == "" {
		return "", "", false
	}
	return k, v, true
}

// formatLabelLines renders labels as sorted `k:v` strings.
func formatLabelLines(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for k, v := range labels {
		out = append(out, hostSafeText(k)+":"+hostSafeText(v))
	}
	sort.Strings(out)
	return out
}

func runHostLabel(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "set", "rm":
		if len(rest) < 2 || rest[0] == "" {
			return usage()
		}
		host := rest[0]
		params := map[string]any{"host": host}
		if sub == "set" {
			set := map[string]string{}
			for _, p := range rest[1:] {
				k, v, ok := parseLabelPair(p)
				if !ok {
					fmt.Fprintf(stderr, "smind host label set: %q is not a k:v pair\n", p)
					return usage()
				}
				set[k] = v
			}
			params["set"] = set
		} else {
			remove := []string{}
			for _, k := range rest[1:] {
				if k == "" {
					return usage()
				}
				remove = append(remove, k)
			}
			params["remove"] = remove
		}
		call, closeFn, ok := openHost(ctx, stderr)
		if !ok {
			return 1
		}
		defer closeFn()
		var res hostLabelsResult
		if err := call(ctx, "fleet.host.labels", params, &res); err != nil {
			fmt.Fprintf(stderr, "smind host label %s: %s\n", sub, hostErrMessage(err))
			return 1
		}
		name := res.Host
		if name == "" {
			name = host
		}
		fmt.Fprintf(stdout, "%s labels: %s\n", hostSafeText(name), labelsOrNone(res.Labels))
		return 0
	case "ls":
		return runHostLabelLs(ctx, rest, stdout, stderr)
	default:
		return usage()
	}
}

func labelsOrNone(labels map[string]string) string {
	if len(labels) == 0 {
		return "(no labels)"
	}
	return strings.Join(formatLabelLines(labels), ",")
}

func runHostLabelLs(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	positional, args := hostSplitArgs(args)
	fs := flag.NewFlagSet("host label ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print the daemon answer as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || len(positional) > 1 || (len(positional) == 1 && positional[0] == "") {
		fmt.Fprint(stderr, hostUsage)
		return 2
	}
	call, closeFn, ok := openHost(ctx, stderr)
	if !ok {
		return 1
	}
	defer closeFn()

	if len(positional) == 1 {
		var raw json.RawMessage
		if err := call(ctx, "fleet.host.labels", map[string]any{"host": positional[0]}, &raw); err != nil {
			fmt.Fprintf(stderr, "smind host label ls: %s\n", hostErrMessage(err))
			return 1
		}
		if *jsonOut {
			fmt.Fprintln(stdout, string(raw))
			return 0
		}
		var res hostLabelsResult
		if err := json.Unmarshal(raw, &res); err != nil {
			fmt.Fprintf(stderr, "smind host label ls: bad answer: %v\n", err)
			return 1
		}
		if len(res.Labels) == 0 {
			fmt.Fprintln(stdout, "(no labels)")
			return 0
		}
		for _, l := range formatLabelLines(res.Labels) {
			fmt.Fprintln(stdout, l)
		}
		return 0
	}

	var raw json.RawMessage
	if err := call(ctx, "fleet.host.list", nil, &raw); err != nil {
		fmt.Fprintf(stderr, "smind host label ls: %s\n", hostErrMessage(err))
		return 1
	}
	if *jsonOut {
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	var res struct {
		Hosts []struct {
			Alias  string            `json:"alias"`
			HostID string            `json:"hostId"`
			Labels map[string]string `json:"labels"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		fmt.Fprintf(stderr, "smind host label ls: bad answer: %v\n", err)
		return 1
	}
	if len(res.Hosts) == 0 {
		fmt.Fprintln(stdout, "(no hosts)")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tLABELS")
	for _, h := range res.Hosts {
		alias := h.Alias
		if alias == "" {
			alias = shortHostID(h.HostID)
		}
		labels := "-"
		if len(h.Labels) > 0 {
			labels = strings.Join(formatLabelLines(h.Labels), ",")
		}
		fmt.Fprintf(tw, "%s\t%s\n", hostSafeText(alias), labels)
	}
	_ = tw.Flush()
	return 0
}
