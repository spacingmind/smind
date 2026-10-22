package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/wsclient"
)

const pushUsage = `Usage:
  smind push test [<device id prefix>]    send a test notification to the phones registered for push
`

// pushDial is a variable so tests can stub the daemon connection.
var pushDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

type pushTestOutcome struct {
	DeviceID string `json:"deviceId"`
	Sent     bool   `json:"sent"`
	Error    string `json:"error,omitempty"`
}

func cmdPush(args []string) int {
	return runPush(context.Background(), args, osStdout, osStderr)
}

// runPush is cmdPush with its environment injected. Exit codes: 0 every
// device was sent, 1 a failure or nothing to send to, 2 usage.
func runPush(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "test" || len(args) > 2 {
		fmt.Fprint(stderr, pushUsage)
		return 2
	}
	params := map[string]any{}
	if len(args) == 2 && args[1] != "" {
		params["deviceId"] = args[1]
	}

	call, closeFn, err := pushDial(ctx)
	if err != nil {
		if isNoDaemon(err) {
			fmt.Fprintln(stderr, "smind: no daemon is running (start it with `smind serve`)")
		} else {
			fmt.Fprintf(stderr, "smind: %v\n", err)
		}
		return 1
	}
	defer closeFn()

	var res struct {
		Results []pushTestOutcome `json:"results"`
	}
	if err := call(ctx, "push.test", params, &res); err != nil {
		fmt.Fprintf(stderr, "smind: %s\n", pushErrorText(err))
		return 1
	}
	if len(res.Results) == 0 {
		fmt.Fprintln(stderr, "No phone is registered for push. In the app: Connection -> Notifications -> Turn on.")
		return 1
	}

	tw := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "DEVICE\tRESULT")
	sent := 0
	for _, r := range res.Results {
		outcome := r.Error
		if r.Sent {
			outcome = "sent"
			sent++
		} else if outcome == "" {
			outcome = "failed"
		}
		fmt.Fprintf(tw, "%s\t%s\n", r.DeviceID, outcome)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d of %d sent\n", sent, len(res.Results))
	if sent == len(res.Results) {
		return 0
	}
	return 1
}

// pushErrorText maps a daemon error to a one-line explanation.
func pushErrorText(err error) string {
	var rpc *wsclient.RPCError
	if errors.As(err, &rpc) {
		switch rpc.Code {
		case "push_disabled":
			return "push notifications are disabled on this daemon (push.enabled / SMIND_PUSH)"
		case "not_found":
			return "no phone with that device id is registered for push"
		case "ambiguous":
			return "that device id prefix matches several devices; use more characters"
		case "invalid_params":
			return rpc.Message
		}
	}
	return err.Error()
}
