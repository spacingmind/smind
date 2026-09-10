package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGeneratePKCECodes(t *testing.T) {
	t.Parallel()

	pkce, err := GeneratePKCECodes()
	if err != nil {
		t.Fatalf("GeneratePKCECodes() error = %v", err)
	}
	if pkce.CodeVerifier == "" {
		t.Fatal("CodeVerifier = \"\", want non-empty")
	}
	if pkce.CodeChallenge == "" {
		t.Fatal("CodeChallenge = \"\", want non-empty")
	}
	// RFC 7636 base64url-no-padding: none of these characters appear.
	if strings.ContainsAny(pkce.CodeVerifier, "+/=") {
		t.Errorf("CodeVerifier = %q, contains non-base64url-no-padding characters", pkce.CodeVerifier)
	}
	if strings.ContainsAny(pkce.CodeChallenge, "+/=") {
		t.Errorf("CodeChallenge = %q, contains non-base64url-no-padding characters", pkce.CodeChallenge)
	}

	// The challenge must be exactly the S256 derivation of the verifier,
	// computed independently here.
	sum := sha256.Sum256([]byte(pkce.CodeVerifier))
	wantChallenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(sum[:])
	if pkce.CodeChallenge != wantChallenge {
		t.Errorf("CodeChallenge = %q, want S256(verifier) = %q", pkce.CodeChallenge, wantChallenge)
	}

	pkce2, err := GeneratePKCECodes()
	if err != nil {
		t.Fatalf("GeneratePKCECodes() second call error = %v", err)
	}
	if pkce2.CodeVerifier == pkce.CodeVerifier {
		t.Error("two calls to GeneratePKCECodes() produced the same verifier, want random")
	}
}

func TestGenerateState(t *testing.T) {
	t.Parallel()

	s1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error = %v", err)
	}
	if s1 == "" {
		t.Fatal("GenerateState() = \"\", want non-empty")
	}
	if strings.ContainsAny(s1, "+/=") {
		t.Errorf("GenerateState() = %q, contains non-base64url-no-padding characters", s1)
	}

	s2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() second call error = %v", err)
	}
	if s1 == s2 {
		t.Error("two calls to GenerateState() produced the same value, want random")
	}
}

func TestAnthropicLogin_AuthorizeURL(t *testing.T) {
	t.Parallel()

	login := NewAnthropicLogin()
	pkce := PKCECodes{CodeVerifier: "verifier-abc", CodeChallenge: "challenge-abc"}
	raw := login.AuthorizeURL("state-xyz", pkce)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	if u.Scheme+"://"+u.Host+u.Path != anthropicAuthorizeURL {
		t.Errorf("authorize URL base = %q, want %q", u.Scheme+"://"+u.Host+u.Path, anthropicAuthorizeURL)
	}
	q := u.Query()
	if q.Get("client_id") != anthropicClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), anthropicClientID)
	}
	if q.Get("redirect_uri") != "http://localhost:54545/callback" {
		t.Errorf("redirect_uri = %q, want http://localhost:54545/callback", q.Get("redirect_uri"))
	}
	if q.Get("code") != "true" {
		t.Errorf("code = %q, want true", q.Get("code"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("scope") != anthropicScope {
		t.Errorf("scope = %q, want %q", q.Get("scope"), anthropicScope)
	}
	if q.Get("code_challenge") != "challenge-abc" {
		t.Errorf("code_challenge = %q, want challenge-abc", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("state = %q, want state-xyz", q.Get("state"))
	}
}

func TestOpenAILogin_AuthorizeURL(t *testing.T) {
	t.Parallel()

	login := NewOpenAILogin()
	pkce := PKCECodes{CodeVerifier: "verifier-abc", CodeChallenge: "challenge-abc"}
	raw := login.AuthorizeURL("state-xyz", pkce)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	if u.Scheme+"://"+u.Host+u.Path != openaiAuthorizeURL {
		t.Errorf("authorize URL base = %q, want %q", u.Scheme+"://"+u.Host+u.Path, openaiAuthorizeURL)
	}
	q := u.Query()
	if q.Get("client_id") != openaiClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), openaiClientID)
	}
	if q.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
		t.Errorf("redirect_uri = %q, want http://localhost:1455/auth/callback", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "openid email profile offline_access" {
		t.Errorf("scope = %q, want openid email profile offline_access", q.Get("scope"))
	}
	if q.Get("code_challenge") != "challenge-abc" {
		t.Errorf("code_challenge = %q, want challenge-abc", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("state = %q, want state-xyz", q.Get("state"))
	}
	if q.Get("prompt") != "login" {
		t.Errorf("prompt = %q, want login", q.Get("prompt"))
	}
	if q.Get("id_token_add_organizations") != "true" {
		t.Errorf("id_token_add_organizations = %q, want true", q.Get("id_token_add_organizations"))
	}
	if q.Get("codex_cli_simplified_flow") != "true" {
		t.Errorf("codex_cli_simplified_flow = %q, want true", q.Get("codex_cli_simplified_flow"))
	}
}

func TestAnthropicLogin_Exchange(t *testing.T) {
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

	login := NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
	pkce := PKCECodes{CodeVerifier: "verifier-abc", CodeChallenge: "challenge-abc"}
	got, err := login.Exchange(context.Background(), "auth-code-123", "state-xyz", pkce)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if got.AccessToken != "access-new" || got.RefreshToken != "refresh-new" {
		t.Errorf("Exchange() = %+v, want access-new/refresh-new", got)
	}

	// Anthropic's token endpoint expects a JSON body, unlike OpenAI's.
	if ct := gotHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json", ct)
	}
	if gotBody["grant_type"] != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", gotBody["grant_type"])
	}
	if gotBody["code"] != "auth-code-123" {
		t.Errorf("code = %q, want auth-code-123", gotBody["code"])
	}
	if gotBody["redirect_uri"] != "http://localhost:54545/callback" {
		t.Errorf("redirect_uri = %q, want http://localhost:54545/callback", gotBody["redirect_uri"])
	}
	if gotBody["client_id"] != anthropicClientID {
		t.Errorf("client_id = %q, want %q", gotBody["client_id"], anthropicClientID)
	}
	if gotBody["code_verifier"] != "verifier-abc" {
		t.Errorf("code_verifier = %q, want verifier-abc", gotBody["code_verifier"])
	}
	if gotBody["state"] != "state-xyz" {
		t.Errorf("state = %q, want state-xyz", gotBody["state"])
	}
}

func TestAnthropicLogin_Exchange_NonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	login := NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
	_, err := login.Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
	if err == nil {
		t.Fatal("Exchange() error = nil, want error for non-200 response")
	}
}

func TestOpenAILogin_Exchange(t *testing.T) {
	t.Parallel()

	accessToken := jwtWithExpiry(t, time.Now().Add(time.Hour))
	var gotForm url.Values
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse request form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  accessToken,
			"refresh_token": "refresh-new",
		})
	}))
	defer srv.Close()

	login := NewOpenAILogin(WithOpenAILoginHTTPClient(testHTTPClient(t, srv)))
	pkce := PKCECodes{CodeVerifier: "verifier-abc", CodeChallenge: "challenge-abc"}
	got, err := login.Exchange(context.Background(), "auth-code-123", "state-xyz", pkce)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if got.AccessToken != accessToken || got.RefreshToken != "refresh-new" {
		t.Errorf("Exchange() = %+v", got)
	}

	// OpenAI's token endpoint expects a form-encoded body, and no state
	// field -- unlike Anthropic's JSON body, which does carry state.
	if ct := gotHeaders.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type header = %q, want application/x-www-form-urlencoded", ct)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", gotForm.Get("grant_type"))
	}
	if gotForm.Get("client_id") != openaiClientID {
		t.Errorf("client_id = %q, want %q", gotForm.Get("client_id"), openaiClientID)
	}
	if gotForm.Get("code") != "auth-code-123" {
		t.Errorf("code = %q, want auth-code-123", gotForm.Get("code"))
	}
	if gotForm.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
		t.Errorf("redirect_uri = %q, want http://localhost:1455/auth/callback", gotForm.Get("redirect_uri"))
	}
	if gotForm.Get("code_verifier") != "verifier-abc" {
		t.Errorf("code_verifier = %q, want verifier-abc", gotForm.Get("code_verifier"))
	}
	if gotForm.Has("state") {
		t.Errorf("form has a state field = %q, want none (OpenAI's token request never sends state)", gotForm.Get("state"))
	}
}

func TestOpenAILogin_Exchange_NonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	login := NewOpenAILogin(WithOpenAILoginHTTPClient(testHTTPClient(t, srv)))
	_, err := login.Exchange(context.Background(), "code", "state", PKCECodes{CodeVerifier: "v"})
	if err == nil {
		t.Fatal("Exchange() error = nil, want error for non-200 response")
	}
}
