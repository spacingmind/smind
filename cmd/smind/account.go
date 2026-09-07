package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
		fmt.Fprintln(os.Stderr, "usage: smind account <add|ls> ...")
		return 2
	}
	switch args[0] {
	case "add":
		return cmdAccountAdd(args[1:])
	case "ls":
		return cmdAccountList(args[1:])
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
