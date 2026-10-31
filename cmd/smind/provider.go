package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const providerUsage = `Usage:
  smind provider update <id> [--models a,b] [--add-models a,b] [--disallow T,U] [--inherit-claude-config=true|false]
    --models                  replace the instance's model list ("" removes the field)
    --add-models              models added to the provider's own catalog ("" removes the field)
    --disallow                tools the instance may not call ("" removes the field)
    --inherit-claude-config   true/false: share ~/.claude instead of a private config directory
`

// providerDial is a variable so tests can stub the daemon connection.
var providerDial = cooldownDial

func cmdProvider(args []string) int {
	return runProvider(context.Background(), args, osStdout, osStderr)
}

// runProvider is cmdProvider with its environment injected. Exit codes:
// 0 the update succeeded, 1 a daemon or call error, 2 usage.
func runProvider(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "update" {
		fmt.Fprint(stderr, providerUsage)
		return 2
	}
	return runProviderUpdate(ctx, args[1:], stdout, stderr)
}

func runProviderUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "" {
		fmt.Fprint(stderr, providerUsage)
		return 2
	}
	id := args[0]
	fs := flag.NewFlagSet("provider update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	models := fs.String("models", "", "comma-separated model ids that replace the list")
	addModels := fs.String("add-models", "", "comma-separated model ids added to the catalog")
	disallow := fs.String("disallow", "", "comma-separated tool names the instance may not call")
	inherit := fs.String("inherit-claude-config", "", "share ~/.claude instead of a private config directory")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		fmt.Fprint(stderr, providerUsage)
		return 2
	}
	// fs.Visit sees only the flags given on the command line: a field is
	// sent only when its flag was named, an explicitly empty value meaning
	// "remove the field" rather than "leave it alone".
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if len(given) == 0 {
		fmt.Fprint(stderr, providerUsage)
		return 2
	}
	params := map[string]any{"id": id}
	if given["models"] {
		params["models"] = providerModelList(*models)
	}
	if given["add-models"] {
		params["additionalModels"] = providerModelList(*addModels)
	}
	if given["disallow"] {
		params["disallowedTools"] = providerToolList(*disallow)
	}
	if given["inherit-claude-config"] {
		b, err := strconv.ParseBool(*inherit)
		if err != nil {
			fmt.Fprint(stderr, providerUsage)
			return 2
		}
		params["inheritClaudeConfig"] = b
	}

	call, closeFn, err := providerDial(ctx)
	if err != nil {
		return cooldownDialError(err, stderr)
	}
	defer closeFn()

	var res struct {
		ID string `json:"id"`
	}
	if err := call(ctx, "provider.updateInstance", params, &res); err != nil {
		fmt.Fprintf(stderr, "smind: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Updated %s.\n", id)
	return 0
}

// providerSplitList splits a comma-separated flag value, trimming spaces
// around each item and dropping the empty ones.
func providerSplitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// providerModelList always returns a non-nil slice, so an explicitly empty
// --models marshals to [] (remove the field), not null (absent).
func providerModelList(value string) []map[string]any {
	out := []map[string]any{}
	for _, id := range providerSplitList(value) {
		out = append(out, map[string]any{"id": id})
	}
	return out
}

func providerToolList(value string) []string {
	items := providerSplitList(value)
	if items == nil {
		return []string{}
	}
	return items
}
