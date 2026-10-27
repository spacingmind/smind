package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/store"
)

func cmdWorkspace(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace <create|ls|setup|readiness> ...")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdWorkspaceCreate(args[1:])
	case "ls":
		return cmdWorkspaceList(args[1:])
	case "setup":
		return cmdWorkspaceSetup(args[1:])
	case "readiness":
		return cmdWorkspaceReadiness(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "smind workspace: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdWorkspaceCreate(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace create <repoPath> <name> <policy>")
		return 2
	}
	repoPath, name, policy := args[0], args[1], args[2]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var ws store.Workspace
	err = client.Call(context.Background(), "workspace.create", map[string]any{
		"path": repoPath, "title": name, "routingPolicy": policy, "accountIds": []int64{},
	}, &ws)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace create: %v\n", err)
		return 1
	}
	fmt.Printf("%d\t%s\t%s\t%s\n", ws.ID, ws.Title, ws.Path, ws.RoutingPolicy)
	return 0
}

func cmdWorkspaceList(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind workspace ls")
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	var workspaces []store.Workspace
	if err := client.Call(context.Background(), "workspace.list", nil, &workspaces); err != nil {
		fmt.Fprintf(os.Stderr, "workspace ls: %v\n", err)
		return 1
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tPATH\tPOLICY")
	for _, ws := range workspaces {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", ws.ID, ws.Title, ws.Path, ws.RoutingPolicy)
	}
	tw.Flush()
	return 0
}

const workspaceSetupUsage = `usage: smind workspace setup show <workspaceId>
       smind workspace setup set <workspaceId> [--setup-file F|-] [--teardown-file F|-]
       smind workspace setup clear <workspaceId> [--setup] [--teardown]`

// workspaceScripts is workspace.get's result as the CLI reads it: the
// workspace plus its setup/teardown script text (ADR-0024: workspace.get is
// the only wire method that returns the text).
type workspaceScripts struct {
	ID             int64
	Title          string
	SetupScript    string
	TeardownScript string
}

// cmdWorkspaceSetup edits a workspace's human-authored worktree
// setup/teardown scripts (ADR-0024). Only this CLI and the web UI can write
// them; there is no MCP tool for it.
func cmdWorkspaceSetup(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, workspaceSetupUsage)
		return 2
	}
	verb := args[0]
	id, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "smind workspace setup: invalid workspaceId %q\n%s\n", args[1], workspaceSetupUsage)
		return 2
	}
	fs := flag.NewFlagSet("workspace setup "+verb, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var setupFile, teardownFile string
	var clearSetup, clearTeardown bool
	switch verb {
	case "show":
	case "set":
		fs.StringVar(&setupFile, "setup-file", "", "file with the setup script ('-' = stdin)")
		fs.StringVar(&teardownFile, "teardown-file", "", "file with the teardown script ('-' = stdin)")
	case "clear":
		fs.BoolVar(&clearSetup, "setup", false, "clear only the setup script")
		fs.BoolVar(&clearTeardown, "teardown", false, "clear only the teardown script")
	default:
		fmt.Fprintf(os.Stderr, "smind workspace setup: unknown subcommand %q\n%s\n", verb, workspaceSetupUsage)
		return 2
	}
	if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, workspaceSetupUsage)
		return 2
	}

	params := map[string]any{"workspaceId": id}
	switch verb {
	case "set":
		if setupFile == "" && teardownFile == "" {
			fmt.Fprintf(os.Stderr, "workspace setup set: pass --setup-file and/or --teardown-file\n%s\n", workspaceSetupUsage)
			return 2
		}
		if setupFile == "-" && teardownFile == "-" {
			fmt.Fprintln(os.Stderr, "workspace setup set: only one of --setup-file/--teardown-file may read stdin")
			return 2
		}
		for _, f := range []struct{ key, path string }{{"setupScript", setupFile}, {"teardownScript", teardownFile}} {
			if f.path == "" {
				continue
			}
			body, err := readScriptFile(f.path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "workspace setup set: %v\n", err)
				return 1
			}
			params[f.key] = body
		}
	case "clear":
		if !clearSetup && !clearTeardown {
			clearSetup, clearTeardown = true, true
		}
		if clearSetup {
			params["setupScript"] = ""
		}
		if clearTeardown {
			params["teardownScript"] = ""
		}
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	if verb == "show" {
		var ws workspaceScripts
		if err := client.Call(context.Background(), "workspace.get", map[string]any{"id": id}, &ws); err != nil {
			fmt.Fprintf(os.Stderr, "workspace setup show: %v\n", err)
			return 1
		}
		printScript("setup", ws.SetupScript)
		printScript("teardown", ws.TeardownScript)
		return 0
	}

	var ws store.Workspace
	if err := client.Call(context.Background(), "workspace.setScripts", params, &ws); err != nil {
		fmt.Fprintf(os.Stderr, "workspace setup %s: %v\n", verb, err)
		return 1
	}
	fmt.Printf("workspace %d: setup script %s, teardown script %s\n", ws.ID, setLabel(ws.HasSetupScript), setLabel(ws.HasTeardownScript))
	return 0
}

func setLabel(has bool) string {
	if has {
		return "set"
	}
	return "not set"
}

func printScript(name, body string) {
	if body == "" {
		fmt.Printf("# %s: (none)\n", name)
		return
	}
	fmt.Printf("# %s:\n%s\n", name, body)
}

// readScriptFile reads a script from path, or from stdin when path is "-".
func readScriptFile(path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(b), nil
}
