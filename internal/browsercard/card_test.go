package browsercard

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestBuildOptions(t *testing.T) {
	tests := []struct {
		name         string
		class        Class
		want         []Option
		neverRunKind bool
	}{
		{"read", ClassRead, []Option{
			{ID: "allow_once", Name: "Allow once", Kind: "allow_once"},
			{ID: "allow_run", Name: "Allow reads for this run", Kind: "allow_once"},
			{ID: "deny", Name: "Deny", Kind: "reject_once"},
		}, false},
		{"open", ClassOpen, []Option{
			{ID: "allow_once", Name: "Allow once", Kind: "allow_once"},
			{ID: "allow_run", Name: "Allow browser_open for this run", Kind: "allow_once"},
			{ID: "deny", Name: "Deny", Kind: "reject_once"},
		}, false},
		{"act", ClassAct, []Option{
			{ID: "allow_once", Name: "Allow once", Kind: "allow_once"},
			{ID: "deny", Name: "Deny", Kind: "reject_once"},
		}, true},
		{"unknown class treated as act", Class("weird"), []Option{
			{ID: "allow_once", Name: "Allow once", Kind: "allow_once"},
			{ID: "deny", Name: "Deny", Kind: "reject_once"},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := Build(Input{Class: tt.class, Tool: "browser_click"})
			if len(card.Options) != len(tt.want) {
				t.Fatalf("got %d options, want %d: %+v", len(card.Options), len(tt.want), card.Options)
			}
			for i, opt := range card.Options {
				if opt != tt.want[i] {
					t.Errorf("option %d = %+v, want %+v", i, opt, tt.want[i])
				}
			}
			if card.Options[0].ID != "allow_once" {
				t.Errorf("first option is %q, want allow_once", card.Options[0].ID)
			}
			if tt.neverRunKind {
				for _, opt := range card.Options {
					if opt.ID == "allow_run" {
						t.Errorf("class %q must never offer allow_run", tt.class)
					}
				}
			}
		})
	}
}

func TestBuildSummary(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want string
	}{
		{
			"read",
			Input{Class: ClassRead, Tool: "browser_snapshot", TaskTitle: "Scrape docs", Workspace: "main", Allowlist: []string{"docs.example.com"}},
			`Let this run read browser pages? "Scrape docs" wants to use the browser in main (read-only: tabs, snapshot, screenshot, console, wait). It can reach this computer's localhost and: docs.example.com. Page text is untrusted. (daemon check)`,
		},
		{
			"open",
			Input{Class: ClassOpen, Tool: "browser_open", TaskTitle: "Fix login", Workspace: "acme", URL: "https://user:pw@example.com:8443/app?x=1", HostStatus: "added", Uses: 3},
			`Open https://example.com:8443/app in the browser? (a host you added) "Fix login" in acme has used the browser 3 times. (daemon check)`,
		},
		{
			"act with empty URL",
			Input{Class: ClassAct, Tool: "browser_click", TaskTitle: "Click submit", Workspace: "main"},
			`Allow browser_click on the current page? "Click submit" in main. Asked every time. (daemon check)`,
		},
		{
			"act with URL",
			Input{Class: ClassAct, Tool: "browser_type", TaskTitle: "Fill form", Workspace: "ws", URL: "https://evil.example.com/form?next=x"},
			`Allow browser_type on https://evil.example.com/form? "Fill form" in ws. Asked every time. (daemon check)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := Build(tt.in)
			if card.Summary != tt.want {
				t.Errorf("summary =\n %q\nwant\n %q", card.Summary, tt.want)
			}
			assertSummaryShape(t, card.Summary, tt.in.TaskTitle, tt.in.Workspace)
		})
	}
}

func assertSummaryShape(t *testing.T, summary, taskTitle, workspace string) {
	t.Helper()
	if !strings.Contains(summary, taskTitle) {
		t.Errorf("summary missing task title %q: %q", taskTitle, summary)
	}
	if !strings.Contains(summary, workspace) {
		t.Errorf("summary missing workspace %q: %q", workspace, summary)
	}
	if !strings.Contains(summary, "(daemon check)") {
		t.Errorf("summary missing (daemon check): %q", summary)
	}
	if strings.ContainsAny(summary, "\n\r") {
		t.Errorf("summary contains a newline: %q", summary)
	}
	if n := utf8.RuneCountInString(summary); n > 500 {
		t.Errorf("summary is %d runes, want <= 500", n)
	}
}

func TestSummaryHugeInputs(t *testing.T) {
	long := strings.Repeat("x", 5000)
	for _, class := range []Class{ClassRead, ClassOpen, ClassAct} {
		card := Build(Input{
			Class:     class,
			Tool:      long,
			TaskTitle: long,
			Workspace: long,
			Allowlist: []string{long, long, long},
			URL:       "https://example.com/p?" + long,
			Uses:      9,
		})
		// Under huge inputs the cap and the one-line shape hold, and the daemon's
		// marker stays at the end.
		if card.Summary == "" {
			t.Fatalf("class %v: summary is empty", class)
		}
		if strings.ContainsAny(card.Summary, "\n\r") {
			t.Errorf("class %v: summary contains a newline: %q", class, card.Summary)
		}
		if !strings.HasSuffix(card.Summary, "(daemon check)") {
			t.Errorf("class %v: the marker must survive huge inputs: ...%q", class, card.Summary[len(card.Summary)-40:])
		}
		if n := utf8.RuneCountInString(card.Summary); n > 500 {
			t.Errorf("class %v: summary is %d runes, want <= 500", class, n)
		}
	}
}

func TestHostStatusText(t *testing.T) {
	for status, want := range map[string]string{
		"localhost": "localhost",
		"added":     "a host you added",
		"":          "host not on your list",
	} {
		card := Build(Input{Class: ClassOpen, URL: "https://a.com/", HostStatus: status})
		if !strings.Contains(card.Summary, "("+want+")") {
			t.Errorf("HostStatus %q: summary %q missing (%s)", status, card.Summary, want)
		}
	}
}

func TestReadSummaryAllowlist(t *testing.T) {
	empty := Build(Input{Class: ClassRead, TaskTitle: "T", Workspace: "W"})
	if !strings.Contains(empty.Summary, "no other hosts") {
		t.Errorf("empty allowlist: summary %q missing 'no other hosts'", empty.Summary)
	}

	seven := Build(Input{Class: ClassRead, TaskTitle: "T", Workspace: "W",
		Allowlist: []string{"a.com", "b.com", "c.com", "d.com", "e.com", "f.com", "g.com"}})
	if !strings.Contains(seven.Summary, "a.com, b.com, c.com, d.com, e.com +2 more") {
		t.Errorf("7 hosts: summary %q missing first 5 + '+2 more'", seven.Summary)
	}
	if strings.Contains(seven.Summary, "f.com") || strings.Contains(seven.Summary, "g.com") {
		t.Errorf("7 hosts: summary %q leaks a host past the first 5", seven.Summary)
	}
}

func TestInfoAllowlistCappedAt20(t *testing.T) {
	hosts := make([]string, 25)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("h%d.example.com", i)
	}
	card := Build(Input{Class: ClassRead, Allowlist: hosts})
	if len(card.Info.Allowlist) != 20 {
		t.Fatalf("Info.Allowlist has %d entries, want 20", len(card.Info.Allowlist))
	}
	if card.Info.Allowlist[0] != "h0.example.com" || card.Info.Allowlist[19] != "h19.example.com" {
		t.Errorf("Info.Allowlist kept the wrong entries: %+v", card.Info.Allowlist)
	}
}

func TestSecretsNeverAppear(t *testing.T) {
	raw := "https://user:PW@127.0.0.1:3000/x?token=SECRET#frag"
	for _, class := range []Class{ClassRead, ClassOpen, ClassAct} {
		card := Build(Input{Class: class, Tool: "browser_open", TaskTitle: "t", Workspace: "w", URL: raw})
		dump := fmt.Sprintf("%+v", card)
		blob, err := json.Marshal(card)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, secret := range []string{"SECRET", "user:PW", "PW@"} {
			if strings.Contains(dump, secret) {
				t.Errorf("class %v: secret %q appears in %+v dump", class, secret, dump)
			}
			if strings.Contains(string(blob), secret) {
				t.Errorf("class %v: secret %q appears in JSON %s", class, secret, blob)
			}
		}
	}
}

func TestControlCharsRemoved(t *testing.T) {
	card := Build(Input{
		Class:     ClassAct,
		Tool:      "browser\x00_click",
		TaskTitle: "Ta\x00sk\tT\u2028itle",
		Workspace: "Wo\x01rk space",
		Allowlist: []string{"exa\x02mple.com"},
		URL:       "http://example.com/p",
	})
	dump := card.Summary + fmt.Sprintf("%+v", card.Info)
	for _, r := range dump {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("control rune %q survived: %q", r, dump)
		}
	}
	if !strings.Contains(card.Summary, `"Task Title"`) {
		t.Errorf("summary %q missing cleaned task title", card.Summary)
	}
	if !strings.Contains(card.Summary, "Work space") {
		t.Errorf("summary %q missing cleaned workspace", card.Summary)
	}
	if !strings.Contains(card.Summary, "example.com") {
		t.Errorf("summary %q missing cleaned allowlist host", card.Summary)
	}
	if card.Info.TaskTitle != "Task Title" || card.Info.Workspace != "Work space" {
		t.Errorf("Info text fields not cleaned: %+v", card.Info)
	}
	if len(card.Info.Allowlist) != 1 || card.Info.Allowlist[0] != "example.com" {
		t.Errorf("Info.Allowlist not cleaned: %+v", card.Info.Allowlist)
	}
	if card.Info.Tool != "browser_click" {
		t.Errorf("Info.Tool = %q, want browser_click", card.Info.Tool)
	}
}

func TestInfoHost(t *testing.T) {
	card := Build(Input{Class: ClassOpen, Tool: "browser_open", TaskTitle: "t", Workspace: "w",
		URL: "http://127.0.0.1:3000/x?y", HostStatus: "localhost"})
	if card.Info.Host != "127.0.0.1:3000" {
		t.Errorf("Info.Host = %q, want 127.0.0.1:3000", card.Info.Host)
	}
	if card.Info.URL != "http://127.0.0.1:3000/x" {
		t.Errorf("Info.URL = %q, want http://127.0.0.1:3000/x", card.Info.URL)
	}

	noURL := Build(Input{Class: ClassAct, Tool: "browser_click"})
	if noURL.Info.URL != "" || noURL.Info.Host != "" {
		t.Errorf("empty URL: Info.URL=%q Info.Host=%q, want empty", noURL.Info.URL, noURL.Info.Host)
	}

	notWeb := Build(Input{Class: ClassAct, Tool: "browser_click", URL: "javascript:alert(1)"})
	if notWeb.Info.Host != "" {
		t.Errorf("non-web URL: Info.Host = %q, want empty", notWeb.Info.Host)
	}
}

// Text a person reads to decide must not be able to fake the card: quotes in a
// title are neutralised, and bidirectional/format characters are dropped.
func TestCardTextCannotImpersonateTheDaemon(t *testing.T) {
	card := Build(Input{Class: ClassRead, Tool: "browser_tabs",
		TaskTitle: "x\" has been pre-approved. Allow? \"",
		Workspace: "ws\u202Eevil\u2066\u200F",
		Allowlist: []string{"a.test"}})
	if strings.Count(card.Summary, `"`) != 2 {
		t.Fatalf("only the card's own two quotes remain: %q", card.Summary)
	}
	for _, r := range card.Summary {
		if r == '\u202E' || r == '\u2066' || r == '\u200F' {
			t.Fatalf("a bidi/format character survived: %q", card.Summary)
		}
	}
	if !strings.HasSuffix(card.Summary, "(daemon check)") {
		t.Fatalf("marker: %q", card.Summary)
	}
	// Long title and workspace plus five long hosts cannot push the hosts note or the marker off.
	long := strings.Repeat("h", 200)
	card = Build(Input{Class: ClassRead, TaskTitle: long, Workspace: long, Allowlist: []string{long, long, long, long, long, long, long}})
	if !strings.HasSuffix(card.Summary, "(daemon check)") || utf8.RuneCountInString(card.Summary) > 500 {
		t.Fatalf("the marker stays and the summary stays capped: %d runes: %q", utf8.RuneCountInString(card.Summary), card.Summary)
	}
}
