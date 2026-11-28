package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/relay/server"
)

func Test_relay_allowed_origin_flag_and_env_are_merged_with_the_default(t *testing.T) {
	relayHome(t)

	// Flag values first, then the env values, de-duplicated by exact
	// string, in order.
	origins, err := resolveRelayOrigins([]string{"https://a.example", "https://b.example"}, "https://b.example, https://c.example,,")
	if err != nil {
		t.Fatalf("resolveRelayOrigins: %v", err)
	}
	if want := []string{"https://a.example", "https://b.example", "https://c.example"}; !reflect.DeepEqual(origins, want) {
		t.Fatalf("origins = %v, want %v", origins, want)
	}
	if origins, err = resolveRelayOrigins(nil, ""); err != nil || origins != nil {
		t.Fatalf("no flags and no env = %v, %v; want nil, nil (the server applies its default origin)", origins, err)
	}

	// No flag and no env: the config leaves the origin policy to the server.
	cfg, code := parseRelayServeFlags(nil, io.Discard)
	if code != 0 {
		t.Fatalf("parseRelayServeFlags code = %d", code)
	}
	if cfg.AllowedOrigins != nil || cfg.NoDefaultWebOrigin {
		t.Fatalf("AllowedOrigins = %v, NoDefaultWebOrigin = %v; want nil and false", cfg.AllowedOrigins, cfg.NoDefaultWebOrigin)
	}

	// With flag and env values, the merged list reaches the config and the
	// default origin stays on: the operator's values are added to it, not a
	// replacement, unless --no-default-web-origin is given.
	t.Setenv(relayAllowedOriginsEnv, "https://b.example, https://c.example")
	cfg, code = parseRelayServeFlags([]string{"--allowed-origin", "https://a.example", "--allowed-origin", "https://b.example"}, io.Discard)
	if code != 0 {
		t.Fatalf("parseRelayServeFlags code = %d", code)
	}
	if want := []string{"https://a.example", "https://b.example", "https://c.example"}; !reflect.DeepEqual(cfg.AllowedOrigins, want) {
		t.Errorf("AllowedOrigins = %v, want %v (added to the default %q)", cfg.AllowedOrigins, want, server.DefaultWebOrigin)
	}
	if cfg.NoDefaultWebOrigin {
		t.Error("NoDefaultWebOrigin = true; operator origins must be added to the default, not replace it")
	}
	cfg, code = parseRelayServeFlags([]string{"--allowed-origin", "https://a.example", "--no-default-web-origin"}, io.Discard)
	if code != 0 || !cfg.NoDefaultWebOrigin {
		t.Fatalf("--no-default-web-origin: code = %d, NoDefaultWebOrigin = %v", code, cfg.NoDefaultWebOrigin)
	}
}

func Test_relay_allowed_origin_invalid_stops_startup(t *testing.T) {
	relayHome(t)

	// An invalid flag value: exit 2 before anything listens, naming the
	// flag and the value.
	code, _, stderr := runRelay(t, "--allowed-origin", "http://evil.example")
	if code != 2 || !strings.Contains(stderr, "smind relay: --allowed-origin:") || !strings.Contains(stderr, "http://evil.example") {
		t.Fatalf("invalid flag value = %d, stderr %q; want exit 2 naming the flag and the value", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(config.Dir(), "listeners")); !os.IsNotExist(err) {
		t.Fatalf("the relay left listener records behind (stat err = %v); it must not listen with an invalid origin", err)
	}

	// An invalid env value: the same, with the environment variable named.
	if code, _, stderr := runRelay(t, "--allowed-origin", "https://a.example/path"); code != 2 || !strings.Contains(stderr, "https://a.example/path") {
		t.Fatalf("path origin: code=%d stderr=%q (the real parser must refuse a path)", code, stderr)
	}
	t.Setenv(relayAllowedOriginsEnv, "https://good.example,http://evil.example")
	code, _, stderr = runRelay(t)
	if code != 2 || !strings.Contains(stderr, "smind relay: "+relayAllowedOriginsEnv+":") || !strings.Contains(stderr, "http://evil.example") {
		t.Fatalf("invalid env value = %d, stderr %q; want exit 2 naming the env variable and the value", code, stderr)
	}

	// The resolve error itself names the value and its source.
	_, err := resolveRelayOrigins([]string{"https://good.example"}, "http://evil.example")
	if err == nil || !strings.Contains(err.Error(), relayAllowedOriginsEnv) || !strings.Contains(err.Error(), "http://evil.example") {
		t.Fatalf("resolveRelayOrigins error = %v; want it to name the env variable and the value", err)
	}
}
