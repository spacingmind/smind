package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spacingmind/smind/internal/config"
)

// statusProbeTimeout bounds the /healthz probe `smind status` makes.
const statusProbeTimeout = 2 * time.Second

// statusInfo is everything renderStatus prints, as plain values.
type statusInfo struct {
	Addr         string // host:port the CLI dials; empty when config failed
	Home         string
	ConfigPath   string
	ConfigExists bool
	ConfigErr    error // config.Load failed: the port was never probed

	Reachable bool
	Service   string // /healthz "service" (when Reachable)
	Version   string
	ManagedBy string
	ProbeErr  error // when !Reachable
}

// renderStatus formats info as aligned `label  value` lines.
func renderStatus(w io.Writer, info statusInfo) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if info.Addr != "" {
		fmt.Fprintf(tw, "daemon\thttp://%s\n", info.Addr)
	}
	fmt.Fprintf(tw, "home\t%s\n", info.Home)
	switch {
	case info.ConfigErr != nil:
		fmt.Fprintf(tw, "config\tERROR: %v\n", info.ConfigErr)
	case info.ConfigExists:
		fmt.Fprintf(tw, "config\t%s (exists)\n", info.ConfigPath)
	default:
		fmt.Fprintf(tw, "config\t%s (absent: defaults)\n", info.ConfigPath)
	}
	switch {
	case info.ConfigErr != nil:
		// Not probed.
	case !info.Reachable:
		fmt.Fprintf(tw, "status\tNOT reachable: %v\n", info.ProbeErr)
	case info.Service != "smind":
		fmt.Fprintf(tw, "status\tsomething else answers on this port (service=%s)\n", info.Service)
	default:
		line := "reachable, smind " + info.Version
		if info.ManagedBy != "" {
			line += " (managed by " + info.ManagedBy + ")"
		}
		fmt.Fprintf(tw, "status\t%s\n", line)
	}
	_ = tw.Flush()
}

// ok reports whether info describes a healthy smind daemon.
func (i statusInfo) ok() bool {
	return i.ConfigErr == nil && i.Reachable && i.Service == "smind"
}

// probeHealth GETs the daemon's unauthenticated /healthz.
func probeHealth(ctx context.Context, addr string) (service, version, managedBy string, err error) {
	ctx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return "", "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("/healthz returned %s", resp.Status)
	}
	var body struct {
		Service   string `json:"service"`
		Version   string `json:"version"`
		ManagedBy string `json:"managedBy"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", "", "", fmt.Errorf("decode /healthz: %w", err)
	}
	return body.Service, body.Version, body.ManagedBy, nil
}

// cmdStatus implements `smind status`: it resolves the daemon the way
// dialDaemon does and reports whether it answers.
func cmdStatus(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind status")
		return 2
	}

	info := statusInfo{Home: config.Dir(), ConfigPath: config.Path()}
	if _, err := os.Stat(info.ConfigPath); err == nil {
		info.ConfigExists = true
	}

	cfg, err := config.Load()
	if err != nil {
		info.ConfigErr = err
		renderStatus(os.Stdout, info)
		return 1
	}
	info.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Server.ClientPort()))

	svc, ver, managed, err := probeHealth(context.Background(), info.Addr)
	if err != nil {
		info.ProbeErr = err
	} else {
		info.Reachable, info.Service, info.Version, info.ManagedBy = true, svc, ver, managed
	}
	renderStatus(os.Stdout, info)
	if !info.ok() {
		return 1
	}
	return 0
}
