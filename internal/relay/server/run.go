package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/spacingmind/smind/internal/config"
)

// Config controls a standalone relay process.
type Config struct {
	// Listen address, e.g. ":7400" ("" means DefaultListenAddr).
	ListenAddr string
	// GRPCWebListenAddr is the address the same RPCs are additionally
	// served on via grpc-web framing (see grpcweb.go), a separate port
	// from ListenAddr's native gRPC. Unlike ListenAddr, "" does NOT mean
	// DefaultGRPCWebListenAddr -- it means "bind an ephemeral port",
	// exactly like leaving GRPCWebListener nil, so every existing test
	// that only sets ListenAddr/Listener (never mentioning grpc-web at
	// all) keeps working with zero risk of two concurrently-running test
	// processes colliding on one hardcoded port. cmd/smind's `smind
	// relay` CLI is the one caller that explicitly passes
	// DefaultGRPCWebListenAddr, so real deployments still get a stable,
	// documented port.
	GRPCWebListenAddr string
	// DataDir holds the persisted self-signed TLS certificate and
	// workspace enrollments (config.Dir()/relay by default from the CLI).
	DataDir string
	// BufferCap is the per-side reconnect buffer size (0 = default).
	BufferCap int
	// Listener, when non-nil, is served instead of binding ListenAddr —
	// lets tests hand Run a kernel-assigned ephemeral listener with no
	// bind race. Run takes ownership (closes it on stop).
	Listener net.Listener
	// GRPCWebListener, when non-nil, is served instead of binding
	// GRPCWebListenAddr -- the grpc-web counterpart of Listener, for the
	// same reason.
	GRPCWebListener net.Listener
}

// DefaultListenAddr is the relay's default bind address.
const DefaultListenAddr = ":7400"

// DefaultGRPCWebListenAddr is the relay's default grpc-web bind address --
// deliberately a separate port from DefaultListenAddr; see grpcweb.go's
// doc comment for why native gRPC and grpc-web aren't multiplexed onto one
// listener.
const DefaultGRPCWebListenAddr = ":7401"

// RelayDir returns the relay's data directory under the smind home
// (~/.spacingmind/relay, $SMIND_HOME override).
func RelayDir() string {
	return filepath.Join(config.Dir(), "relay")
}

// Run starts a standalone relay: TLS certificate (self-signed, persisted),
// admission verifier seeded from the workspace store, gRPC server on the
// listen address, serving until ctx is cancelled or SIGINT/SIGTERM. It
// matches the daemon serve lifecycle: cancellation stops accepting, drains
// streams, exits 0.
func Run(ctx context.Context, cfg Config) error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if cfg.GRPCWebListenAddr == "" {
		// See the Config field's doc comment: unlike ListenAddr, an empty
		// GRPCWebListenAddr means "ephemeral port", not
		// DefaultGRPCWebListenAddr.
		cfg.GRPCWebListenAddr = "127.0.0.1:0"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = RelayDir()
	}

	store := newWorkspaceStore(cfg.DataDir)
	verifier, err := store.verifier()
	if err != nil {
		return fmt.Errorf("relay: workspaces: %w", err)
	}
	cert, err := LoadOrCreateCert(cfg.DataDir)
	if err != nil {
		return err
	}

	srv := New(verifier, cfg.BufferCap)
	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})))
	srv.Register(gs)

	lis := cfg.Listener
	if lis == nil {
		lis, err = net.Listen("tcp", cfg.ListenAddr)
		if err != nil {
			return fmt.Errorf("relay: listen %s: %w", cfg.ListenAddr, err)
		}
	}
	webLis := cfg.GRPCWebListener
	if webLis == nil {
		webLis, err = net.Listen("tcp", cfg.GRPCWebListenAddr)
		if err != nil {
			return fmt.Errorf("relay: listen %s: %w", cfg.GRPCWebListenAddr, err)
		}
	}

	// Signals cancel ctx the same way an external context would, so tests
	// (no signals) and production (Ctrl+C) share one shutdown path.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- gs.Serve(lis)
	}()

	// grpc-web's own shutdown is driven by ctx directly (see serveGRPCWeb),
	// independent of the native-gRPC select below -- a failure here never
	// affects native gRPC, which is exactly the point of using a separate
	// listener instead of multiplexing one port.
	webErrCh := make(chan error, 1)
	go func() { webErrCh <- serveGRPCWeb(ctx, gs, cert, webLis) }()

	fmt.Fprintf(os.Stderr, "smind relay listening on %s (fingerprint %s), grpc-web on %s\n", lis.Addr(), CertFingerprint(cert), webLis.Addr())

	select {
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() {
			gs.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			gs.Stop()
		}
		select {
		case err := <-webErrCh:
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay: grpc-web: %v\n", err)
			}
		case <-time.After(6 * time.Second):
			fmt.Fprintln(os.Stderr, "relay: grpc-web did not stop in time")
		}
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("relay: serve: %w", err)
		}
		return nil
	}
}

// EnrollWorkspace creates a workspace in the relay's store, returning the
// raw secret exactly once.
func EnrollWorkspace(dataDir, id string) ([]byte, error) {
	return newWorkspaceStore(dataDir).enroll(id)
}

// ListWorkspaces returns enrolled workspace IDs.
func ListWorkspaces(dataDir string) ([]string, error) {
	return newWorkspaceStore(dataDir).list()
}

// EnrollWorkspaceAtDefaultDir is the CLI convenience for
// EnrollWorkspace(RelayDir(), id).
func EnrollWorkspaceAtDefaultDir(id string) ([]byte, error) {
	return EnrollWorkspace(RelayDir(), id)
}
