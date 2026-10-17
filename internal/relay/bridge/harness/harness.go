package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

const harnessWorkspaceID = "ws-node-harness"

// Config is what Run needs; main fills it from flags.
type Config struct {
	Hub        bool
	Legacy     bool
	MaxDevices int
	MobileStub bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// OnReady, when set, is called once everything is up, before the READY
	// line (in-process tests read the addresses from it).
	OnReady func(Ready)
}

// Ready describes the running harness.
type Ready struct {
	PairingURL  string
	NativeAddr  string
	WebAddr     string
	Fingerprint string
	SecretHex   string
	WorkspaceID string
	DaemonPub   []byte
	Home        string
}

// lineWriter serialises whole-line writes from concurrent goroutines.
type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) println(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintln(l.w, s)
}

func (l *lineWriter) printf(format string, args ...any) { l.println(fmt.Sprintf(format, args...)) }

// Run starts the relay and daemon side, announces them on cfg.Stdout and
// serves until ctx is cancelled (or, in hub mode, stdin says quit / EOF).
func Run(ctx context.Context, cfg Config) error {
	if cfg.MobileStub && !cfg.Hub {
		return errors.New("-mobile-stub requires -hub")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	out := &lineWriter{w: cfg.Stdout}
	errw := &lineWriter{w: cfg.Stderr}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	relayDir, err := os.MkdirTemp("", "smind-relay-harness-")
	if err != nil {
		return err
	}
	secret, err := server.EnrollHost(relayDir, harnessWorkspaceID)
	if err != nil {
		return err
	}

	nativeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	webLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	// Generate the cert BEFORE starting Run, so this process's own
	// fingerprint and the one Run serves are guaranteed identical -- Run
	// would otherwise race its own LoadOrCreateCert against this one (see
	// internal/relay/client/integration_test.go's startRelay, which
	// documents the same ordering requirement).
	cert, err := server.LoadOrCreateCert(relayDir)
	if err != nil {
		return err
	}
	fingerprint := server.CertFingerprint(cert)

	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		if err := server.Run(ctx, server.Config{DataDir: relayDir, Listener: nativeLis, GRPCWebListener: webLis}); err != nil {
			errw.println("relay run: " + err.Error())
		}
	}()
	defer func() { cancel(); <-relayDone }()

	dbDir, err := os.MkdirTemp("", "smind-harness-db-")
	if err != nil {
		return err
	}
	db, err := store.Open(filepath.Join(dbDir, "smind.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	acctDB, err := store.Open(filepath.Join(dbDir, "accounts.db"))
	if err != nil {
		return err
	}
	defer acctDB.Close()

	wm := workspace.New(db)
	api, err := wsapi.New(wm, accounts.New(acctDB), nil, db, "harness-token")
	if err != nil {
		return err
	}

	daemonKP, err := e2ee.GenerateKeyPair()
	if err != nil {
		return err
	}

	bcfg := bridge.Config{
		RelayAddress: nativeLis.Addr().String(),
		WorkspaceID:  harnessWorkspaceID,
		SecretHex:    hex.EncodeToString(secret),
		Fingerprint:  fingerprint,
	}

	var hubd *daemon
	if cfg.Hub {
		reg, err := devices.Open(filepath.Join(relayDir, "relay-devices.json"))
		if err != nil {
			return err
		}
		hubd = newDaemon(bridge.Options{
			Registry:      reg,
			LegacyDevices: cfg.Legacy,
			MaxDevices:    cfg.MaxDevices,
			Logf:          func(f string, a ...any) { errw.printf("hub: "+f, a...) },
		}, bcfg, daemonKP, api)
		hubd.start(ctx)
		defer hubd.stop()
	} else {
		go func() {
			if err := bridge.Run(ctx, bcfg, daemonKP, api); err != nil {
				errw.println("bridge run: " + err.Error())
			}
		}()
	}

	offer := pairing.Offer{
		DaemonID:         bridge.DaemonKeyID(daemonKP),
		PublicKey:        daemonKP.Public(),
		Relay:            "https://" + webLis.Addr().String(),
		RelayFingerprint: fingerprint,
		Secret:           secret,
		WorkspaceID:      harnessWorkspaceID,
	}
	url, err := offer.URL("")
	if err != nil {
		return err
	}

	if cfg.OnReady != nil {
		cfg.OnReady(Ready{
			PairingURL: url, NativeAddr: nativeLis.Addr().String(), WebAddr: webLis.Addr().String(),
			Fingerprint: fingerprint, SecretHex: hex.EncodeToString(secret), WorkspaceID: harnessWorkspaceID,
			DaemonPub: daemonKP.Public(), Home: relayDir,
		})
	}

	// The one line a test harness watches stdout for; everything else on
	// stdout/stderr is diagnostic only.
	out.println("READY " + url)

	// The offer's Relay field is the grpc-web address (mobile-only,
	// see cmd/smind/relay.go's cmdRelayOffer) -- the desktop Rust client
	// (ADR-0007 amendment, 2026-09-25) dials the relay's *native* gRPC
	// listener instead, whose address production code derives from the
	// grpc-web one by a fixed port convention that doesn't hold here
	// (nativeLis/webLis above are two independent ephemeral ports, not
	// adjacent ones). Printing it directly lets the Rust interop test
	// (desktop/daemon-client/tests/relay_harness_interop.rs) dial the
	// right port without that convention; it's an additive diagnostic
	// line, not a protocol change -- the mobile-side integration tests
	// only ever match the "READY " line and ignore any other output.
	out.println("NATIVE " + nativeLis.Addr().String())
	out.println("HOME " + relayDir)

	if !cfg.Hub {
		<-ctx.Done() // block until the parent test process kills us.
		return nil
	}
	out.println("DAEMONPUB " + base64.StdEncoding.EncodeToString(daemonKP.Public()))

	if cfg.MobileStub {
		m := &mobileStub{
			web: webLis.Addr().String(), fingerprint: fingerprint, workspace: harnessWorkspaceID,
			secret: secret, daemon: daemonKP.Public(), out: out, d: hubd,
		}
		if err := m.prepare(); err != nil {
			return err
		}
		go m.run(ctx)
	}

	serveCommands(ctx, cfg.Stdin, out, hubd)
	return nil
}
