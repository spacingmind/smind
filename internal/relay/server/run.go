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
	// DataDir holds the persisted self-signed TLS certificate and
	// workspace enrollments (config.Dir()/relay by default from the CLI).
	DataDir string
	// BufferCap is the per-side reconnect buffer size (0 = default).
	BufferCap int
	// Listener, when non-nil, is served instead of binding ListenAddr —
	// lets tests hand Run a kernel-assigned ephemeral listener with no
	// bind race. Run takes ownership (closes it on stop).
	Listener net.Listener
}

// DefaultListenAddr is the relay's default bind address.
const DefaultListenAddr = ":7400"

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

	// Signals cancel ctx the same way an external context would, so tests
	// (no signals) and production (Ctrl+C) share one shutdown path.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- gs.Serve(lis)
	}()

	fmt.Fprintf(os.Stderr, "smind relay listening on %s (fingerprint %s)\n", lis.Addr(), CertFingerprint(cert))

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
