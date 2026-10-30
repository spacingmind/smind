package uiview

import (
	"encoding/json"
	"strings"
	"testing"
)

const okSVG = `<svg viewBox="0 0 24 24"><title>Check</title><path d="M20 6 9 17l-5-5"/></svg>`

// assertNoRawHTML fails when s would hand a renderer anything scripted or
// remote outside a code region.
func assertNoRawHTML(t *testing.T, s string) {
	t.Helper()
	plain := strings.ToLower(outsideCode(s))
	for _, bad := range []string{"<script", "<img", "<iframe"} {
		if strings.Contains(plain, bad) {
			t.Fatalf("result still contains %q outside code: %q", bad, s)
		}
	}
}

func TestSanitize_Markdown_StripsRawHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<script>alert(1)</script>hello", "alert(1)hello"},
		{"<b>bold</b> text", "bold text"},
		{"<!-- hidden --><!DOCTYPE html>x", "x"},
		{"a < b, 5 <3, x<<y", "a < b, 5 <3, x<"},
		{"<svg onload=alert(1)>hi</svg>", "hi"},
		{"ok <iframe src=//evil", "ok "},
		{"autolink <https://evil.example> gone", "autolink  gone"},
		{"<scr<script>ipt>x", "ipt>x"},
		{"code span `<script>` stays", "code span `<script>` stays"},
		{"```\n<img src=x>\n```\nafter <img>", "```\n<img src=x>\n```\nafter "},
	}
	for _, tc := range cases {
		if got := SanitizeMarkdown(tc.in); got != tc.want {
			t.Errorf("SanitizeMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range cases {
		assertNoRawHTML(t, SanitizeMarkdown(tc.in))
	}
}

func TestSanitize_Markdown_ImagesBecomeAltText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a ![logo](https://evil.example/x.png) b", "a logo b"},
		{"![chart][ref1]", "chart"},
		{"![](javascript:alert(1))", ""},
		{"![x](data:image/png;base64,AAAA)", "x"},
		{"[docs](https://example.com/a)", "[docs](https://example.com/a)"},
		{"[docs](HTTPS://example.com/a)", "[docs](HTTPS://example.com/a)"},
		{"[mail](mailto:a@b.example)", "[mail](mailto:a@b.example)"},
		{"[bad](javascript:alert(1))", "bad"},
		{"[ftp](ftp://x.example)", "ftp"},
		{"[spaces]( https://example.com )", "[spaces]( https://example.com )"},
	}
	for _, tc := range cases {
		if got := SanitizeMarkdown(tc.in); got != tc.want {
			t.Errorf("SanitizeMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
		assertNoRawHTML(t, SanitizeMarkdown(tc.in))
	}
}

func TestSanitize_SVG_64KiB_NoScript(t *testing.T) {
	cases := []struct {
		name string
		svg  string
		ok   bool
	}{
		{"ok svg", okSVG, true},
		{"ok namespaced", `<svg xmlns="http://www.w3.org/2000/svg"><rect width="4" height="4"/></svg>`, true},
		{"script element", `<svg><script>alert(1)</script></svg>`, false},
		{"style attribute", `<svg><rect style="fill:red"/></svg>`, false},
		{"onload handler", `<svg onload="alert(1)"/>`, false},
		{"foreignObject", `<svg><foreignObject><div>x</div></foreignObject></svg>`, false},
		{"external href", `<svg><use href="https://evil.example/x#y"/></svg>`, false},
		{"javascript href", `<svg><use xlink:href="javascript:alert(1)"/></svg>`, false},
		{"data url value", `<svg><path d="M0 0" fill="data:text/html,x"/></svg>`, false},
		{"url() not a fragment", `<svg><rect fill="url(evil)"/></svg>`, false},
		{"DOCTYPE with entity", `<!DOCTYPE svg [<!ENTITY xxe "http://evil.example">]><svg><path d="&xxe;"/></svg>`, false},
		{"entity reference", `<svg><path d="&xxe;"/></svg>`, false},
		{"processing instruction", `<?xml version="1.0"?><svg/>`, false},
		{"two roots", `<svg/><svg/>`, false},
		{"text outside title", `<svg>loose text</svg>`, false},
		{"64KiB and one byte", `<svg>` + strings.Repeat(" ", MaxSVGBytes) + `</svg>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SanitizeSVG(tc.svg)
			if tc.ok {
				if err != nil {
					t.Fatalf("expected the svg to pass, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the svg to be rejected")
			}
			ve, ok := err.(*Error)
			if !ok {
				t.Fatalf("expected a *uiview.Error, got %T", err)
			}
			if ve.Path != "svg" {
				t.Fatalf("error path = %q, want \"svg\"", ve.Path)
			}
		})
	}
}

func TestSanitize_Icon_NameOrSVGNotBoth(t *testing.T) {
	cases := []struct {
		name string
		icon any
		ok   bool
		path string
	}{
		{"lucide name", "Zap", true, ""},
		{"inline svg", map[string]any{"svg": okSVG}, true, ""},
		{"name and svg", map[string]any{"svg": okSVG, "name": "Zap"}, false, "p.icon.name"},
		{"lowercase name", "zap", false, "p.icon"},
		{"empty name", "", false, "p.icon"},
		{"number", 42, false, "p.icon"},
		{"array", []any{"Zap"}, false, "p.icon"},
		{"svg not a string", map[string]any{"svg": 3}, false, "p.icon.svg"},
		{"unsafe svg", map[string]any{"svg": `<svg><script>alert(1)</script></svg>`}, false, "p.icon.svg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateIcon(tc.icon, "p.icon")
			if tc.ok {
				if err != nil {
					t.Fatalf("expected the icon to pass, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected the icon to be rejected")
			}
			if got := errPath(t, err); got != tc.path {
				t.Fatalf("error path = %q, want %q", got, tc.path)
			}
		})
	}
}

func TestSanitize_Tone_OnlyTokens(t *testing.T) {
	for _, tone := range []string{"success", "warning", "danger", "muted"} {
		if err := validateTone(tone, "p.tone"); err != nil {
			t.Errorf("tone %q must be valid, got %v", tone, err)
		}
	}
	for _, tone := range []any{"#ff0000", "red", "Success", "", nil, 3} {
		err := validateTone(tone, "p.tone")
		if err == nil {
			t.Errorf("tone %v must be rejected", tone)
			continue
		}
		if got := errPath(t, err); got != "p.tone" {
			t.Errorf("tone %v: error path = %q, want p.tone", tone, got)
		}
	}
}

// openURLDoc wraps url in a button so the error carries a real document path.
func openURLDoc(url string) []byte {
	return []byte(`{"ui":1,"root":{"type":"button","label":"Open","action":{"kind":"openUrl","url":"` + url + `"}}}`)
}

// actionHost returns the host the normalised document added to the action.
func actionHost(t *testing.T, doc []byte) string {
	t.Helper()
	var out struct {
		Root struct {
			Action struct {
				Host string `json:"host"`
			} `json:"action"`
		} `json:"root"`
	}
	if err := json.Unmarshal(doc, &out); err != nil {
		t.Fatal(err)
	}
	return out.Root.Action.Host
}

func TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
		host string
	}{
		{"http ok", "http://example.com/a", true, "example.com"},
		{"https ok", "https://example.com", true, "example.com"},
		{"https with port", "https://Example.com:8443/a?x=1", true, "example.com:8443"},
		{"uppercase scheme", "HTTPS://Example.COM/A", true, "example.com"},
		{"javascript", "javascript:alert(1)", false, ""},
		{"data", "data:text/html;base64,PHNjcmlwdD4=", false, ""},
		{"file", "file:///etc/passwd", false, ""},
		{"ftp", "ftp://example.com/", false, ""},
		{"protocol relative", "//evil.example/x", false, ""},
		{"userinfo", "https://user:pw@example.com/", false, ""},
		{"control character", "https://exa\\u0007mple.com/", false, ""},
		{"whitespace", "https://example.com/a b", false, ""},
		{"no host", "https:///path", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Validate(openURLDoc(tc.url), Env{})
			if tc.ok {
				if err != nil {
					t.Fatalf("expected %q to pass, got %v", tc.url, err)
				}
				if got := actionHost(t, out); got != tc.host {
					t.Fatalf("added host = %q, want %q", got, tc.host)
				}
				return
			}
			if out != nil {
				t.Fatal("a rejected url must produce no output")
			}
			if got := errPath(t, err); got != "$.root.action.url" {
				t.Fatalf("error path = %q, want $.root.action.url (%v)", got, err)
			}
		})
	}

	// A plugin cannot supply the host field itself.
	_, err := Validate([]byte(`{"ui":1,"root":{"type":"button","label":"Open","action":{"kind":"openUrl","url":"https://example.com","host":"evil.example"}}}`), Env{})
	if got := errPath(t, err); got != "$.root.action.host" {
		t.Fatalf("error path = %q, want $.root.action.host", got)
	}
}

func TestSanitize_Rpc_MustBeDeclared(t *testing.T) {
	doc := []byte(`{"ui":1,"root":{"type":"button","label":"Go","action":{"kind":"rpc","rpc":"refresh"}}}`)
	if _, err := Validate(doc, rpcEnv); err != nil {
		t.Fatalf("a declared rpc must pass, got %v", err)
	}
	out, err := Validate(doc, Env{})
	if out != nil {
		t.Fatal("an undeclared rpc must produce no output")
	}
	if got := errPath(t, err); got != "$.root.action.rpc" {
		t.Fatalf("error path = %q, want $.root.action.rpc", got)
	}
	if msg := err.(*Error).Msg; msg != "not declared in contributes.rpc" {
		t.Fatalf("error msg = %q", msg)
	}
}

func TestSanitize_OpenPanel_MustBeDeclared(t *testing.T) {
	doc := []byte(`{"ui":1,"root":{"type":"button","label":"Open","action":{"kind":"openPanel","panel":"overview"}}}`)
	env := Env{Panels: map[string]bool{"overview": true}}
	if _, err := Validate(doc, env); err != nil {
		t.Fatalf("a declared panel must pass, got %v", err)
	}
	out, err := Validate(doc, Env{})
	if out != nil {
		t.Fatal("an undeclared panel must produce no output")
	}
	if got := errPath(t, err); got != "$.root.action.panel" {
		t.Fatalf("error path = %q, want $.root.action.panel", got)
	}
}

func TestSanitize_NoRemoteImageFields(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
		path string
	}{
		{"src on a text node", nodeDoc(`{"type":"text","text":"x","src":"https://x.example/y.png"}`), "$.root.src"},
		{"url on a text node", nodeDoc(`{"type":"text","text":"x","url":"https://x.example/y.png"}`), "$.root.url"},
		{"href on a badge", nodeDoc(`{"type":"badge","text":"x","href":"https://x.example/y.png"}`), "$.root.href"},
		{"src on a list item", nodeDoc(`{"type":"list","items":[{"title":"t","src":"https://x.example/y.png"}]}`), "$.root.items[0].src"},
		{"src on a table row cell", []byte(`{"ui":1,"root":{"type":"table","columns":[{"key":"a","label":"A"}],"rows":[{"a":"x","src":"y"}]}}`), "$.root.rows[0].src"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Validate(tc.doc, Env{})
			if out != nil {
				t.Fatal("a remote-image field must not validate")
			}
			if got := errPath(t, err); got != tc.path {
				t.Fatalf("error path = %q, want %q", got, tc.path)
			}
		})
	}
}
