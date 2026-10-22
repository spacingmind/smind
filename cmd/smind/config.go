package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spacingmind/smind/internal/config"
)

const cmdConfigUsage = `usage:
  smind config get [path]          show one setting (or all) as stored in config.yaml
  smind config set <path> <value>  change a setting (comments in config.yaml are kept)
  smind config unset <path>        remove a setting, restoring its default

settable paths: server.port, server.listen, server.hostnames, providers.<id>.defaultModel (id: claude-native, codex-native, glm, kimi),
  (server.passwordHash: unset only; use smind set-password)
changes apply when the daemon restarts (smind stop && smind serve)`

// cmdConfig implements `smind config get|set|unset` (ADR-0031 D10): the one
// documented way to change what `smind serve` used to take as launch flags.
func cmdConfig(args []string) int { return runConfig(args, os.Stdout, os.Stderr) }

func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, cmdConfigUsage)
		return 2
	}
	switch args[0] {
	case "get":
		return configGet(args[1:], stdout, stderr)
	case "set":
		if len(args) != 3 {
			fmt.Fprintf(stderr, "smind config set: want <path> <value>\n%s\n", cmdConfigUsage)
			return 2
		}
		if err := config.Set(args[1], args[2]); err != nil {
			fmt.Fprintf(stderr, "smind config set: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s updated in %s\n", args[1], config.Path())
		configAfterWrite(args[1], stdout, stderr)
		return 0
	case "unset":
		if len(args) != 2 {
			fmt.Fprintf(stderr, "smind config unset: want <path>\n%s\n", cmdConfigUsage)
			return 2
		}
		if err := config.Unset(args[1]); err != nil {
			fmt.Fprintf(stderr, "smind config unset: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s removed from %s\n", args[1], config.Path())
		configAfterWrite(args[1], stdout, stderr)
		return 0
	}
	fmt.Fprintf(stderr, "smind config: unknown subcommand %q\n%s\n", args[0], cmdConfigUsage)
	return 2
}

func configGet(args []string, stdout, stderr io.Writer) int {
	switch len(args) {
	case 0:
		kv, err := config.GetAll()
		if err != nil {
			fmt.Fprintf(stderr, "smind config get: %v\n", err)
			return 1
		}
		for _, p := range kv {
			v := p[1]
			if v == "" {
				v = "(not set)"
			}
			fmt.Fprintf(stdout, "%s = %s\n", p[0], v)
		}
		return 0
	case 1:
		v, err := config.Get(args[0])
		if err != nil {
			fmt.Fprintf(stderr, "smind config get: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, v)
		if env := overridingEnv(args[0]); env != "" && os.Getenv(env) != "" {
			fmt.Fprintf(stderr, "note: $%s is set and overrides this value when the daemon runs\n", env)
		}
		return 0
	}
	fmt.Fprintf(stderr, "smind config get: want at most one path\n%s\n", cmdConfigUsage)
	return 2
}

func overridingEnv(path string) string {
	switch path {
	case "server.listen":
		return config.EnvListen
	case "server.hostnames":
		return config.EnvHostnames
	case "server.passwordHash":
		return config.EnvPassword
	}
	return ""
}

// configAfterWrite prints the follow-ups every write needs: restart, and a
// warning when the result exposes the daemon without a password.
func configAfterWrite(path string, stdout, stderr io.Writer) {
	fmt.Fprintln(stdout, "restart the daemon to apply: smind stop && smind serve")
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if !cfg.Server.ListenIsLoopback() && !cfg.Server.HasPassword() {
		fmt.Fprintf(stderr, "warning: server.listen is %q, not a loopback address, and no password is set: the daemon will refuse to start until you run `smind set-password`\n", cfg.Server.Listen)
	}
	if strings.HasPrefix(path, "server.") && os.Getenv(config.EnvListen) != "" && path == "server.listen" {
		fmt.Fprintf(stderr, "note: $%s overrides server.listen when the daemon runs\n", config.EnvListen)
	}
}
