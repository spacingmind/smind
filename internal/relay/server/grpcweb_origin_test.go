package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"nhooyr.io/websocket"
)

func originRequest(t *testing.T, relay *grpcWebTestRelay, method string, origin *string) (int, http.Header, []byte) {
	t.Helper()
	frame, err := grpcWebDataFrame(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     relay.workspace,
		ClientNonce:     bytes.Repeat([]byte{1}, admission.NonceSize),
		DaemonKeyId:     "origin-test-daemon",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, "https://"+relay.webAddr+"/relay.v1.Relay/AdmitChallenge", bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	if origin != nil {
		req.Header.Set("Origin", *origin)
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "content-type,x-grpc-web")
	}
	transport := &http.Transport{TLSClientConfig: relay.tlsConfig}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, data
}

func assertOriginUnary(t *testing.T, relay *grpcWebTestRelay, origin *string) {
	t.Helper()
	status, headers, data := originRequest(t, relay, http.MethodPost, origin)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, data)
	}
	var challenge relaypb.AdmitChallengeResponse
	trailers := parseGRPCWebFrames(t, "AdmitChallenge", data, &challenge)
	if trailers.status != "0" || len(challenge.GetServerNonce()) == 0 {
		t.Fatalf("challenge failed: %+v, %v", trailers, &challenge)
	}
	want := ""
	if origin != nil {
		want = *origin
	}
	if got := headers.Get("Access-Control-Allow-Origin"); got != want {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, want)
	}
}

func Test_grpcweb_unary_from_the_default_origin_works_with_cors_headers(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()
	origin := DefaultWebOrigin
	assertOriginUnary(t, relay, &origin)
	status, headers, _ := originRequest(t, relay, http.MethodOptions, &origin)
	if status != http.StatusOK || headers.Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("preflight: status=%d headers=%v", status, headers)
	}
}

func Test_grpcweb_websocket_from_the_default_origin_works(t *testing.T) {
	grpcWebOriginPingPong(t, DefaultWebOrigin)
}

func Test_grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()
	for _, origin := range []string{"https://unlisted.example", "null", "https://app.spacingmind.com.evil.org", "https://evilapp.spacingmind.com", DefaultWebOrigin + ":8443", "http://app.spacingmind.com", DefaultWebOrigin + "/path", "https://u@app.spacingmind.com"} {
		t.Run(origin, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodOptions} {
				status, headers, data := originRequest(t, relay, method, &origin)
				if status != http.StatusForbidden || strings.TrimSpace(string(data)) != "origin not allowed" {
					t.Errorf("%s: status=%d body=%q", method, status, data)
				}
				if headers.Get("Access-Control-Allow-Origin") != "" {
					t.Errorf("%s: refusal has CORS allow header", method)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			transport := &http.Transport{TLSClientConfig: relay.tlsConfig}
			defer transport.CloseIdleConnections()
			conn, resp, err := websocket.Dial(ctx, "wss://"+relay.webAddr+"/relay.v1.Relay/OpenControl", &websocket.DialOptions{
				HTTPClient:   &http.Client{Transport: transport},
				HTTPHeader:   http.Header{"Origin": []string{origin}},
				Subprotocols: []string{"grpc-websockets"},
			})
			if conn != nil {
				conn.Close(websocket.StatusNormalClosure, "test done")
			}
			if resp != nil && resp.Body != nil {
				defer resp.Body.Close()
			}
			if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("websocket: response=%v err=%v", resp, err)
			}
		})
	}
}

func Test_grpcweb_without_an_origin_is_unchanged(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()
	assertOriginUnary(t, relay, nil)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: relay.tlsConfig}, Timeout: 5 * time.Second}
	if id := admitOverGRPCWeb(t, client, "https://"+relay.webAddr, relay.workspace, "no-origin-daemon", relay.secret); len(id) == 0 {
		t.Fatal("empty admission id")
	}
	grpcWebOriginPingPong(t, "")
}

func Test_grpcweb_operator_origin_is_accepted(t *testing.T) {
	const operator = "https://operator.example:8443"
	for _, noDefault := range []bool{false, true} {
		t.Run(fmt.Sprint(noDefault), func(t *testing.T) {
			relay, stop := startGRPCWebTestRelay(t, Config{AllowedOrigins: []string{operator}, NoDefaultWebOrigin: noDefault})
			defer stop()
			origin := operator
			assertOriginUnary(t, relay, &origin)
			origin = DefaultWebOrigin
			if noDefault {
				status, _, _ := originRequest(t, relay, http.MethodPost, &origin)
				if status != http.StatusForbidden {
					t.Fatalf("disabled default: status=%d", status)
				}
			} else {
				assertOriginUnary(t, relay, &origin)
			}
			for _, origin := range []string{"https://operator.example", "https://operator.example:8444", "https://operator.example.evil.org:8443"} {
				status, _, _ := originRequest(t, relay, http.MethodPost, &origin)
				if status != http.StatusForbidden {
					t.Errorf("operator lookalike %q: status=%d", origin, status)
				}
			}
		})
	}
	// Invalid programmatic configuration is refused before starting listeners.
	for _, origin := range []string{"https://*.example", "http://example.com"} {
		if err := Run(context.Background(), Config{AllowedOrigins: []string{origin}, DataDir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), origin) {
			t.Fatalf("invalid origin %q: %v", origin, err)
		}
	}
}

func Test_grpcweb_refused_origin_logging_is_bounded_and_throttled(t *testing.T) {
	var log originRefusalLog
	now := time.Now()
	if !log.shouldLog("https://first.example", now) || log.shouldLog("https://first.example", now.Add(time.Second)) {
		t.Fatal("repeated origin was not suppressed")
	}
	for i := 0; i < 300; i++ {
		log.shouldLog(fmt.Sprint(i), now)
	}
	if len(log.last) != 256 {
		t.Fatalf("log size = %d", len(log.last))
	}
	if log.shouldLog("https://first.example", now.Add(59*time.Second)) {
		t.Fatal("flood evicted active origin")
	}
	if !log.shouldLog("https://first.example", now.Add(time.Minute)) {
		t.Fatal("origin was not logged again after one minute")
	}
	if len(log.last) != 1 {
		t.Fatalf("expired entries retained: %d", len(log.last))
	}
}
