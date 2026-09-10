package accounts

import (
	"strings"
	"testing"
)

func TestRenderCallbackSuccessPage(t *testing.T) {
	t.Parallel()

	page := renderCallbackSuccessPage("Anthropic")

	if !strings.Contains(page, "Anthropic") {
		t.Errorf("success page does not mention the provider label %q:\n%s", "Anthropic", page)
	}
	if !strings.Contains(page, "Connected") {
		t.Errorf("success page does not contain the expected title:\n%s", page)
	}
	if !strings.Contains(page, "<!DOCTYPE html>") {
		t.Errorf("success page is not a full HTML document:\n%s", page)
	}
}

func TestRenderCallbackErrorPage(t *testing.T) {
	t.Parallel()

	page := renderCallbackErrorPage("access_denied")

	if !strings.Contains(page, "access_denied") {
		t.Errorf("error page does not mention the reason %q:\n%s", "access_denied", page)
	}
	if !strings.Contains(page, "Login failed") {
		t.Errorf("error page does not contain the expected title:\n%s", page)
	}
}

// TestRenderCallbackErrorPage_EscapesReason confirms a vendor-supplied
// ?error= value (untrusted input reaching this template) can't inject
// markup into the page -- html/template's auto-escaping should turn it into
// inert text, not live HTML.
func TestRenderCallbackErrorPage_EscapesReason(t *testing.T) {
	t.Parallel()

	page := renderCallbackErrorPage(`<script>alert(1)</script>`)

	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Errorf("error page did not escape an HTML-shaped reason, got:\n%s", page)
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Errorf("error page should contain the escaped form of the reason, got:\n%s", page)
	}
}
