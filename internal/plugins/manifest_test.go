package plugins

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestManifest_Validation_FieldNamed(t *testing.T) {
	base := func(mut func(m map[string]string)) string {
		f := map[string]string{
			"id": `"demo"`, "name": `"Demo"`, "version": `"1.0.0"`, "api": `1`,
			"engines": `{"smind":">=0.1.0"}`,
		}
		mut(f)
		var parts []string
		for k, v := range f {
			if v != "" {
				parts = append(parts, fmt.Sprintf("%q:%s", k, v))
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	cases := []struct {
		name  string
		mut   func(m map[string]string)
		field string
		fix   string
	}{
		{"missing id", func(m map[string]string) { m["id"] = "" }, "id", "set id"},
		{"upper id", func(m map[string]string) { m["id"] = `"Demo"` }, "id", "a-z"},
		{"id too long", func(m map[string]string) { m["id"] = `"a` + strings.Repeat("b", 64) + `"` }, "id", "1-64"},
		{"id leading digit", func(m map[string]string) { m["id"] = `"1abc"` }, "id", "starting with a letter"},
		{"id windows device", func(m map[string]string) { m["id"] = `"con"` }, "id", "device name"},
		{"id traversal", func(m map[string]string) { m["id"] = `"../x"` }, "id", ""},
		{"id wrong type", func(m map[string]string) { m["id"] = `7` }, "id", ""},
		{"missing name", func(m map[string]string) { m["name"] = "" }, "name", "display name"},
		{"missing version", func(m map[string]string) { m["version"] = "" }, "version", "MAJOR.MINOR.PATCH"},
		{"bad version", func(m map[string]string) { m["version"] = `"one"` }, "version", "semantic versioning"},
		{"missing api", func(m map[string]string) { m["api"] = "" }, "api", "one of"},
		{"api unsupported", func(m map[string]string) { m["api"] = `2` }, "api", "supported plugin API"},
		{"api string", func(m map[string]string) { m["api"] = `"1"` }, "api", ""},
		{"missing engines", func(m map[string]string) { m["engines"] = "" }, "engines", "engines"},
		{"unknown engines key", func(m map[string]string) { m["engines"] = `{"smind":">=0.1.0","node":">=20"}` }, "engines", "only accepts"},
		{"engines range garbage", func(m map[string]string) { m["engines"] = `{"smind":"latest"}` }, "engines.smind", "semver range"},
		{"engines unsatisfied", func(m map[string]string) { m["engines"] = `{"smind":">=9.0.0"}` }, "engines.smind", "upgrade smind"},
		{"engines missing smind", func(m map[string]string) { m["engines"] = `{}` }, "engines.smind", "semver range"},
		{"unknown capability", func(m map[string]string) { m["capabilities"] = `["tasks.read","tasks.wirte"]` }, "capabilities[1]", "use one of"},
		{"duplicate capability", func(m map[string]string) { m["capabilities"] = `["tasks.read","tasks.read"]` }, "capabilities[1]", "duplicate"},
		{"capabilities object", func(m map[string]string) { m["capabilities"] = `{"tasks.read":true}` }, "capabilities", "array"},
		{"empty process", func(m map[string]string) { m["process"] = `{"command":[]}` }, "process.command", "argv"},
		{"command path escape", func(m map[string]string) {
			m["contributes"] = `{"skills":["../../etc"]}`
		}, "contributes.skills[0]", "without"},
		{"absolute path", func(m map[string]string) { m["contributes"] = `{"profiles":["/etc/passwd"]}` }, "contributes.profiles[0]", "relative"},
		{"backslash path", func(m map[string]string) { m["contributes"] = `{"themes":[{"id":"t","file":"a\\b.json"}]}` }, "contributes.themes[0].file", "forward-slashed"},
		{"drive path", func(m map[string]string) { m["contributes"] = `{"themes":[{"id":"t","file":"C:/x.json"}]}` }, "contributes.themes[0].file", "relative"},
		{"data path reserved", func(m map[string]string) { m["contributes"] = `{"skills":["data/x"]}` }, "contributes.skills[0]", ""},
		{"hook without process", func(m map[string]string) {
			m["contributes"] = `{"hooks":[{"event":"run.finished"}]}`
		}, "contributes.hooks[0]", "declare process"},
		{"unknown hook event", func(m map[string]string) {
			m["process"] = `{"command":["x"]}`
			m["contributes"] = `{"hooks":[{"event":"run.exploded"}]}`
		}, "contributes.hooks[0].event", "use one of"},
		{"command both prompt and rpc", func(m map[string]string) {
			m["contributes"] = `{"commands":[{"name":"c","prompt":"p.md","rpc":"r"}]}`
		}, "contributes.commands[0]", "exactly one"},
		{"mcp stdio without command", func(m map[string]string) {
			m["contributes"] = `{"mcpServers":[{"name":"s","transport":"stdio"}]}`
		}, "contributes.mcpServers[0].command", "argv"},
		{"mcp bad transport", func(m map[string]string) {
			m["contributes"] = `{"mcpServers":[{"name":"s","transport":"carrier-pigeon"}]}`
		}, "contributes.mcpServers[0].transport", "stdio, http or sse"},
		{"setting bad type", func(m map[string]string) { m["settings"] = `[{"key":"k","type":"blob","label":"K"}]` }, "settings[0].type", "string, secret"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(base(c.mut)), "0.15.0")
			var me *ManifestError
			if !errors.As(err, &me) {
				t.Fatalf("want ManifestError, got %v", err)
			}
			found := false
			for _, fe := range me.Errs {
				if fe.Field == c.field {
					found = true
					if fe.Fix == "" || fe.Problem == "" {
						t.Errorf("error for %s lacks problem or fix: %+v", c.field, fe)
					}
					if c.fix != "" && !strings.Contains(strings.ToLower(fe.Fix+" "+fe.Problem), strings.ToLower(c.fix)) {
						t.Errorf("fix %q does not mention %q", fe.Fix, c.fix)
					}
				}
			}
			if !found {
				t.Fatalf("no error named field %q: %v", c.field, me.Fields())
			}
			if CodeOf(err) != CodeBadManifest {
				t.Fatalf("code = %q", CodeOf(err))
			}
		})
	}

	t.Run("valid manifest parses", func(t *testing.T) {
		m, err := ParseManifest([]byte(goodManifest), "0.15.0")
		if err != nil || m.ID != "demo" {
			t.Fatalf("%v %v", m, err)
		}
	})
	t.Run("size cap", func(t *testing.T) {
		big := `{"id":"demo","pad":"` + strings.Repeat("x", MaxManifestBytes) + `"}`
		_, err := ParseManifest([]byte(big), "0.15.0")
		var me *ManifestError
		if !errors.As(err, &me) || me.Errs[0].Field != "(file)" || !strings.Contains(me.Errs[0].Problem, "cap") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("not JSON", func(t *testing.T) {
		_, err := ParseManifest([]byte("nope"), "0.15.0")
		var me *ManifestError
		if !errors.As(err, &me) {
			t.Fatal(err)
		}
	})
	t.Run("several problems all reported", func(t *testing.T) {
		_, err := ParseManifest([]byte(`{"id":"BAD","api":9}`), "0.15.0")
		var me *ManifestError
		errors.As(err, &me)
		if len(me.Errs) < 4 {
			t.Fatalf("want every problem, got %v", me.Fields())
		}
	})
}

func TestManifest_UnknownTopLevel_Ignored(t *testing.T) {
	m, err := ParseManifest([]byte(`{"id":"demo","name":"D","version":"1.0.0","api":1,"engines":{"smind":"*"},"futureKey":{"x":1},"license":"MIT"}`), "0.15.0")
	if err != nil || m.ID != "demo" {
		t.Fatalf("%v %v", m, err)
	}
}

func TestManifest_PathsConfined(t *testing.T) {
	for _, p := range []string{"a/b.md", "skills/review", "x"} {
		if err := ConfinedPath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"", "..", "../x", "a/../b", "/x", `\x`, "a\\b", "C:x", "a//b", "./a", "a/", "data", "data/x", "a\x00b", "a:b"} {
		if err := ConfinedPath(p); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
}

func TestEngines_SemverAndPrerelease(t *testing.T) {
	cases := []struct {
		rng, ver string
		want     bool
	}{
		{">=0.15.0", "0.15.0", true},
		{">=0.15.0", "0.14.9", false},
		{">=0.15.0", "0.15.0-rc.1", false}, // a prerelease is below its release
		{">=0.15.0-rc.1", "0.15.0-rc.2", true},
		{">=0.15.0-rc.1", "0.15.0", true},
		{">=0.15.0-rc.2", "0.15.0-rc.1", false},
		{">=0.14.0", "0.15.0-rc.1", false}, // npm rule: no prerelease comparator on 0.15.0
		{">=0.14.0 <0.16.0", "0.15.3", true},
		{">=0.14.0 <0.16.0", "0.16.0", false},
		{"^0.15.0", "0.15.9", true},
		{"^0.15.0", "0.16.0", false},
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{">1.0.0 || <0.5.0", "0.4.0", true},
		{">1.0.0 || <0.5.0", "0.7.0", false},
		{"*", "3.4.5", true},
		{"v1.0.0", "1.0.0+build7", true},
		{">=1.0.0-alpha.2", "1.0.0-alpha.10", true}, // numeric identifiers compare numerically
		{">=1.0.0-alpha", "1.0.0-alpha.1", true},
		{"<1.0.0-beta", "1.0.0-alpha", true},
	}
	for _, c := range cases {
		r, err := parseRange(c.rng)
		if err != nil {
			t.Fatalf("%s: %v", c.rng, err)
		}
		v, err := parseSemver(c.ver)
		if err != nil {
			t.Fatalf("%s: %v", c.ver, err)
		}
		if got := r.satisfies(v); got != c.want {
			t.Errorf("%q satisfies %q = %v, want %v", c.ver, c.rng, got, c.want)
		}
	}
	for _, bad := range []string{"", "1.2", "01.2.3", "a.b.c", "1.2.3-", ">=", "1.2.3.4", ">=1.x"} {
		if _, err := parseRange(bad); err == nil {
			t.Errorf("range %q should be rejected", bad)
		}
	}
	// A development build satisfies any range, so a developer can run what they build.
	if _, err := ParseManifest([]byte(`{"id":"demo","name":"D","version":"1.0.0","api":1,"engines":{"smind":">=99.0.0"}}`), "dev"); err != nil {
		t.Fatalf("dev build: %v", err)
	}
	// A real version below the range is refused with the field named.
	if _, err := ParseManifest([]byte(`{"id":"demo","name":"D","version":"1.0.0","api":1,"engines":{"smind":">=99.0.0"}}`), "0.15.0"); err == nil {
		t.Fatal("engines gate not enforced")
	}
}
