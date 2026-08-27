package accounts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/transport"
)

// rewriteTransport redirects every request to target's host, regardless of
// the URL the caller built the request against. This lets tests exercise
// AnthropicRefresher/OpenAIRefresher's real (constant) token URLs while
// actually talking to a local httptest server.
type rewriteTransport struct {
	target *url.URL
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testHTTPClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", srv.URL, err)
	}
	return &http.Client{Transport: &rewriteTransport{target: target}}
}

func jwtWithExpiry(t *testing.T, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]int64{"exp": exp.Unix()})
	if err != nil {
		t.Fatalf("marshal jwt payload: %v", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + body + "." + sig
}

func TestAnthropicRefresher_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]string
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeaders = r.Header.Clone()
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600}`))
		}))
		defer srv.Close()

		refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(testHTTPClient(t, srv)))
		before := time.Now()
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		if got.AccessToken != "access-new" {
			t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
		}
		if got.RefreshToken != "refresh-new" {
			t.Errorf("RefreshToken = %q, want refresh-new", got.RefreshToken)
		}
		wantExpiry := before.Add(3600 * time.Second)
		if got.ExpiresAt.Before(wantExpiry.Add(-5*time.Second)) || got.ExpiresAt.After(wantExpiry.Add(5*time.Second)) {
			t.Errorf("ExpiresAt = %v, want near %v", got.ExpiresAt, wantExpiry)
		}

		if gotBody["client_id"] != anthropicClientID {
			t.Errorf("request client_id = %q, want %q", gotBody["client_id"], anthropicClientID)
		}
		if gotBody["grant_type"] != "refresh_token" {
			t.Errorf("request grant_type = %q, want refresh_token", gotBody["grant_type"])
		}
		if gotBody["refresh_token"] != "refresh-old" {
			t.Errorf("request refresh_token = %q, want refresh-old", gotBody["refresh_token"])
		}
		if gotBody["scope"] != anthropicScope {
			t.Errorf("request scope = %q, want %q", gotBody["scope"], anthropicScope)
		}
		if ct := gotHeaders.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type header = %q, want application/json", ct)
		}
		if ua := gotHeaders.Get("User-Agent"); ua != "axios/1.15.2" {
			t.Errorf("User-Agent header = %q, want axios/1.15.2", ua)
		}
	})

	t.Run("missing refresh token in response falls back to original", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":60}`))
		}))
		defer srv.Close()

		refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if got.RefreshToken != "refresh-old" {
			t.Errorf("RefreshToken = %q, want preserved refresh-old", got.RefreshToken)
		}
	})

	t.Run("non-200 response returns status and body in error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		}))
		defer srv.Close()

		refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for non-200 response")
		}
		if got := err.Error(); !strings.Contains(got, "403") || !strings.Contains(got, "invalid_grant") {
			t.Errorf("Refresh() error = %q, want it to mention status 403 and body", got)
		}
	})

	t.Run("malformed JSON response returns error, not panic", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for malformed JSON response")
		}
	})
}

func TestOpenAIRefresher_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		wantExpiry := time.Now().Add(2 * time.Hour).Truncate(time.Second)
		accessToken := jwtWithExpiry(t, wantExpiry)

		var gotBody map[string]string
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeaders = r.Header.Clone()
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"access_token":  accessToken,
				"refresh_token": "refresh-new",
			})
		}))
		defer srv.Close()

		refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		if got.AccessToken != accessToken {
			t.Errorf("AccessToken = %q, want %q", got.AccessToken, accessToken)
		}
		if got.RefreshToken != "refresh-new" {
			t.Errorf("RefreshToken = %q, want refresh-new", got.RefreshToken)
		}
		if !got.ExpiresAt.Equal(wantExpiry) {
			t.Errorf("ExpiresAt = %v, want %v (from JWT exp claim)", got.ExpiresAt, wantExpiry)
		}

		if gotBody["client_id"] != openaiClientID {
			t.Errorf("request client_id = %q, want %q", gotBody["client_id"], openaiClientID)
		}
		if gotBody["grant_type"] != "refresh_token" {
			t.Errorf("request grant_type = %q, want refresh_token", gotBody["grant_type"])
		}
		if gotBody["refresh_token"] != "refresh-old" {
			t.Errorf("request refresh_token = %q, want refresh-old", gotBody["refresh_token"])
		}
		if ct := gotHeaders.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type header = %q, want application/json", ct)
		}
	})

	t.Run("missing refresh token in response falls back to original", func(t *testing.T) {
		t.Parallel()

		accessToken := jwtWithExpiry(t, time.Now().Add(time.Hour))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": accessToken})
		}))
		defer srv.Close()

		refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if got.RefreshToken != "refresh-old" {
			t.Errorf("RefreshToken = %q, want preserved refresh-old", got.RefreshToken)
		}
	})

	t.Run("non-200 response returns status and body in error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		}))
		defer srv.Close()

		refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for non-200 response")
		}
		if got := err.Error(); !strings.Contains(got, "401") || !strings.Contains(got, "invalid_grant") {
			t.Errorf("Refresh() error = %q, want it to mention status 401 and body", got)
		}
	})

	t.Run("malformed JSON response returns error, not panic", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for malformed JSON response")
		}
	})

	t.Run("access token not JWT shaped returns error, not guessed expiry", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"access_token":  "not-a-jwt",
				"refresh_token": "refresh-new",
			})
		}))
		defer srv.Close()

		refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for a non-JWT access token")
		}
	})
}

func TestJWTExpiry(t *testing.T) {
	t.Parallel()

	t.Run("no exp claim", func(t *testing.T) {
		t.Parallel()

		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{}`))
		sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
		_, err := jwtExpiry(header + "." + payload + "." + sig)
		if err == nil {
			t.Fatal("jwtExpiry() error = nil, want error for missing exp claim")
		}
	})

	t.Run("unparseable payload segment", func(t *testing.T) {
		t.Parallel()

		_, err := jwtExpiry("a.b.c")
		if err == nil {
			t.Fatal("jwtExpiry() error = nil, want error for unparseable base64 payload")
		}
	})
}

func TestNewAnthropicRefresher_DefaultTransport(t *testing.T) {
	t.Parallel()

	r := NewAnthropicRefresher()
	if r.httpClient == nil {
		t.Fatal("httpClient = nil, want a default uTLS-fingerprinted client")
	}
	if _, ok := r.httpClient.Transport.(*transport.RoundTripper); !ok {
		t.Errorf("httpClient.Transport = %T, want *transport.RoundTripper", r.httpClient.Transport)
	}
}

func TestNewOpenAIRefresher_DefaultTransport(t *testing.T) {
	t.Parallel()

	r := NewOpenAIRefresher()
	if r.httpClient == nil {
		t.Fatal("httpClient = nil, want a default uTLS-fingerprinted client")
	}
	if _, ok := r.httpClient.Transport.(*transport.RoundTripper); !ok {
		t.Errorf("httpClient.Transport = %T, want *transport.RoundTripper", r.httpClient.Transport)
	}
}

func TestAnthropicRefresher_ContextCancelled(t *testing.T) {
	t.Parallel()

	refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(http.DefaultClient))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := refresher.Refresh(ctx, OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want error for an already-cancelled context")
	}
}

func TestOpenAIRefresher_ContextCancelled(t *testing.T) {
	t.Parallel()

	refresher := NewOpenAIRefresher(WithOpenAIHTTPClient(http.DefaultClient))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := refresher.Refresh(ctx, OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want error for an already-cancelled context")
	}
}

func TestKimiRefresher_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotForm url.Values
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeaders = r.Header.Clone()
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse request form: %v", err)
			}
			gotForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600,"scope":"chat"}`))
		}))
		defer srv.Close()

		refresher := NewKimiRefresher(WithKimiHTTPClient(testHTTPClient(t, srv)))
		before := time.Now()
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		if got.AccessToken != "access-new" {
			t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
		}
		if got.RefreshToken != "refresh-new" {
			t.Errorf("RefreshToken = %q, want refresh-new", got.RefreshToken)
		}
		wantExpiry := before.Add(3600 * time.Second)
		if got.ExpiresAt.Before(wantExpiry.Add(-5*time.Second)) || got.ExpiresAt.After(wantExpiry.Add(5*time.Second)) {
			t.Errorf("ExpiresAt = %v, want near %v", got.ExpiresAt, wantExpiry)
		}

		if gotForm.Get("client_id") != kimiClientID {
			t.Errorf("request client_id = %q, want %q", gotForm.Get("client_id"), kimiClientID)
		}
		if gotForm.Get("grant_type") != "refresh_token" {
			t.Errorf("request grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
		}
		if gotForm.Get("refresh_token") != "refresh-old" {
			t.Errorf("request refresh_token = %q, want refresh-old", gotForm.Get("refresh_token"))
		}
		if ct := gotHeaders.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type header = %q, want application/x-www-form-urlencoded", ct)
		}
		if accept := gotHeaders.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept header = %q, want application/json", accept)
		}
	})

	t.Run("missing refresh token in response falls back to original", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":60}`))
		}))
		defer srv.Close()

		refresher := NewKimiRefresher(WithKimiHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if got.RefreshToken != "refresh-old" {
			t.Errorf("RefreshToken = %q, want preserved refresh-old", got.RefreshToken)
		}
	})

	t.Run("non-200 response returns status and body in error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"invalid grant"}`))
		}))
		defer srv.Close()

		refresher := NewKimiRefresher(WithKimiHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for non-200 response")
		}
		if got := err.Error(); !strings.Contains(got, "403") || !strings.Contains(got, "invalid grant") {
			t.Errorf("Refresh() error = %q, want it to mention status 403 and body", got)
		}
	})

	t.Run("malformed JSON response returns error, not panic", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		refresher := NewKimiRefresher(WithKimiHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for malformed JSON response")
		}
	})
}

func TestXAIRefresher_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		var gotForm url.Values
		var gotHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeaders = r.Header.Clone()
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse request form: %v", err)
			}
			gotForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","id_token":"id-new","token_type":"Bearer","expires_in":3600}`))
		}))
		defer srv.Close()

		refresher := NewXAIRefresher(WithXAIHTTPClient(testHTTPClient(t, srv)))
		before := time.Now()
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		if got.AccessToken != "access-new" {
			t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
		}
		if got.RefreshToken != "refresh-new" {
			t.Errorf("RefreshToken = %q, want refresh-new", got.RefreshToken)
		}
		wantExpiry := before.Add(3600 * time.Second)
		if got.ExpiresAt.Before(wantExpiry.Add(-5*time.Second)) || got.ExpiresAt.After(wantExpiry.Add(5*time.Second)) {
			t.Errorf("ExpiresAt = %v, want near %v", got.ExpiresAt, wantExpiry)
		}

		if gotForm.Get("client_id") != xaiClientID {
			t.Errorf("request client_id = %q, want %q", gotForm.Get("client_id"), xaiClientID)
		}
		if gotForm.Get("grant_type") != "refresh_token" {
			t.Errorf("request grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
		}
		if gotForm.Get("refresh_token") != "refresh-old" {
			t.Errorf("request refresh_token = %q, want refresh-old", gotForm.Get("refresh_token"))
		}
		if ct := gotHeaders.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type header = %q, want application/x-www-form-urlencoded", ct)
		}
		if accept := gotHeaders.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept header = %q, want application/json", accept)
		}
	})

	t.Run("missing refresh token in response falls back to original", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":60}`))
		}))
		defer srv.Close()

		refresher := NewXAIRefresher(WithXAIHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if got.RefreshToken != "refresh-old" {
			t.Errorf("RefreshToken = %q, want preserved refresh-old", got.RefreshToken)
		}
	})

	t.Run("non-200 response returns status and body in error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"invalid grant"}`))
		}))
		defer srv.Close()

		refresher := NewXAIRefresher(WithXAIHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for non-200 response")
		}
		if got := err.Error(); !strings.Contains(got, "401") || !strings.Contains(got, "invalid grant") {
			t.Errorf("Refresh() error = %q, want it to mention status 401 and body", got)
		}
	})

	t.Run("malformed JSON response returns error, not panic", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		refresher := NewXAIRefresher(WithXAIHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for malformed JSON response")
		}
	})
}

func TestAntigravityRefresher_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("success sends client_secret", func(t *testing.T) {
		t.Parallel()

		var gotForm url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse request form: %v", err)
			}
			gotForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600,"scope":"cloud-platform"}`))
		}))
		defer srv.Close()

		refresher := NewAntigravityRefresher(WithAntigravityHTTPClient(testHTTPClient(t, srv)))
		before := time.Now()
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		if got.AccessToken != "access-new" {
			t.Errorf("AccessToken = %q, want access-new", got.AccessToken)
		}
		if got.RefreshToken != "refresh-new" {
			t.Errorf("RefreshToken = %q, want refresh-new", got.RefreshToken)
		}
		wantExpiry := before.Add(3600 * time.Second)
		if got.ExpiresAt.Before(wantExpiry.Add(-5*time.Second)) || got.ExpiresAt.After(wantExpiry.Add(5*time.Second)) {
			t.Errorf("ExpiresAt = %v, want near %v", got.ExpiresAt, wantExpiry)
		}

		if gotForm.Get("client_id") != antigravityClientID {
			t.Errorf("request client_id = %q, want %q", gotForm.Get("client_id"), antigravityClientID)
		}
		if gotForm.Get("client_secret") != antigravityClientSecret {
			t.Errorf("request client_secret = %q, want %q", gotForm.Get("client_secret"), antigravityClientSecret)
		}
		if gotForm.Get("grant_type") != "refresh_token" {
			t.Errorf("request grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
		}
		if gotForm.Get("refresh_token") != "refresh-old" {
			t.Errorf("request refresh_token = %q, want refresh-old", gotForm.Get("refresh_token"))
		}
	})

	t.Run("missing refresh token in response falls back to original", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-new","token_type":"Bearer","expires_in":60}`))
		}))
		defer srv.Close()

		refresher := NewAntigravityRefresher(WithAntigravityHTTPClient(testHTTPClient(t, srv)))
		got, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if got.RefreshToken != "refresh-old" {
			t.Errorf("RefreshToken = %q, want preserved refresh-old", got.RefreshToken)
		}
	})

	t.Run("non-200 response returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
		}))
		defer srv.Close()

		refresher := NewAntigravityRefresher(WithAntigravityHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for non-200 response")
		}
		if got := err.Error(); !strings.Contains(got, "invalid_grant") {
			t.Errorf("Refresh() error = %q, want it to mention invalid_grant", got)
		}
	})

	t.Run("malformed JSON response returns error, not panic", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		refresher := NewAntigravityRefresher(WithAntigravityHTTPClient(testHTTPClient(t, srv)))
		_, err := refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
		if err == nil {
			t.Fatal("Refresh() error = nil, want error for malformed JSON response")
		}
	})
}

func TestAntigravityRefresher_ContextCancelled(t *testing.T) {
	t.Parallel()

	refresher := NewAntigravityRefresher(WithAntigravityHTTPClient(http.DefaultClient))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := refresher.Refresh(ctx, OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want error for an already-cancelled context")
	}
}

func TestKimiRefresher_ContextCancelled(t *testing.T) {
	t.Parallel()

	refresher := NewKimiRefresher(WithKimiHTTPClient(http.DefaultClient))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := refresher.Refresh(ctx, OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want error for an already-cancelled context")
	}
}

func TestXAIRefresher_ContextCancelled(t *testing.T) {
	t.Parallel()

	refresher := NewXAIRefresher(WithXAIHTTPClient(http.DefaultClient))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := refresher.Refresh(ctx, OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want error for an already-cancelled context")
	}
}

func TestAnthropicRefresher_ConnectionRefused(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("Listener.Close() error = %v", err)
	}
	target, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	refresher := NewAnthropicRefresher(WithAnthropicHTTPClient(&http.Client{Transport: &rewriteTransport{target: target}}))
	_, err = refresher.Refresh(context.Background(), OAuthCredential{RefreshToken: "refresh-old"})
	if err == nil {
		t.Fatal("Refresh() error = nil, want connection-refused error")
	}
}
