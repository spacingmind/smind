// Command harness is a throwaway test helper, not part of smind's own
// CLI: it starts a real relay (internal/relay/server.Run) and a real
// daemon-side bridge (internal/relay/bridge.Run) into it, with one
// enrolled workspace, then prints a real pairing URL and blocks.
//
// It exists solely so mobile/'s cross-language integration test
// (mobile/src/relay/__tests__/integration.node.test.ts) can drive the
// real TypeScript grpc-web + E2EE client (mobile/src/relay/client.ts)
// against a real Go relay end-to-end -- proving interoperability, not
// just that each side's own unit tests pass in isolation.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

const harnessWorkspaceID = "ws-node-harness"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
}

func run() error {
	relayDir, err := os.MkdirTemp("", "smind-relay-harness-")
	if err != nil {
		return err
	}
	secret, err := server.EnrollWorkspace(relayDir, harnessWorkspaceID)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := server.Run(ctx, server.Config{DataDir: relayDir, Listener: nativeLis, GRPCWebListener: webLis}); err != nil {
			fmt.Fprintln(os.Stderr, "relay run:", err)
		}
	}()

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

	cfg := bridge.Config{
		RelayAddress: nativeLis.Addr().String(),
		WorkspaceID:  harnessWorkspaceID,
		SecretHex:    hex.EncodeToString(secret),
		Fingerprint:  fingerprint,
	}
	go func() {
		if err := bridge.Run(ctx, cfg, daemonKP, api); err != nil {
			fmt.Fprintln(os.Stderr, "bridge run:", err)
		}
	}()

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

	// The one line a test harness watches stdout for; everything else on
	// stdout/stderr is diagnostic only.
	fmt.Println("READY " + url)

	select {} // block until the parent test process kills us.
}
