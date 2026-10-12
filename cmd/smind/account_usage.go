package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// osStderr is os.Stdout's stderr sibling (see osStdout in version.go):
// the command wrappers print usage errors to it, tests swap it.
var osStderr io.Writer = os.Stderr

// accountUsageEntry and accountUsageResponse mirror the `account.usage`
// wire result (PR-C wire contract): only the keys the daemon sends, no
// credential material exists on this wire at all. The nested report
// reuses quota.Report, whose JSON shape is the stored/transported one.
type accountUsageEntry struct {
	AccountID      int64         `json:"accountId"`
	Key            string        `json:"key"`
	Provider       string        `json:"provider"`
	DisplayName    string        `json:"displayName"`
	CredentialType string        `json:"credentialType"`
	Stale          bool          `json:"stale"`
	NextRefreshAt  *string       `json:"nextRefreshAt"`
	RefreshBlocked *string       `json:"refreshBlocked"`
	InFlight       bool          `json:"inFlight"`
	LastError      *string       `json:"lastError"`
	Report         *quota.Report `json:"report"`
}

type accountUsageResponse struct {
	Enabled    bool                `json:"enabled"`
	TTLSeconds int                 `json:"ttlSeconds"`
	Reports    []accountUsageEntry `json:"reports"`
}

// wsCaller is the slice of *wsclient.Client the command needs: one
// request/response round trip.
type wsCaller func(ctx context.Context, method string, params any, result any) error

// accountUsageDial is a variable so tests can stub the daemon connection
// without ever starting a real daemon or touching ~/.smind.
var accountUsageDial = func(ctx context.Context) (wsCaller, error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, err
	}
	return client.Call, nil
}

func cmdAccountUsage(args []string) int {
	refresh, jsonOut := false, false
	for _, arg := range args {
		switch arg {
		case "--refresh":
			refresh = true
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(osStderr, "usage: smind account usage [--refresh] [--json]")
			return 2
		}
	}
	return runAccountUsage(context.Background(), accountUsageDial, refresh, jsonOut, time.Now(), osStdout, osStderr)
}

// runAccountUsage is the testable core of `smind account usage`: dial is
// injected so unit tests never need a daemon, and now is injected so
// countdowns are deterministic.
func runAccountUsage(ctx context.Context, dial func(context.Context) (wsCaller, error), refresh, jsonOut bool, now time.Time, stdout, stderr io.Writer) int {
	call, err := dial(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	mode := "touch"
	if refresh {
		mode = "refresh"
	}
	var result accountUsageResponse
	if err := call(ctx, "account.usage", map[string]any{"mode": mode}, &result); err != nil {
		fmt.Fprintf(stderr, "account usage: %v\n", err)
		return 1
	}

	if jsonOut {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "account usage: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	renderAccountUsage(stdout, &result, refresh, now)
	return 0
}

func renderAccountUsage(w io.Writer, result *accountUsageResponse, refresh bool, now time.Time) {
	fmt.Fprint(w, "")
}
