package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
)

// accountResult is the credential-free account metadata returned by the
// account WebSocket methods. Keeping this separate from store.Account avoids
// ever decoding credential_data into the CLI response path.
type accountResult struct {
	ID             int64  `json:"id"`
	Provider       string `json:"provider"`
	Label          string `json:"label"`
	CredentialType string `json:"credentialType"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func cmdAccount(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind account <add|ls|login> ...")
		return 2
	}
	switch args[0] {
	case "add":
		return cmdAccountAdd(args[1:])
	case "ls":
		return cmdAccountList(args[1:])
	case "login":
		return cmdAccountLogin(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind account: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdAccountAdd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: smind account add <provider> <label> < credential")
		return 2
	}
	credential, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "account add: read credential: %v\n", err)
		return 1
	}
	if strings.TrimSpace(string(credential)) == "" {
		fmt.Fprintln(os.Stderr, "account add: credential from stdin is empty")
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var account accountResult
	err = client.Call(context.Background(), "account.add", map[string]any{
		"provider": args[0], "label": args[1], "credential": string(credential),
	}, &account)
	if err != nil {
		fmt.Fprintf(os.Stderr, "account add: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\n", account.ID, account.Provider, account.Label, account.CredentialType)
	return 0
}

func cmdAccountList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind account ls")
		return 2
	}
	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var accounts []accountResult
	if err := client.Call(context.Background(), "account.list", nil, &accounts); err != nil {
		fmt.Fprintf(os.Stderr, "account ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPROVIDER\tLABEL\tCREDENTIALTYPE\tCREATED\tUPDATED")
	for _, account := range accounts {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", account.ID, account.Provider, account.Label,
			account.CredentialType, account.CreatedAt, account.UpdatedAt)
	}
	tw.Flush()
	return 0
}

// authorizeURLEventParams is the params payload of the "authorizeUrl" event
// account.oauthStart emits, mirroring internal/wsapi/handlers.go's
// handleAccountOAuthStart.
type authorizeURLEventParams struct {
	URL string `json:"url"`
}

// cmdAccountLogin drives a browser-based OAuth login through the daemon
// (account.oauthStart): it blocks until the daemon reports the login done,
// printing the authorize URL as soon as the daemon has one (always -- the
// daemon may be headless or reached over SSH, so stdout is the one channel
// guaranteed to reach whoever's driving this) and best-effort opening it in
// a local browser. Ctrl+C cancels the wait the same way `task send`/`task
// attach` do (see streamRun); the daemon itself tears down the OAuth
// callback listener however this request ends, so a Ctrl+C here doesn't
// leak it.
func cmdAccountLogin(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: smind account login <provider> <label>")
		return 2
	}
	provider, label := args[0], args[1]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var account accountResult
	err = client.CallStream(ctx, "account.oauthStart", map[string]any{
		"provider": provider, "label": label,
	}, func(event string, params json.RawMessage) {
		if event != "authorizeUrl" {
			return
		}
		var p authorizeURLEventParams
		if err := json.Unmarshal(params, &p); err != nil || p.URL == "" {
			return
		}
		fmt.Printf("Open this URL to log in:\n%s\n", p.URL)
		openBrowser(p.URL)
	}, &account)
	if err != nil {
		fmt.Fprintf(os.Stderr, "account login: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\n", account.ID, account.Provider, account.Label, account.CredentialType)
	return 0
}

// openBrowser best-effort opens url in the local system browser. Failure is
// silent (beyond os.Stderr) rather than fatal: the URL was already printed,
// so a headless daemon or a missing/misconfigured browser opener doesn't
// break the login flow, just its convenience.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "account login: could not auto-open browser: %v\n", err)
	}
}
