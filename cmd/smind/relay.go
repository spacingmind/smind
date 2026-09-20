package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/server"
)

// cmdRelay runs the self-hostable relay server (`smind relay`), or manages
// its workspace enrollments (`smind relay workspace ...`), or configures
// this daemon as a relay *client* (`smind relay connect`/`smind relay
// offer`) -- a different role from operating a relay: see internal/relay/
// bridge's doc comment.
//
//	smind relay [--listen <addr>] [--data-dir <dir>]
//	smind relay workspace new <id>    (prints the raw secret once)
//	smind relay workspace ls
//	smind relay connect <relay-address> <workspace-id> <secret>
//	smind relay offer
func cmdRelay(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "workspace":
			return cmdRelayWorkspace(args[1:])
		case "connect":
			return cmdRelayConnect(args[1:])
		case "offer":
			return cmdRelayOffer(args[1:])
		case "-h", "--help", "help":
			printRelayUsage(os.Stdout)
			return 0
		}
	}

	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", server.DefaultListenAddr, "listen address (native gRPC)")
	grpcWebListen := fs.String("grpc-web-listen", server.DefaultGRPCWebListenAddr, "listen address for the same RPCs over grpc-web")
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
		ListenAddr:        *listen,
		GRPCWebListenAddr: *grpcWebListen,
		DataDir:           *dataDir,
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

// cmdRelayConnect implements `smind relay connect <relay-address>
// <workspace-id> <secret>`: the daemon-as-relay-client pairing config
// (docs/decisions -- see docs/plans/active/mobile-app-milestone-1.md's
// Decisions). address/workspaceID/secret are exactly the values `smind
// relay workspace new`'s one-time output already gives a relay operator;
// the relay's TLS fingerprint is captured here via trust-on-first-use
// (bridge.FetchFingerprint) rather than a fourth argument, since a fresh
// relay's fingerprint isn't something a human would otherwise copy by hand.
// Persisting this under $SMIND_HOME is what makes `smind serve` dial the
// relay automatically on every future start -- opt-in, since the config
// simply doesn't exist until this command runs.
func cmdRelayConnect(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: smind relay connect <relay-address> <workspace-id> <secret>")
		return 2
	}
	address, workspaceID, secret := args[0], args[1], args[2]
	if _, err := hex.DecodeString(secret); err != nil {
		fmt.Fprintf(os.Stderr, "smind relay connect: secret is not valid hex: %v\n", err)
		return 2
	}

	fingerprint, err := bridge.FetchFingerprint(address)
	if err != nil {
		log.Fatalf("relay connect: %v", err)
	}
	if err := bridge.SaveConfig(config.Dir(), bridge.Config{
		RelayAddress: address,
		WorkspaceID:  workspaceID,
		SecretHex:    secret,
		Fingerprint:  fingerprint,
	}); err != nil {
		log.Fatalf("relay connect: %v", err)
	}
	fmt.Printf("connected to relay %s (workspace %q, fingerprint %s)\n\n`smind serve` will now dial this relay automatically. Run `smind relay offer` for a pairing URL for a mobile device.\n", address, workspaceID, fingerprint)
	return 0
}

// cmdRelayOffer implements `smind relay offer`: prints the pairing deep
// link (internal/relay/pairing.Offer.URL) for the relay connection `smind
// relay connect` configured, using this daemon's persisted long-lived E2EE
// keypair (generated on first use, see internal/relay/e2ee.LoadOrCreateKeyPair)
// as the offer's public key.
func cmdRelayOffer(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind relay offer")
		return 2
	}
	dir := config.Dir()
	cfg, ok, err := bridge.LoadConfig(dir)
	if err != nil {
		log.Fatalf("relay offer: %v", err)
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "smind relay offer: no relay connection configured; run `smind relay connect <relay-address> <workspace-id> <secret>` first")
		return 1
	}
	kp, err := e2ee.LoadOrCreateKeyPair(dir)
	if err != nil {
		log.Fatalf("relay offer: %v", err)
	}
	offer := pairing.Offer{
		DaemonID:         bridge.DaemonKeyID(kp),
		PublicKey:        kp.Public(),
		Relay:            "https://" + cfg.RelayAddress,
		RelayFingerprint: cfg.Fingerprint,
	}
	url, err := offer.URL("")
	if err != nil {
		log.Fatalf("relay offer: %v", err)
	}
	fmt.Println(url)
	return 0
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
  smind relay [--listen <addr>] [--grpc-web-listen <addr>] [--data-dir <dir>]
                                                           start the relay server
  smind relay workspace new <id>                          enroll a workspace (secret printed once)
  smind relay workspace ls                                list enrolled workspaces
  smind relay connect <address> <workspace-id> <secret>   configure this daemon to use a relay
  smind relay offer                                       print a pairing URL for a mobile device

The relay is a dumb pipe: it authenticates workspaces (HMAC
challenge-response) and forwards opaque ciphertext between the daemon and
paired mobile devices. It never sees plaintext. Data lives under
$SMIND_HOME/relay (default ~/.spacingmind/relay): a self-signed TLS
certificate (pin its fingerprint, printed at startup, at pairing time)
and the workspace secret hashes.

"smind relay connect"/"smind relay offer" are the OTHER side: they make
this daemon a client of some relay (self-hosted or not), so "smind serve"
dials out to it in the background and a paired mobile device can reach
this daemon's workspaces through it.
`)
}
