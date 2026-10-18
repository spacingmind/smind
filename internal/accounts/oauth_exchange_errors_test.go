package accounts

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExchangeErrorsNeverEchoTheVendorBody is "exchange errors never echo the
// vendor body": a non-200 token response, or a 200 that does not parse, keeps
// its status in the error but drops the body, for both providers; neither the
// errors nor anything logged carries the body. Not parallel: it swaps the
// process-wide log and slog destinations.
func TestExchangeErrorsNeverEchoTheVendorBody(t *testing.T) {
	var logs bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})

	const secret = "VENDOR-BODY-SECRET-77aa"
	body := fmt.Sprintf(`{"error":"invalid_grant","error_description":%q,"refresh_token":%q}`, secret, secret)

	serve := func(status int, respBody string) *http.Client {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(respBody))
		}))
		t.Cleanup(srv.Close)
		return testHTTPClient(t, srv)
	}

	var errs []string
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		for name, exchange := range map[string]func() error{
			"anthropic": func() error {
				_, err := NewAnthropicLogin(WithAnthropicLoginHTTPClient(serve(status, body))).Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
				return err
			},
			"openai": func() error {
				_, err := NewOpenAILogin(WithOpenAILoginHTTPClient(serve(status, body))).Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
				return err
			},
		} {
			err := exchange()
			if err == nil {
				t.Errorf("%s/%d: Exchange() error = nil, want a failure", name, status)
				continue
			}
			if want := fmt.Sprintf("status %d", status); !strings.Contains(err.Error(), want) {
				t.Errorf("%s/%d: error %q does not name the status", name, status, err.Error())
			}
			errs = append(errs, err.Error())
		}
	}

	// A 200 that does not parse (the body is not JSON) must not echo it either.
	_, err := NewAnthropicLogin(WithAnthropicLoginHTTPClient(serve(http.StatusOK, secret))).Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
	if err == nil {
		t.Error("anthropic: Exchange() of a garbage 200 error = nil")
	} else {
		errs = append(errs, err.Error())
	}
	_, err = NewOpenAILogin(WithOpenAILoginHTTPClient(serve(http.StatusOK, secret))).Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
	if err == nil {
		t.Error("openai: Exchange() of a garbage 200 error = nil")
	} else {
		errs = append(errs, err.Error())
	}

	for _, msg := range errs {
		if strings.Contains(msg, secret) || strings.Contains(msg, "invalid_grant") {
			t.Errorf("an exchange error echoes the vendor body: %q", msg)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Errorf("logs leak the vendor body: %q", logs.String())
	}
}
