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
	if got.Credential.AccessToken != "access-new" || got.Credential.RefreshToken != "refresh-new" {
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
	if got.Credential.AccessToken != accessToken || got.Credential.RefreshToken != "refresh-new" {
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

// idTokenWithClaims builds an unsigned JWT whose payload is claims, like the
// id_token OpenAI's token endpoint returns (the daemon never verifies it).
func idTokenWithClaims(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal id_token claims: %v", err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(payload) + "." + enc.EncodeToString([]byte("sig"))
}

// anthropicIdentityOf runs an Anthropic exchange against a fake token
// endpoint whose response carries the given account.uuid and
// organization.uuid ("" omits that object), and returns the resulting
// identity.
func anthropicIdentityOf(t *testing.T, newLogin func(body string) *AnthropicLogin, accountUUID, orgUUID string) string {
	t.Helper()
	body := map[string]any{"access_token": "a", "refresh_token": "r", "expires_in": 3600}
	if accountUUID != "" {
		body["account"] = map[string]any{"uuid": accountUUID, "email_address": "someone@example.com"}
	}
	if orgUUID != "" {
		body["organization"] = map[string]any{"uuid": orgUUID}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal anthropic exchange body: %v", err)
	}
	res, err := newLogin(string(raw)).Exchange(context.Background(), "c", "s", PKCECodes{CodeVerifier: "v"})
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if res.Credential.AccessToken != "a" {
		t.Fatalf("credential = %+v, want the login to succeed whatever the identity", res.Credential)
	}
	return AccountIdentity("anthropic", res.Subject)
}

func newAnthropicLoginForBody(t *testing.T) func(body string) *AnthropicLogin {
	t.Helper()
	return func(body string) *AnthropicLogin {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
	}
}

func TestLoginExchange_Identity(t *testing.T) {
	t.Parallel()

	t.Run("oauth exchange derives a stable identity for anthropic and openai", func(t *testing.T) {
		t.Parallel()

		access := jwtWithExpiry(t, time.Now().Add(time.Hour))
		anthropicLogin := func(body string) *AnthropicLogin {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(srv.Close)
			return NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
		}
		openaiLogin := func(idToken string) *OpenAILogin {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]string{"access_token": access, "refresh_token": "r"}
				if idToken != "" {
					resp["id_token"] = idToken
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			t.Cleanup(srv.Close)
			return NewOpenAILogin(WithOpenAILoginHTTPClient(testHTTPClient(t, srv)))
		}
		identityOf := func(provider string, res ExchangeResult, err error) string {
			t.Helper()
			if err != nil {
				t.Fatalf("Exchange() error = %v", err)
			}
			return AccountIdentity(provider, res.Subject)
		}
		anthropicIdentity := func(accountUUID, orgUUID string) string {
			return anthropicIdentityOf(t, anthropicLogin, accountUUID, orgUUID)
		}
		openaiIdentity := func(sub, chatgptAccount string) string {
			var idToken string
			if sub != "" {
				idToken = idTokenWithClaims(t, map[string]any{
					"sub":                         sub,
					"email":                       "someone@example.com",
					"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": chatgptAccount},
				})
			}
			res, err := openaiLogin(idToken).Exchange(context.Background(), "c", "s", PKCECodes{CodeVerifier: "v"})
			return identityOf("openai", res, err)
		}

		// Anthropic: stable per (account.uuid, organization.uuid), distinct
		// across accounts, empty when the response has no account.
		a1, a1again, a2 := anthropicIdentity("uuid-1", "org-1"), anthropicIdentity("uuid-1", "org-1"), anthropicIdentity("uuid-2", "org-1")
		if a1 == "" || a1 != a1again || a1 == a2 {
			t.Errorf("anthropic identities = %q, %q, %q, want stable and distinct", a1, a1again, a2)
		}
		if got := anthropicIdentity("", ""); got != "" {
			t.Errorf("anthropic identity without account = %q, want empty", got)
		}

		// OpenAI: sub + chatgpt_account_id both matter.
		o1, o1again := openaiIdentity("user-1", "ws-1"), openaiIdentity("user-1", "ws-1")
		oWorkspace, oUser := openaiIdentity("user-1", "ws-2"), openaiIdentity("user-2", "ws-1")
		if o1 == "" || o1 != o1again || o1 == oWorkspace || o1 == oUser {
			t.Errorf("openai identities = %q, %q, %q, %q, want stable and distinct", o1, o1again, oWorkspace, oUser)
		}
		if got := openaiIdentity("", ""); got != "" {
			t.Errorf("openai identity without id_token = %q, want empty", got)
		}

		// Same subject under different providers never collides, and the
		// identity never contains the raw subject.
		if AccountIdentity("anthropic", "same") == AccountIdentity("openai", "same") {
			t.Error("identity does not depend on provider")
		}
		for _, id := range []string{a1, o1} {
			if len(id) != 64 || strings.Contains(id, "uuid-1") || strings.Contains(id, "org-1") || strings.Contains(id, "user-1") || strings.Contains(id, "ws-1") {
				t.Errorf("identity %q is not an opaque sha256 hex", id)
			}
		}
		if AccountIdentity("anthropic", "") != "" {
			t.Error("AccountIdentity with empty subject != empty")
		}
	})

	t.Run("malformed openai id_token yields no identity and does not fail the login", func(t *testing.T) {
		t.Parallel()

		for _, idToken := range []string{"not-a-jwt", "a.!!!.c", idTokenWithClaims(t, map[string]any{"email": "x"})} {
			if got := openAISubject(idToken); got != "" {
				t.Errorf("openAISubject(%q) = %q, want empty", idToken, got)
			}
		}
	})
}

func TestAccountIdentity_Derivation(t *testing.T) {
	t.Parallel()

	t.Run("openai identity requires both sub and chatgpt_account_id", func(t *testing.T) {
		t.Parallel()

		auth := func(id string) map[string]any { return map[string]any{"chatgpt_account_id": id} }
		for name, claims := range map[string]map[string]any{
			"sub only":             {"sub": "user-1"},
			"sub, empty account":   {"sub": "user-1", "https://api.openai.com/auth": auth("")},
			"account only":         {"https://api.openai.com/auth": auth("ws-1")},
			"sub, wrong auth type": {"sub": "user-1", "https://api.openai.com/auth": "oops"},
		} {
			if got := openAISubject(idTokenWithClaims(t, claims)); got != "" {
				t.Errorf("openAISubject(%s) = %q, want empty (no identity)", name, got)
			}
		}
		both := openAISubject(idTokenWithClaims(t, map[string]any{"sub": "user-1", "https://api.openai.com/auth": auth("ws-1")}))
		if both == "" {
			t.Error("openAISubject with both sub and chatgpt_account_id is empty")
		}
	})

	t.Run("anthropic identity requires both account uuid and organization uuid", func(t *testing.T) {
		t.Parallel()

		newLogin := newAnthropicLoginForBody(t)
		for name, ids := range map[string][2]string{
			"missing organization": {"acct-1", ""},
			"missing account":      {"", "org-1"},
			"both missing":         {"", ""},
		} {
			if got := anthropicIdentityOf(t, newLogin, ids[0], ids[1]); got != "" {
				t.Errorf("%s: identity = %q, want empty (no identity, login still succeeds)", name, got)
			}
		}
		if got := anthropicIdentityOf(t, newLogin, "acct-1", "org-1"); got == "" {
			t.Error("identity with both account and organization uuid is empty")
		}
		for _, pair := range [][2]string{{"", "org"}, {"acct", ""}, {"", ""}} {
			if got := anthropicSubject(pair[0], pair[1]); got != "" {
				t.Errorf("anthropicSubject(%q, %q) = %q, want empty", pair[0], pair[1], got)
			}
		}
	})

	t.Run("anthropic same account uuid in a different org yields a different identity", func(t *testing.T) {
		t.Parallel()

		newLogin := newAnthropicLoginForBody(t)
		personal := anthropicIdentityOf(t, newLogin, "acct-1", "org-personal")
		team := anthropicIdentityOf(t, newLogin, "acct-1", "org-team")
		otherPerson := anthropicIdentityOf(t, newLogin, "acct-2", "org-personal")
		if personal == "" || team == "" || personal == team || personal == otherPerson {
			t.Errorf("identities = %q, %q, %q, want all distinct", personal, team, otherPerson)
		}
		// account/org boundary inside the subject is delimiter safe.
		if anthropicSubject("a|b", "c") == anthropicSubject("a", "b|c") || anthropicSubject("a:1", "b") == anthropicSubject("a", "1:b") {
			t.Error("anthropic subject collides across the account/org boundary")
		}
	})

	t.Run("identity hash input is delimiter safe", func(t *testing.T) {
		t.Parallel()

		// Provider/subject boundary.
		if AccountIdentity("a", "b:c") == AccountIdentity("a:b", "c") {
			t.Error("AccountIdentity collides across the provider/subject boundary")
		}
		// sub/account boundary inside the OpenAI subject.
		tok := func(sub, account string) string {
			return openAISubject(idTokenWithClaims(t, map[string]any{
				"sub": sub, "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account},
			}))
		}
		if tok("a|b", "c") == tok("a", "b|c") || tok("a:1", "b") == tok("a", "1:b") {
			t.Error("openai subject collides across the sub/account boundary")
		}
		if AccountIdentity("openai", tok("a|b", "c")) == AccountIdentity("openai", tok("a", "b|c")) {
			t.Error("openai identity collides across the sub/account boundary")
		}
	})
}

func TestLoginExchange_UnexpectedShapes(t *testing.T) {
	t.Parallel()

	t.Run("exchange tolerates unexpected account and id_token shapes", func(t *testing.T) {
		t.Parallel()

		access := jwtWithExpiry(t, time.Now().Add(time.Hour))

		for name, account := range map[string]string{
			"string":       `"oops"`,
			"null":         `null`,
			"array":        `[]`,
			"number":       `7`,
			"uuid number":  `{"uuid":5}`,
			"uuid object":  `{"uuid":{"x":1}}`,
			"empty object": `{}`,
		} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600,"account":` + account + `}`))
			}))
			t.Cleanup(srv.Close)
			login := NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
			res, err := login.Exchange(context.Background(), "c", "s", PKCECodes{CodeVerifier: "v"})
			if err != nil {
				t.Errorf("anthropic account=%s (%s): Exchange() error = %v, want the login to succeed", name, account, err)
				continue
			}
			if res.Subject != "" || res.Credential.AccessToken != "a" {
				t.Errorf("anthropic account=%s: result = %+v, want credential kept and no subject", name, res)
			}
		}

		// A valid account with an unusable organization is no identity either.
		for name, org := range map[string]string{
			"string":      `"oops"`,
			"null":        `null`,
			"array":       `[]`,
			"uuid number": `{"uuid":5}`,
			"uuid object": `{"uuid":{"x":1}}`,
			"empty":       `{}`,
		} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600,"account":{"uuid":"acct-1"},"organization":` + org + `}`))
			}))
			t.Cleanup(srv.Close)
			login := NewAnthropicLogin(WithAnthropicLoginHTTPClient(testHTTPClient(t, srv)))
			res, err := login.Exchange(context.Background(), "c", "s", PKCECodes{CodeVerifier: "v"})
			if err != nil {
				t.Errorf("anthropic organization=%s (%s): Exchange() error = %v, want the login to succeed", name, org, err)
				continue
			}
			if res.Subject != "" || res.Credential.AccessToken != "a" {
				t.Errorf("anthropic organization=%s: result = %+v, want credential kept and no subject", name, res)
			}
		}

		authClaim := map[string]any{"chatgpt_account_id": "ws-1"}
		for name, idToken := range map[string]string{
			"number":          `123`,
			"object":          `{"x":1}`,
			"null":            `null`,
			"array":           `[]`,
			"not a jwt":       `"abc"`,
			"bad base64":      `"a.!!!.c"`,
			"claims wrong":    `"` + idTokenWithClaims(t, map[string]any{"sub": 5, "https://api.openai.com/auth": "x"}) + `"`,
			"auth wrong type": `"` + idTokenWithClaims(t, map[string]any{"sub": "u", "https://api.openai.com/auth": []int{1}}) + `"`,
			"account number":  `"` + idTokenWithClaims(t, map[string]any{"sub": "u", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": 9}}) + `"`,
		} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"access_token":"` + access + `","refresh_token":"r","id_token":` + idToken + `}`))
			}))
			t.Cleanup(srv.Close)
			login := NewOpenAILogin(WithOpenAILoginHTTPClient(testHTTPClient(t, srv)))
			res, err := login.Exchange(context.Background(), "c", "s", PKCECodes{CodeVerifier: "v"})
			if err != nil {
				t.Errorf("openai id_token=%s: Exchange() error = %v, want the login to succeed", name, err)
				continue
			}
			if res.Subject != "" || res.Credential.AccessToken != access {
				t.Errorf("openai id_token=%s: result = %+v, want credential kept and no subject", name, res)
			}
		}

		// A padded base64url payload (some issuers pad) still yields the subject.
		payload, _ := json.Marshal(map[string]any{"sub": "user-1", "https://api.openai.com/auth": authClaim})
		padded := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.URLEncoding.EncodeToString(payload) + "." + "c2ln"
		if !strings.HasSuffix(strings.Split(padded, ".")[1], "=") {
			// Make the fixture meaningful: pick a payload whose length needs padding.
			payload, _ = json.Marshal(map[string]any{"sub": "user-12", "https://api.openai.com/auth": authClaim})
			padded = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.URLEncoding.EncodeToString(payload) + "." + "c2ln"
		}
		if got := openAISubject(padded); got == "" {
			t.Errorf("openAISubject(padded payload %q) is empty, want a subject", strings.Split(padded, ".")[1])
		}
	})
}
