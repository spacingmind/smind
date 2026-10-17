// Command harness is a throwaway test helper, not part of smind's own
// CLI: it starts a real relay (internal/relay/server.Run) and a real
// daemon-side bridge into it, with one enrolled workspace, then prints a
// real pairing URL and blocks.
//
// It exists solely so the cross-language integration tests (mobile/'s
// node test and desktop/daemon-client's Rust interop test) can drive the
// real TypeScript/Rust relay clients against a real Go relay end-to-end --
// proving interoperability, not just that each side's own unit tests pass
// in isolation.
//
// With no flags it runs the legacy bridge (bridge.Run). With -hub it runs the
// multi-device hub with a device registry and a stdin command channel; with
// -mobile-stub it also plays a mobile device itself. See harness.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	var cfg Config
	flag.BoolVar(&cfg.Hub, "hub", false, "run the multi-device hub with a device registry (stdin commands)")
	flag.BoolVar(&cfg.Legacy, "legacy", false, "hub mode: also serve clients without device identity")
	flag.IntVar(&cfg.MaxDevices, "max-devices", 0, "hub mode: bound on served devices (0 = default)")
	flag.BoolVar(&cfg.MobileStub, "mobile-stub", false, "hub mode: play a mobile device over grpc-web")
	flag.Parse()
	cfg.Stdin, cfg.Stdout, cfg.Stderr = os.Stdin, os.Stdout, os.Stderr

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := Run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
}
