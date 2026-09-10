package accounts

import (
	"bytes"
	"html/template"
)

// callbackPageData is what callbackPageTemplate renders. Title and Message
// are the only caller-influenced strings that ever reach the page -- Message
// includes the vendor's raw error code on failure, so it goes through
// html/template's contextual auto-escaping like everything else here,
// rather than any manual string concatenation.
type callbackPageData struct {
	Success bool
	Title   string
	Message string
}

// callbackPageTemplate is the page a browser lands on after the vendor's
// redirect hits callbackServer. Colors are the exact --background/
// --foreground/--muted/--border/--destructive values from
// web/packages/ui/src/index.css (both light and dark, via
// prefers-color-scheme) so this feels like part of smind rather than a
// generic OAuth library's stock page -- the one moment in the whole login
// flow with no smind UI chrome around it at all.
const callbackPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}} - smind</title>
<style>
  :root {
    --background: oklch(1 0 0);
    --foreground: oklch(0.145 0 0);
    --muted-foreground: oklch(0.556 0 0);
    --border: oklch(0.922 0 0);
    --success: oklch(0.6 0.15 145);
    --destructive: oklch(0.577 0.245 27.325);
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --background: oklch(0.145 0 0);
      --foreground: oklch(0.985 0 0);
      --muted-foreground: oklch(0.708 0 0);
      --border: oklch(1 0 0 / 10%);
      --success: oklch(0.7 0.15 145);
      --destructive: oklch(0.704 0.191 22.216);
    }
  }
  * { box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 100vh;
    margin: 0;
    background: var(--background);
    color: var(--foreground);
  }
  .card {
    text-align: center;
    padding: 2.5rem 2rem;
    max-width: 380px;
    width: 100%;
  }
  .icon {
    width: 48px;
    height: 48px;
    margin: 0 auto 1.25rem;
    border-radius: 9999px;
    border: 1.5px solid {{if .Success}}var(--success){{else}}var(--destructive){{end}};
    color: {{if .Success}}var(--success){{else}}var(--destructive){{end}};
    display: flex;
    align-items: center;
    justify-content: center;
  }
  h1 {
    font-size: 1.125rem;
    font-weight: 600;
    margin: 0 0 0.5rem;
  }
  p {
    color: var(--muted-foreground);
    font-size: 0.875rem;
    line-height: 1.5;
    margin: 0;
  }
  .hint {
    margin-top: 1.5rem;
    padding-top: 1.25rem;
    border-top: 1px solid var(--border);
    font-size: 0.75rem;
  }
</style>
</head>
<body>
  <div class="card">
    <div class="icon">
      {{if .Success}}
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
      {{else}}
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      {{end}}
    </div>
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <p class="hint">You can close this tab and go back to smind.</p>
  </div>
</body>
</html>`

var callbackPage = template.Must(template.New("oauth-callback").Parse(callbackPageTemplate))

// renderCallbackSuccessPage returns the page shown after a completed login
// for a provider label like "Anthropic" or "OpenAI".
func renderCallbackSuccessPage(providerLabel string) string {
	return mustRenderCallbackPage(callbackPageData{
		Success: true,
		Title:   "Connected",
		Message: "Your " + providerLabel + " account is connected to smind.",
	})
}

// renderCallbackErrorPage returns the page shown when the vendor's redirect
// carried an error (or no code at all). reason is surfaced as-is (escaped by
// html/template) -- it's either the vendor's own ?error= value or a short
// internal description, never a full internal error chain.
func renderCallbackErrorPage(reason string) string {
	return mustRenderCallbackPage(callbackPageData{
		Success: false,
		Title:   "Login failed",
		Message: "smind couldn't complete the login: " + reason + ". Go back to smind and try again.",
	})
}

func mustRenderCallbackPage(data callbackPageData) string {
	var buf bytes.Buffer
	if err := callbackPage.Execute(&buf, data); err != nil {
		// callbackPageTemplate is a fixed, tested template executed with
		// caller-controlled but always-string data -- Execute failing here
		// would mean the template itself is broken, not a runtime
		// condition callers can recover from.
		panic("oauth callback page template: " + err.Error())
	}
	return buf.String()
}
