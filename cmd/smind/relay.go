package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/relay/server"
)

// cmdRelay runs the self-hostable relay server (`smind relay`), or manages
// its workspace enrollments (`smind relay workspace ...`).
//
//	smind relay [--listen <addr>] [--data-dir <dir>]
//	smind relay workspace new <id>    (prints the raw secret once)
//	smind relay workspace ls
func cmdRelay(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "workspace":
			return cmdRelayWorkspace(args[1:])
		case "-h", "--help", "help":
			printRelayUsage(os.Stdout)
			return 0
		}
	}

	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", server.DefaultListenAddr, "listen address")
	dataDir := fs.String("data-dir", "", "relay data directory (default $SMIND_HOME/relay)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "smind relay: unexpected argument %q\n\n", fs.Arg(0))
		printRelayUsage(os.Stderr)
		return 2
	}
	if *dataDir == "" {
		*dataDir = server.RelayDir()
	}

	if err := server.Run(context.Background(), server.Config{
		ListenAddr: *listen,
		DataDir:    *dataDir,
	}); err != nil {
		log.Fatalf("relay: %v", err)
	}
	return 0
}

func cmdRelayWorkspace(args []string) int {
	if len(args) == 0 {
		printRelayUsage(os.Stderr)
		return 2
	}
	dataDir := server.RelayDir()
	switch args[0] {
	case "new":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: smind relay workspace new <id>")
			return 2
		}
		secret, err := server.EnrollWorkspace(dataDir, args[1])
		if err != nil {
			log.Fatalf("relay: %v", err)
		}
		fmt.Printf("workspace %q enrolled. Raw secret (shown ONCE, not retrievable again):\n\n  %s\n\nGive this to the daemon's pairing flow; the relay stores only its hash.\n", args[1], secretHex(secret))
		return 0
	case "ls":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: smind relay workspace ls")
			return 2
		}
		ids, err := server.ListWorkspaces(dataDir)
		if err != nil {
			log.Fatalf("relay: %v", err)
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			fmt.Println("(no workspaces enrolled)")
			return 0
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "smind relay workspace: unknown subcommand %q\n\n", args[0])
		printRelayUsage(os.Stderr)
		return 2
	}
}

func secretHex(secret []byte) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	for _, v := range secret {
		b.WriteByte(hex[v>>4])
		b.WriteByte(hex[v&0xf])
	}
	return b.String()
}

func printRelayUsage(w io.Writer) {
	fmt.Fprint(w, `smind relay — self-hostable E2EE relay server

Usage:
  smind relay [--listen <addr>] [--data-dir <dir>]       start the relay server
  smind relay workspace new <id>                          enroll a workspace (secret printed once)
  smind relay workspace ls                                list enrolled workspaces

The relay is a dumb pipe: it authenticates workspaces (HMAC
challenge-response) and forwards opaque ciphertext between the daemon and
paired mobile devices. It never sees plaintext. Data lives under
$SMIND_HOME/relay (default ~/.spacingmind/relay): a self-signed TLS
certificate (pin its fingerprint, printed at startup, at pairing time)
and the workspace secret hashes.
`)
}
