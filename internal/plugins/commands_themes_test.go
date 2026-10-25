package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// themeJSON builds a theme file with every canonical token for the given
// schemes, each set to val; edit lets a test change the sets first.
func themeJSON(t *testing.T, val string, edit func(light, dark map[string]string), schemes ...string) string {
	t.Helper()
	sets := map[string]map[string]string{}
	for _, s := range schemes {
		m := map[string]string{}
		for _, tok := range ThemeTokens() {
			m[tok] = val
		}
		sets[s] = m
	}
	if edit != nil {
		edit(sets["light"], sets["dark"])
	}
	b, err := json.Marshal(sets)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const cmdThemeContributes = `"contributes":{` +
	`"commands":[{"name":"review","description":"Review the diff","prompt":"commands/review.md"},` +
	`{"name":"plan","prompt":"commands/plan.md"}],` +
	`"themes":[{"id":"dusk","label":"Dusk","file":"themes/dusk.json"}]}`

func cmdThemeFiles(t *testing.T) map[string]string {
	return map[string]string{
		"commands/review.md": "Review the staged diff for bugs.\n",
		"commands/plan.md":   "Plan the change first.",
		"themes/dusk.json":   themeJSON(t, "#112233", nil, "light", "dark"),
	}
}

func TestPluginCommands_ActiveOnly_SwitchOff_Empty(t *testing.T) {
	e := newContribEnv(t, nil)
	in := mustInstall(t, e, writePluginDir(t, manifestWith("demo", "1.0.0", cmdThemeContributes), cmdThemeFiles(t)))
	if len(e.m.Commands()) != 0 || len(e.m.Themes()) != 0 {
		t.Fatal("an installed but disabled plugin contributes nothing")
	}
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	cmds := e.m.Commands()
	if len(cmds) != 2 || cmds[0].Qualified != "demo:review" || cmds[0].Prompt != "Review the staged diff for bugs.\n" || cmds[0].Alias != "review" {
		t.Fatalf("%+v", cmds)
	}
	if th := e.m.Themes(); len(th) != 1 || th[0].Qualified != "demo:dusk" || len(th[0].Schemes) != 2 {
		t.Fatalf("%+v", th)
	}
	*e.on = false
	if len(e.m.Commands()) != 0 || len(e.m.Themes()) != 0 {
		t.Fatal("the global switch off must empty commands and themes")
	}
	*e.on = true
	if _, err := e.m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	if len(e.m.Commands()) != 0 || len(e.m.Themes()) != 0 {
		t.Fatal("disable must remove commands and themes")
	}
}

func TestSlashCommands_FirstPluginByIdWinsBetweenPlugins(t *testing.T) {
	e := newContribEnv(t, nil)
	one := `"contributes":{"commands":[{"name":"review","prompt":"c.md"}]}`
	files := map[string]string{"c.md": "text"}
	enableDemo(t, e, "zeta", one, files)
	enableDemo(t, e, "alpha", one, files)
	byQ := map[string]Command{}
	var order []string
	for _, c := range e.m.Commands() {
		byQ[c.Qualified] = c
		order = append(order, c.Qualified)
	}
	if strings.Join(order, ",") != "alpha:review,zeta:review" {
		t.Fatalf("plugin id order: %v", order)
	}
	if byQ["alpha:review"].Alias != "review" || byQ["zeta:review"].Alias != "" {
		t.Fatalf("the bare name belongs to the first plugin by id: %+v", byQ)
	}
	got, _ := e.m.Get("zeta")
	var found bool
	for _, c := range got.Conflicts {
		if c.Kind == "command" && c.Name == "review" && strings.Contains(c.Reason, "alpha") && strings.Contains(c.Reason, "zeta:review") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the loser must see the collision: %+v", got.Conflicts)
	}
	if g, _ := e.m.Get("alpha"); len(g.Conflicts) != 0 {
		t.Fatalf("the winner has no conflict: %+v", g.Conflicts)
	}
}

func TestPluginCommands_BadPromptFiles_RejectedAtInstall(t *testing.T) {
	big := strings.Repeat("x", maxPromptFileBytes+1)
	bad := map[string]struct {
		extra string
		files map[string]string
		want  string
	}{
		"missing":       {`"contributes":{"commands":[{"name":"a","prompt":"c.md"}]}`, map[string]string{}, "does not exist"},
		"empty":         {`"contributes":{"commands":[{"name":"a","prompt":"c.md"}]}`, map[string]string{"c.md": " \n"}, "empty"},
		"binary":        {`"contributes":{"commands":[{"name":"a","prompt":"c.md"}]}`, map[string]string{"c.md": "ab\x00cd"}, "UTF-8"},
		"invalid utf8":  {`"contributes":{"commands":[{"name":"a","prompt":"c.md"}]}`, map[string]string{"c.md": "\xff\xfe"}, "UTF-8"},
		"too big":       {`"contributes":{"commands":[{"name":"a","prompt":"c.md"}]}`, map[string]string{"c.md": big}, "cap"},
		"duplicate":     {`"contributes":{"commands":[{"name":"a","prompt":"c.md"},{"name":"a","prompt":"c.md"}]}`, map[string]string{"c.md": "x"}, "twice"},
		"extra field":   {`"contributes":{"commands":[{"name":"a","prompt":"c.md","exec":"rm"}]}`, map[string]string{"c.md": "x"}, ""},
		"escaping path": {`"contributes":{"commands":[{"name":"a","prompt":"../c.md"}]}`, map[string]string{"c.md": "x"}, "path"},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newContribEnv(t, nil)
			_, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", c.extra), c.files))
			var me *ManifestError
			if name == "extra field" {
				// Unknown keys inside a contribution entry are ignored today
				// (they carry no behaviour); the entry still inserts text only.
				if err != nil {
					t.Fatalf("unknown entry keys are ignored like unknown top-level keys: %v", err)
				}
				return
			}
			if !errors.As(err, &me) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a manifest error mentioning %q, got %v", c.want, err)
			}
		})
	}
}

func TestTheme_IncompleteTokenSet_RejectedWithMissingList(t *testing.T) {
	toks := ThemeTokens()
	e := newContribEnv(t, nil)
	files := cmdThemeFiles(t)
	files["themes/dusk.json"] = themeJSON(t, "#112233", func(_, dark map[string]string) {
		delete(dark, toks[0])
		delete(dark, toks[1])
	}, "light", "dark")
	_, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", cmdThemeContributes), files))
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("want a manifest error, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "dark scheme is missing 2 token(s)") || !strings.Contains(msg, toks[0]) || !strings.Contains(msg, toks[1]) {
		t.Fatalf("the missing tokens must be listed: %s", msg)
	}
	if strings.Contains(msg, "light scheme") {
		t.Fatalf("the complete scheme must not be blamed: %s", msg)
	}
	if _, gerr := e.st.GetPlugin("demo"); gerr == nil {
		t.Fatal("a rejected plugin must leave no row")
	}
	// An unknown token and an empty file are rejected too.
	for name, body := range map[string]string{
		"unknown":   themeJSON(t, "#112233", func(l, _ map[string]string) { l["--evil"] = "#fff" }, "light"),
		"no scheme": `{}`,
		"extra key": `{"light":{},"url":"x"}`,
	} {
		files["themes/dusk.json"] = body
		if _, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", cmdThemeContributes), files)); err == nil {
			t.Fatalf("%s: must be rejected", name)
		}
	}
}

func TestTheme_PartialSchemes_Accepted(t *testing.T) {
	e := newContribEnv(t, nil)
	files := cmdThemeFiles(t)
	files["themes/dusk.json"] = themeJSON(t, "#112233", nil, "dark")
	enableDemo(t, e, "demo", cmdThemeContributes, files)
	th := e.m.Themes()
	if len(th) != 1 || th[0].Schemes["dark"] == nil || th[0].Schemes["light"] != nil {
		t.Fatalf("a dark-only theme declares only dark: %+v", th)
	}
}

func TestTheme_HostileValues_Rejected(t *testing.T) {
	hostile := map[string]string{
		"url":              "url(https://evil.example/x.png)",
		"url upper":        "URL(//evil)",
		"var":              "var(--background)",
		"semicolon":        "#fff; background: url(x)",
		"brace close":      "#fff} body{display:none",
		"brace open":       "{#fff}",
		"import":           "@import 'evil.css'",
		"expression":       "expression(alert(1))",
		"calc":             "calc(1px + 2px)",
		"color-mix":        "color-mix(in oklab, red, blue)",
		"named color":      "red",
		"nested paren":     "rgb(0 0 0) url(x)",
		"newline":          "#fff\nurl(x)",
		"comment":          "#fff/*x*/",
		"backslash":        "\\75rl(x)",
		"quote":            `"#fff"`,
		"empty":            "",
		"long":             "#" + strings.Repeat("f", 100),
		"long fn":          "rgb(" + strings.Repeat("1", 80) + " 0 0)",
		"cyrillic digit":   "#ffff٣٣",
		"fullwidth hash":   "＃ffffff",
		"fullwidth digits": "rgb(１ ２ ３)",
		"nbsp":             "rgb(1 2 3)",
		"zero width":       "#fff​",
		"rtl override":     "#fff‮",
		"hex wrong len":    "#12345",
		"fn without close": "rgb(1 2 3",
		"fn url channel":   "rgb(url(x) 2 3)",
		"unit junk":        "12pxurl(x)",
		"important":        "#fff !important",
		"two values":       "#fff #000",
		"attr":             "attr(x)",
		"env":              "env(safe-area-inset-top)",
	}
	for name, val := range hostile {
		t.Run(name, func(t *testing.T) {
			if ValidThemeValue(val) {
				t.Fatalf("%q must not be a valid theme value", val)
			}
			// And through the whole install path, in one token of one scheme.
			e := newContribEnv(t, nil)
			files := cmdThemeFiles(t)
			tok := ThemeTokens()[3]
			files["themes/dusk.json"] = themeJSON(t, "#112233", func(l, _ map[string]string) { l[tok] = val }, "light")
			_, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", cmdThemeContributes), files))
			var me *ManifestError
			if !errors.As(err, &me) || !strings.Contains(err.Error(), tok) {
				t.Fatalf("hostile value must be rejected naming %s: %v", tok, err)
			}
		})
	}
	for _, ok := range []string{"#fff", "#FFFF", "#112233", "#11223344", "rgb(1 2 3)", "rgba(1, 2, 3, 0.5)", "rgb(1 2 3 / 50%)",
		"hsl(120 50% 50%)", "hsl(120deg, 50%, 50%)", "oklch(0.7 0.1 200)", "oklch(70% 0.1 200deg / 0.4)", "12px", "0.5rem", "-1px", "50%", "transparent"} {
		if !ValidThemeValue(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
}

func TestTheme_LabelAndFileLimits(t *testing.T) {
	e := newContribEnv(t, nil)
	files := cmdThemeFiles(t)
	long := `"contributes":{"themes":[{"id":"dusk","label":"` + strings.Repeat("a", 60) + `","file":"themes/dusk.json"}]}`
	if _, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", long), files)); err == nil || !strings.Contains(err.Error(), "label") {
		t.Fatalf("a long label is rejected: %v", err)
	}
	files["themes/dusk.json"] = strings.Repeat(" ", maxThemeFileBytes+1)
	if _, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", cmdThemeContributes), files)); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("an oversized theme file is rejected: %v", err)
	}
}

// cssBlock returns the brace-balanced block that starts at the first
// occurrence of marker in css.
func cssBlock(t *testing.T, css, marker string) string {
	t.Helper()
	i := strings.Index(css, marker)
	if i < 0 {
		t.Fatalf("%q not found in index.css", marker)
	}
	depth := 0
	for j := i; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return css[i:j]
			}
		}
	}
	t.Fatalf("%q block is not closed", marker)
	return ""
}

var cssDeclRe = regexp.MustCompile(`(?m)^\s+(--[a-z0-9-]+):`)

// TestTheme_TokenListDrift pins themetokens.json (the list the daemon
// validates plugin themes against) to the web theme: the colour tokens declared
// in BOTH the light `:root` block and the `.dark` block of index.css. A token
// added to or removed from the web theme without updating the list fails here
// (and in the web test of the same name, which also pins the TS copy).
func TestTheme_TokenListDrift(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "packages", "ui", "src", "index.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	declared := func(block string) map[string]bool {
		out := map[string]bool{}
		for _, m := range cssDeclRe.FindAllStringSubmatch(block, -1) {
			out[m[1]] = true
		}
		return out
	}
	light, dark := declared(cssBlock(t, css, "\n:root {")), declared(cssBlock(t, css, "\n.dark {"))
	nonColour := regexp.MustCompile(`^--(ui-font-size|elevation-shadow-|content-|duration-)`)
	var want []string
	for tok := range light {
		if dark[tok] && !nonColour.MatchString(tok) {
			want = append(want, tok)
		}
	}
	sort.Strings(want)
	got := ThemeTokens()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("internal/plugins/themetokens.json drifted from web/packages/ui/src/index.css\n missing from the list: %v\n not in the css: %v",
			diffStr(want, got), diffStr(got, want))
	}
	if len(got) < 100 {
		t.Fatalf("suspiciously few tokens: %d", len(got))
	}
}

// diffStr returns the entries of a that are not in b.
func diffStr(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}
