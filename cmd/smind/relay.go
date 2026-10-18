package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/server"
	"github.com/spacingmind/smind/internal/securefile"
)

// cmdRelay runs the self-hostable relay server (`smind relay`), or manages
// its relay host enrollments (`smind relay host ...`), or configures
// this daemon as a relay *client* (`smind relay connect`/`smind relay
// offer`) -- a different role from operating a relay: see internal/relay/
// bridge's doc comment.
//
//	smind relay [--listen <addr>] [--data-dir <dir>] [--max-devices-per-host <n>] [--tls-cert <pem> --tls-key <pem>]
//	smind relay host new <id>    (prints the raw secret once)
//	smind relay host ls
//	smind relay host rm <id>
//	smind relay host rotate <id> (prints the new raw secret once)
//	smind relay connect [--fingerprint <hex>] <relay-address> <host-id> <secret>
//	smind relay offer [--window <duration>] [--max-devices <n>]
//	smind relay devices ls [--pending] | approve <id> | revoke <id> | rename <id> <label>
//
// A "relay host" is one daemon's mailbox on a relay (a host id plus an
// admission secret). `smind relay workspace ...` is the deprecated name for
// `smind relay host ...`; the wire format (proto `workspace_id`, the offer's
// `ws` key) and the stored files keep the old word (ADR-0031 D12).
func cmdRelay(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return cmdRelayHost(args[1:])
		case "workspace":
			fmt.Fprintln(os.Stderr, "smind relay: \"relay workspace\" is deprecated; use \"smind relay host\"")
			return cmdRelayHost(args[1:])
		case "connect":
			return cmdRelayConnect(args[1:])
		case "offer":
			return cmdRelayOffer(args[1:])
		case "devices":
			return cmdRelayDevices(args[1:])
		case "-h", "--help", "help":
			printRelayUsage(os.Stdout)
			return 0
		}
	}

	cfg, code := parseRelayServeFlags(args, os.Stderr)
	if code != 0 {
		return code
	}
	if err := server.Run(context.Background(), cfg); err != nil {
		log.Fatalf("relay: %v", err)
	}
	return 0
}

// parseRelayServeFlags reads the flags of `smind relay` (the server). A
// non-zero code means the process should exit with it, usage having been
// printed to stderr.
func parseRelayServeFlags(args []string, stderr io.Writer) (server.Config, int) {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", server.DefaultListenAddr, "listen address (native gRPC)")
	grpcWebListen := fs.String("grpc-web-listen", server.DefaultGRPCWebListenAddr, "listen address for the same RPCs over grpc-web")
	dataDir := fs.String("data-dir", "", "relay data directory (default $SMIND_HOME/relay)")
	maxDevices := fs.Int("max-devices-per-host", server.DefaultMaxDevicesPerHost, "maximum devices attached to one relay host at once")
	tlsCert := fs.String("tls-cert", "", "serve this PEM certificate chain (e.g. Let's Encrypt) instead of a self-signed one; needs --tls-key")
	tlsKey := fs.String("tls-key", "", "the PEM private key for --tls-cert")
	if err := fs.Parse(args); err != nil {
		return server.Config{}, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "smind relay: unexpected argument %q\n\n", fs.Arg(0))
		printRelayUsage(stderr)
		return server.Config{}, 2
	}
	if *dataDir == "" {
		*dataDir = server.RelayDir()
	}
	if *maxDevices < 1 {
		fmt.Fprintln(stderr, "smind relay: --max-devices-per-host must be at least 1")
		return server.Config{}, 2
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		fmt.Fprintln(stderr, "smind relay: --tls-cert and --tls-key must be given together")
		return server.Config{}, 2
	}
	return server.Config{
		ListenAddr:        *listen,
		GRPCWebListenAddr: *grpcWebListen,
		DataDir:           *dataDir,
		MaxDevicesPerHost: *maxDevices,
		TLSCertFile:       *tlsCert,
		TLSKeyFile:        *tlsKey,
	}, 0
}

// relayConnectNote says what connecting does and does not do: the word
// "workspace" here is smind's own, which a relay host is unrelated to.
const relayConnectNote = "Connecting registers this daemon once with the relay; every paired client sees all of this daemon's workspaces, spaces and tasks; nothing is created in smind."

// relayHostSecretNote is printed after a host secret: it is shown once, and
// the relay restarts to pick up a store change.
const relayHostSecretNote = "Give this to the daemon's pairing flow; the relay stores only its hash."

func cmdRelayHost(args []string) int {
	if len(args) == 0 {
		printRelayUsage(os.Stderr)
		return 2
	}
	dataDir := server.RelayDir()
	switch args[0] {
	case "new", "rotate":
		if len(args) != 2 {
			fmt.Fprintf(os.Stderr, "usage: smind relay host %s <id>\n", args[0])
			return 2
		}
		issue, verb := server.EnrollHost, "enrolled"
		if args[0] == "rotate" {
			issue, verb = server.RotateHost, "rotated (the old secret stops admitting once the relay restarts)"
		}
		secret, err := issue(dataDir, args[1])
		if err != nil {
			log.Fatalf("relay: %v", err)
		}
		fmt.Printf("relay host %q %s. Raw secret (shown ONCE, not retrievable again):\n\n  %s\n\n%s\n", args[1], verb, secretHex(secret), relayHostSecretNote)
		return 0
	case "rm":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: smind relay host rm <id>")
			return 2
		}
		if err := server.RemoveHost(dataDir, args[1]); err != nil {
			log.Fatalf("relay: %v", err)
		}
		fmt.Printf("relay host %q removed (it stops admitting once the relay restarts)\n", args[1])
		return 0
	case "ls":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: smind relay host ls")
			return 2
		}
		ids, err := server.ListHosts(dataDir)
		if err != nil {
			log.Fatalf("relay: %v", err)
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			fmt.Println("(no relay hosts enrolled)")
			return 0
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "smind relay host: unknown subcommand %q\n\n", args[0])
		printRelayUsage(os.Stderr)
		return 2
	}
}

// cmdRelayConnect implements `smind relay connect [--fingerprint <hex>]
// <relay-address> <host-id> <secret>`: the daemon-as-relay-client pairing
// config (docs/decisions -- see docs/plans/active/mobile-app-milestone-1.md's
// Decisions). address/hostID/secret are exactly the values `smind relay host
// new`'s one-time output already gives a relay operator.
//
// How the relay's certificate is trusted is decided here, once (ADR-0031
// P12c): a relay serving a certificate that verifies for its host name
// against the system roots is stored with an empty fingerprint (CA mode,
// nothing to pin); any other relay's fingerprint is shown and must be
// confirmed -- or given with --fingerprint -- before it is pinned. Nothing is
// ever pinned silently, because whoever answers this one connection would
// otherwise be trusted for good.
//
// Persisting this under $SMIND_HOME is what makes `smind serve` dial the
// relay automatically on every future start -- opt-in, since the config
// simply doesn't exist until this command runs.
func cmdRelayConnect(args []string) int {
	return runRelayConnect(args, config.Dir(), bridge.TLSProbe{}, os.Stdin, os.Stdout, os.Stderr)
}

// relayTLSProbe is what runRelayConnect needs from bridge.TLSProbe, so a test
// can stand in for the network.
type relayTLSProbe interface {
	Verify(address string) error
	Fingerprint(address string) (string, error)
}

const relayConnectUsage = `usage: smind relay connect [--fingerprint <hex>] <relay-address> <host-id> <secret>

A relay with a certificate from a public CA is verified normally. For a
self-signed relay the certificate's fingerprint is shown and you confirm it
(compare it with the one "smind relay" printed), or pass it as --fingerprint.
`

func runRelayConnect(args []string, dir string, probe relayTLSProbe, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("relay connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, relayConnectUsage) }
	wantFP := fs.String("fingerprint", "", "pin this hex SHA-256 certificate fingerprint (no prompt); the relay must present it")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 3 {
		fs.Usage()
		return 2
	}
	address, hostID, secret := pos[0], pos[1], pos[2]
	if _, err := hex.DecodeString(secret); err != nil {
		fmt.Fprintf(stderr, "smind relay connect: secret is not valid hex: %v\n", err)
		return 2
	}
	var pin []byte
	if *wantFP != "" {
		pin, err = hex.DecodeString(strings.TrimSpace(*wantFP))
		if err != nil || len(pin) != sha256.Size {
			fmt.Fprintln(stderr, "smind relay connect: --fingerprint must be 64 hex characters (the SHA-256 of the relay's certificate)")
			return 2
		}
	}

	fingerprint, trust, code := resolveRelayTrust(address, hex.EncodeToString(pin), probe, stdin, stdout, stderr)
	if code != 0 {
		return code
	}
	if err := bridge.SaveConfig(dir, bridge.Config{
		RelayAddress: address,
		WorkspaceID:  hostID, // bridge.Config keeps the wire name; it is the relay host id
		SecretHex:    secret,
		Fingerprint:  fingerprint,
	}); err != nil {
		fmt.Fprintf(stderr, "smind relay connect: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "connected to relay %s (relay host %q, %s)\n\n`smind serve` will now dial this relay automatically. Run `smind relay offer` for a pairing link for a mobile device or the desktop app.\n\n%s\n\n%s\n", address, hostID, trust, relayLinkWarning, relayConnectNote)
	return 0
}

// resolveRelayTrust decides what to store as the relay's fingerprint and how
// to describe it: "" for a CA-verified relay, the confirmed (or supplied)
// fingerprint otherwise. A non-zero code means connect must stop, having
// said why.
func resolveRelayTrust(address, suppliedFP string, probe relayTLSProbe, stdin io.Reader, stdout, stderr io.Writer) (fingerprint, trust string, code int) {
	if suppliedFP != "" {
		presented, err := probe.Fingerprint(address)
		if err != nil {
			fmt.Fprintf(stderr, "smind relay connect: %v\n", err)
			return "", "", 1
		}
		if presented != suppliedFP {
			fmt.Fprintf(stderr, "smind relay connect: %s presents certificate %s, not the --fingerprint you gave (%s); nothing was saved\n", address, presented, suppliedFP)
			return "", "", 1
		}
		return presented, "pinned fingerprint " + presented, 0
	}

	verr := probe.Verify(address)
	if verr == nil {
		return "", "certificate verified by system roots; no fingerprint to pin", 0
	}
	if !errors.Is(verr, bridge.ErrNotVerified) {
		fmt.Fprintf(stderr, "smind relay connect: %v\n", verr)
		return "", "", 1
	}
	presented, err := probe.Fingerprint(address)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay connect: %v\n", err)
		return "", "", 1
	}
	fmt.Fprintf(stdout, "The relay at %s does not present a certificate your system trusts:\n  %v\n\nIts certificate fingerprint is\n\n  %s\n\nCompare it with the fingerprint \"smind relay\" printed when it started. Anyone who can answer this connection\nright now could present a different certificate, so only continue if they match.\nPin it and connect? [y/N] ", address, verr, presented)
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return presented, "pinned fingerprint " + presented, 0
	}
	fmt.Fprintln(stderr, "\nsmind relay connect: not confirmed; nothing was saved. (Run it again and confirm, pass --fingerprint <hex>, or give the relay a certificate from a public CA with \"smind relay --tls-cert/--tls-key\".)")
	return "", "", 1
}

// parseInterspersed is fs.Parse that also accepts flags after positional
// arguments (`connect addr host secret --fingerprint x`), returning the
// positionals in order.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// cmdRelayOffer implements `smind relay offer`: prints the pairing deep
// link (internal/relay/pairing.Offer.URL) for the relay connection `smind
// relay connect` configured, using this daemon's persisted long-lived E2EE
// keypair (generated on first use, see internal/relay/e2ee.LoadOrCreateKeyPair)
// as the offer's public key.
//
// The offer's Relay field carries the relay's grpc-web address, not the
// native-gRPC one `relay connect`/bridge.Config store for the daemon's own
// dial: a mobile client speaks grpc-web only (see Item 2's plan notes on
// why), so it needs the other listener. Since `smind relay connect` takes
// a single address (matching the plan's literal CLI signature) and has no
// way to learn the operator's actual --grpc-web-listen choice, this
// derives it by the same convention `smind relay`'s own flag defaults use
// (native port + 1, i.e. :7400 native / :7401 grpc-web) -- correct for the
// common default-ports case this milestone's own manual verification
// exercises, and clearly not general: an operator running a customized,
// non-adjacent pair of ports needs a real fix (recording the grpc-web
// address explicitly), which is Milestone 2 scope, not this one's.
func cmdRelayOffer(args []string) int {
	return runRelayOffer(args, config.Dir(), os.Stdout, os.Stderr, isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()))
}

// relayLinkWarning is the consent wording printed whenever a person (not a
// script capturing stdout) is looking at an offer. It names the rotate
// command because the offer carries the relay host's admission secret, so
// rotating that secret is the only revocation.
const relayLinkWarning = "Treat this link like a password: anyone with it can reach this daemon through the relay.\n" +
	"To revoke it, run `smind relay host rotate <id>` (then `smind relay connect` again with the new secret).\n" +
	"Traffic is end-to-end encrypted; the relay cannot read your code or messages."

// runRelayOffer is cmdRelayOffer with its environment injected. stdout gets
// only the pairing URL, exactly as before, so `$(smind relay offer)` stays
// clean; everything meant for a person (the terminal QR, the warning, notes)
// goes to stderr. stdoutIsTTY or --qr turns the QR on; --png writes it to a
// file. An offer too long for any QR code is not an error: the URL is still
// printed and the QR is skipped with a note.
func runRelayOffer(args []string, dir string, stdout, stderr io.Writer, stdoutIsTTY bool) int {
	fs := flag.NewFlagSet("relay offer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: smind relay offer [--window <duration>] [--max-devices <n>] [--qr] [--png <file> [--force]]

Prints a pairing link for a mobile device or the desktop app. The link goes to
stdout; with a terminal on stdout, or --qr, a QR code is drawn on stderr too.
--png writes the QR code to a file (mode 0600; --force overwrites).

Printing the link also opens a pairing window (--window, default 10m): the next
--max-devices (default 1) new devices that connect in that time are approved
automatically; any other new device waits for "smind relay devices approve".

`+relayLinkWarning+"\n")
	}
	qr := fs.Bool("qr", false, "also draw the QR code on stderr (the default when stdout is a terminal)")
	pngPath := fs.String("png", "", "write the QR code as a PNG to `file`")
	force := fs.Bool("force", false, "with --png, overwrite an existing file")
	window := fs.Duration("window", devices.DefaultWindowDuration, "how long new devices are approved automatically after this offer")
	maxDevices := fs.Int("max-devices", devices.DefaultWindowSlots, "how many new devices the pairing window approves automatically")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || (*force && *pngPath == "") {
		fs.Usage()
		return 2
	}
	if *window < time.Second || *maxDevices < 1 {
		fmt.Fprintln(stderr, "smind relay offer: --window must be at least 1s and --max-devices at least 1")
		return 2
	}

	cfg, ok, err := bridge.LoadConfig(dir)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintln(stderr, "smind relay offer: no relay connection configured; run `smind relay connect <relay-address> <host-id> <secret>` first")
		return 1
	}
	kp, err := e2ee.LoadOrCreateKeyPair(dir)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
		return 1
	}
	grpcWebAddr, err := deriveGRPCWebAddress(cfg.RelayAddress)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
		return 1
	}
	secret, err := hex.DecodeString(cfg.SecretHex)
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: decode stored secret: %v\n", err)
		return 1
	}
	offer := pairing.Offer{
		DaemonID:         bridge.DaemonKeyID(kp),
		PublicKey:        kp.Public(),
		Relay:            "https://" + grpcWebAddr,
		RelayFingerprint: cfg.Fingerprint,
		Secret:           secret,
		WorkspaceID:      cfg.WorkspaceID, // the relay host id; the offer's "ws" key keeps the wire name
	}
	url, err := offer.URL("")
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
		return 1
	}
	openOfferWindow(dir, *window, *maxDevices, stderr)
	fmt.Fprintln(stdout, url)

	showQR := *qr || stdoutIsTTY
	if !showQR && *pngPath == "" {
		return 0
	}
	code, err := pairing.EncodeQRFit(url)
	if errors.Is(err, pairing.ErrContentTooLong) {
		fmt.Fprintf(stderr, "\nThis link is too long for a QR code (%d bytes); copy the link above instead.\n", len(url))
	} else if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
		return 1
	} else {
		if showQR {
			fmt.Fprintf(stderr, "\nScan with a mobile device or the desktop app:\n\n%s", code.Text())
		}
		if *pngPath != "" {
			if err := writeOfferPNG(*pngPath, code, *force); err != nil {
				fmt.Fprintf(stderr, "smind relay offer: %v\n", err)
				return 1
			}
			fmt.Fprintf(stderr, "\nQR code written to %s\n", *pngPath)
		}
	}
	fmt.Fprintf(stderr, "\n%s\n", relayLinkWarning)
	return 0
}

// openOfferWindow opens the pairing window the offer being printed relies on
// (through the daemon when it runs, else in the registry file). A failure is
// a warning, not an error: the offer is still valid, new devices just wait for
// "smind relay devices approve" instead of being admitted automatically.
func openOfferWindow(dir string, window time.Duration, maxDevices int, stderr io.Writer) {
	ctx := context.Background()
	ctl, closeFn, err := openDeviceControl(ctx, dir, stderr, "opening the pairing window in the device registry file directly; it applies when the daemon next starts")
	if err == nil {
		defer closeFn()
		_, err = ctl.openWindow(ctx, window, maxDevices)
	}
	if err != nil {
		fmt.Fprintf(stderr, "smind relay offer: warning: could not open the pairing window (%s); new devices will wait for `smind relay devices approve`\n", deviceErrMessage(err))
		return
	}
	fmt.Fprintf(stderr, "Pairing window open for %s or until %s; other new devices wait for `smind relay devices approve`.\n", window, newDevicesPhrase(maxDevices))
}

func newDevicesPhrase(n int) string {
	if n == 1 {
		return "1 new device connects"
	}
	return fmt.Sprintf("%d new devices connect", n)
}

// offerPNGModuleSize is the pixel size of one QR module in --png output: at
// the largest symbol (77 modules) that is a ~700px image, comfortable to scan
// from a screen.
const offerPNGModuleSize = 8

// writeOfferPNG writes code to path for the current user only (the QR is as
// sensitive as the link). Without force it refuses to replace a file.
func writeOfferPNG(path string, code *pairing.Code, force bool) error {
	data, err := code.PNG(offerPNGModuleSize, 4)
	if err != nil {
		return err
	}
	if force {
		return securefile.WriteFile(path, data)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
	}
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return securefile.Restrict(path)
}

// deriveGRPCWebAddress guesses the relay's grpc-web listen address from its
// native one, by the convention `smind relay`'s own flag defaults use
// (grpc-web = native port + 1). See cmdRelayOffer's doc comment for why
// this is a documented milestone-1 simplification, not a general solution.
func deriveGRPCWebAddress(nativeAddr string) (string, error) {
	host, portStr, err := net.SplitHostPort(nativeAddr)
	if err != nil {
		return "", fmt.Errorf("relay address %q: %w", nativeAddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("relay address %q: non-numeric port: %w", nativeAddr, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(port+1)), nil
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
  smind relay [--listen <addr>] [--grpc-web-listen <addr>] [--data-dir <dir>] [--max-devices-per-host <n>]
              [--tls-cert <pem> --tls-key <pem>]           start the relay server (default 16 devices per relay host);
                                                           with --tls-cert/--tls-key it serves your CA certificate
                                                           (e.g. Let's Encrypt) instead of a self-signed one
  smind relay host new <id>                               enroll a relay host (secret printed once)
  smind relay host ls                                     list enrolled relay hosts
  smind relay host rm <id>                                remove a relay host
  smind relay host rotate <id>                            issue a new secret (printed once; the old one stops admitting)
  smind relay connect [--fingerprint <hex>] <address> <host-id> <secret>
                                                           configure this daemon to use a relay
  smind relay offer [--window <duration>] [--max-devices <n>] [--qr] [--png <file> [--force]]
                                                           print a pairing link (and QR code) for a mobile device or the desktop app
                                                           and open a pairing window (default 10m, 1 new device)
  smind relay devices ls [--pending]                      list paired devices (and those awaiting approval)
  smind relay devices approve <id>                        approve a pending device
  smind relay devices revoke <id>                         revoke a device (it is disconnected and cannot return)
  smind relay devices rename <id> <label>                 change a device's label

Device ids accept any unique prefix. With the daemon running, "relay devices"
and "relay offer" go through it; otherwise they edit the device registry file
directly and the change applies when the daemon next starts.

Trusting the relay's certificate: "relay connect" first verifies it like a web
browser would (public CA, matching host name, not expired); if that works nothing
is pinned. Otherwise (a self-signed relay, the default) it shows the certificate
fingerprint and asks you to confirm it, or takes --fingerprint <hex>; it never
pins a certificate silently.

A relay host is one daemon's mailbox on a relay: a host id plus an admission
secret. ("smind relay workspace ..." is the deprecated name for "smind relay
host ...".) Host changes take effect when the relay restarts.

"smind relay offer" prints the pairing link on stdout; on a terminal, or with
--qr, it also draws a QR code on stderr, and --png writes one to a file. Treat
the link like a password: anyone with it can reach this daemon through the
relay; to revoke it, run "smind relay host rotate". Traffic is end-to-end
encrypted; the relay cannot read your code or messages.

The relay is a dumb pipe: it authenticates relay hosts (SCRAM-style
challenge-response) and forwards opaque ciphertext between the daemon and
paired mobile devices. It never sees plaintext. Data lives under
$SMIND_HOME/relay (default ~/.smind/relay): a self-signed TLS
certificate (pin its fingerprint, printed at startup, at pairing time)
and the relay host secret hashes.

"smind relay connect"/"smind relay offer" are the OTHER side: they make
this daemon a client of some relay (self-hosted or not), so "smind serve"
dials out to it in the background and a paired mobile device can reach
this daemon's workspaces through it.

Connecting registers this daemon once with the relay; every paired client sees all of this daemon's workspaces, spaces and tasks; nothing is created in smind.
`)
}
