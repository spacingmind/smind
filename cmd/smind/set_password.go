package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
)

const cmdSetPasswordUsage = "usage: smind set-password [--password-stdin]"

// Seams for tests: a real terminal cannot be driven from `go test`.
var (
	stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	readSecret      = func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
)

// cmdSetPassword implements `smind set-password` (ADR-0031 D8): it stores a
// bcrypt hash of the daemon password in config.yaml (owner-only file), which
// is what lets the daemon listen beyond loopback. The password is read from a
// terminal prompt (twice) or, with --password-stdin, one line from stdin; it is
// never taken from argv (visible to every local user in `ps`) and never echoed
// or printed.
func cmdSetPassword(args []string) int { return runSetPassword(args, os.Stdin, os.Stdout, os.Stderr) }

func runSetPassword(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fromStdin := false
	for _, a := range args {
		if a != "--password-stdin" {
			fmt.Fprintf(stderr, "smind set-password: unexpected argument %q (the password is never taken from the command line)\n%s\n", a, cmdSetPasswordUsage)
			return 2
		}
		fromStdin = true
	}

	var pw string
	switch {
	case fromStdin:
		line, err := bufio.NewReader(io.LimitReader(stdin, 4096)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintf(stderr, "smind set-password: read stdin: %v\n", err)
			return 1
		}
		pw = strings.TrimRight(line, "\r\n")
	case stdinIsTerminal():
		first, err := readSecret("New password: ")
		if err != nil {
			fmt.Fprintf(stderr, "smind set-password: %v\n", err)
			return 1
		}
		second, err := readSecret("Repeat password: ")
		if err != nil {
			fmt.Fprintf(stderr, "smind set-password: %v\n", err)
			return 1
		}
		if first != second {
			fmt.Fprintln(stderr, "smind set-password: the two passwords differ; nothing changed")
			return 1
		}
		pw = first
	default:
		fmt.Fprintf(stderr, "smind set-password: stdin is not a terminal; pipe the password with --password-stdin\n%s\n", cmdSetPasswordUsage)
		return 2
	}

	hash, err := auth.HashPassword(pw) // enforces the length rules
	if err != nil {
		fmt.Fprintf(stderr, "smind set-password: %v; nothing changed\n", err)
		return 1
	}
	if err := config.SetPasswordHash(hash); err != nil {
		fmt.Fprintf(stderr, "smind set-password: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "password stored (bcrypt hash) in %s\nrestart the daemon to apply: smind stop && smind serve\n", config.Path())
	if os.Getenv(config.EnvPassword) != "" {
		fmt.Fprintf(stderr, "note: $%s is set and takes precedence over the stored password when the daemon runs\n", config.EnvPassword)
	}
	return 0
}
