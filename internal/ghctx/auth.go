package ghctx

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/forge"
)

// The sign-in half of the client (ADR-0039 section 8): read the CLI's auth
// state, log in with a pasted token, log out. The token is only ever the
// stdin of one `gh auth login --with-token`; every error leaving this file
// is a typed *forge.Error whose detail has the token (and anything shaped
// like one) cut out.

// loginLoginRe is the shape of a GitHub login (user or app bot) we pass to
// --user.
var loginLoginRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$`)

// rawHosts is the part of `gh auth status --json hosts` we read: host ->
// accounts. scopes is a comma-separated string (empty for a fine-grained
// token, whose scopes GitHub does not report).
type rawHosts struct {
	Hosts map[string][]struct {
		State       string `json:"state"`
		Active      bool   `json:"active"`
		Host        string `json:"host"`
		Login       string `json:"login"`
		Scopes      string `json:"scopes"`
		TokenSource string `json:"tokenSource"`
		GitProtocol string `json:"gitProtocol"`
	} `json:"hosts"`
}

// Status reports the CLI version and every host's sign-in state, from
// `gh --version` and `gh auth status --json hosts` (never --show-token). A
// missing gh is a status with CLI.Installed false.
func (c *Client) Status(ctx context.Context) (forge.AuthStatus, error) {
	out := forge.AuthStatus{Hosts: []forge.AuthHost{}}
	ver, err := c.run(ctx, "", "--version")
	if err != nil {
		if fe, ok := c.mapRunError(ctx, err).(*forge.Error); ok && fe.Code == forge.CodeCLIMissing {
			return out, nil
		}
		return out, c.mapRunError(ctx, err)
	}
	out.CLI = forge.CLIInfo{Installed: true, Version: parseVersion(ver)}
	raw, err := c.run(ctx, "", "auth", "status", "--json", "hosts")
	if err != nil {
		return out, c.mapRunError(ctx, err)
	}
	var rh rawHosts
	if err := json.Unmarshal([]byte(raw), &rh); err != nil {
		return out, forge.NewError(forge.CodeError, "unreadable gh auth status output")
	}
	for host, accts := range rh.Hosts {
		if len(accts) == 0 {
			continue
		}
		a := accts[0]
		for _, x := range accts {
			if x.Active {
				a = x
				break
			}
		}
		h := a.Host
		if h == "" {
			h = host
		}
		out.Hosts = append(out.Hosts, forge.AuthHost{
			Host:          h,
			Authenticated: a.State == "success" || a.State == "timeout",
			Login:         a.Login,
			Scopes:        parseScopes(a.Scopes),
			Protocol:      a.GitProtocol,
			Source:        mapTokenSource(a.TokenSource),
		})
	}
	sort.Slice(out.Hosts, func(i, j int) bool { return out.Hosts[i].Host < out.Hosts[j].Host })
	return out, nil
}

// parseVersion extracts "2.101.0" from "gh version 2.101.0 (2026-09-15)".
func parseVersion(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	f := strings.Fields(line)
	if len(f) >= 3 && f[0] == "gh" && f[1] == "version" {
		return f[2]
	}
	return ""
}

func parseScopes(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.Trim(strings.TrimSpace(p), `'"`); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mapTokenSource turns gh's tokenSource -- "keyring", the name of an
// environment variable, or the path of the file holding the token -- into
// keyring | file | env | none. A path never leaves this function.
func mapTokenSource(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "none"
	case s == "keyring":
		return "keyring"
	case strings.ContainsAny(s, `/\`) || strings.HasSuffix(s, ".yml"):
		return "file"
	case strings.HasSuffix(s, "_TOKEN") || strings.HasSuffix(s, "_PAT"):
		return "env"
	default:
		return "file"
	}
}

// Login runs `gh auth login --hostname <host> --with-token --git-protocol
// https` with token on stdin and nowhere else. host must already have passed
// forge.ValidHost.
func (c *Client) Login(ctx context.Context, host, token string) error {
	if !forge.ValidHost(host) {
		return forge.NewError(forge.CodeError, "invalid host")
	}
	_, err := c.runIn(ctx, "", token+"\n", "auth", "login", "--hostname", host, "--with-token", "--git-protocol", "https")
	if err == nil {
		return nil
	}
	return redactToken(c.mapLoginError(ctx, err), token)
}

// mapLoginError is mapRunError for `gh auth login`: gh says a token was
// rejected with HTTP 401 / "bad credentials" and a classic token that lacks
// scopes with "missing required scope" (the typed codes the UI turns into one
// sentence each).
func (c *Client) mapLoginError(ctx context.Context, err error) error {
	low := strings.ToLower(err.Error())
	var code string
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() != nil):
		return c.mapRunError(ctx, err)
	case strings.Contains(low, "missing required scope") || strings.Contains(low, "missing scope"):
		code = forge.CodeTokenScopes
	case strings.Contains(low, "http 401") || strings.Contains(low, "bad credentials") ||
		strings.Contains(low, "error validating token") || strings.Contains(low, "authentication failed") ||
		strings.Contains(low, "invalid token") || strings.Contains(low, "token is invalid"):
		code = forge.CodeTokenInvalid
	default:
		return c.mapRunError(ctx, err)
	}
	return forge.NewError(code, err.Error())
}

// redactToken cuts token out of a typed error's detail: the scrubber knows
// the shapes of GitHub tokens, this knows the one value we were handed.
func redactToken(err error, token string) error {
	var fe *forge.Error
	if errors.As(err, &fe) && token != "" {
		fe.Detail = strings.ReplaceAll(fe.Detail, token, "[redacted]")
	}
	return err
}

// Logout runs `gh auth logout --hostname <host>`, naming the active account
// (`--user`) when the host has one, since gh refuses to guess among several.
func (c *Client) Logout(ctx context.Context, host string) error {
	if !forge.ValidHost(host) {
		return forge.NewError(forge.CodeError, "invalid host")
	}
	args := []string{"auth", "logout", "--hostname", host}
	if st, err := c.Status(ctx); err == nil {
		for _, h := range st.Hosts {
			if strings.EqualFold(h.Host, host) && loginLoginRe.MatchString(h.Login) {
				args = append(args, "--user", h.Login)
			}
		}
	}
	if _, err := c.run(ctx, "", args...); err != nil {
		return c.mapRunError(ctx, err)
	}
	return nil
}
