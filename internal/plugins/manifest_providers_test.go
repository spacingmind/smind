package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const providerFixtureDir = "providerfixtures/testdata"

type providerExpect struct {
	Valid   bool   `json:"valid"`
	Field   string `json:"field"`
	Display *struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		Icon    string `json:"icon"`
		Secrets []struct {
			Name     string `json:"name"`
			Label    string `json:"label"`
			Required bool   `json:"required"`
		} `json:"secrets"`
		Models int `json:"models"`
		Modes  int `json:"modes"`
	} `json:"display"`
}

func providerManifest(entry string) []byte {
	return []byte(`{"id":"demo","name":"Demo","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},"contributes":{"providers":[` + entry + `]}}`)
}

func fieldMatches(actual, want string) bool {
	return actual == want || strings.HasPrefix(actual, want+".") || strings.HasPrefix(actual, want+"[")
}

// TestProviderManifest_FixtureSuite_Go is the Go side of the shared fixtures
// (web and mobile read the same files for the display model).
func TestProviderManifest_FixtureSuite_Go(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(providerFixtureDir, "*.provider.json"))
	if err != nil || len(files) < 40 {
		t.Fatalf("fixtures: %d files, err %v", len(files), err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".provider.json")
		t.Run(name, func(t *testing.T) {
			entry, _ := os.ReadFile(f)
			raw, err := os.ReadFile(filepath.Join(providerFixtureDir, name+".expect.json"))
			if err != nil {
				t.Fatal(err)
			}
			var exp providerExpect
			if err := json.Unmarshal(raw, &exp); err != nil {
				t.Fatal(err)
			}
			m, perr := ParseManifest(providerManifest(string(entry)), "0.15.0")
			if exp.Valid {
				if perr != nil {
					t.Fatalf("want valid, got %v", perr)
				}
				p := m.Contributes.Providers[0]
				d := exp.Display
				if ProviderID("demo", p.ID) != d.ID || p.Label != d.Label || p.Icon != d.Icon ||
					len(p.ACP.SecretEnv) != len(d.Secrets) || len(p.Models) != d.Models || len(p.Modes) != d.Modes {
					t.Fatalf("display model = %+v, want %+v", p, d)
				}
				for i, s := range d.Secrets {
					g := p.ACP.SecretEnv[i]
					if g.Name != s.Name || g.Label != s.Label || g.Required != s.Required {
						t.Fatalf("secret %d = %+v", i, g)
					}
				}
				return
			}
			var me *ManifestError
			if !errors.As(perr, &me) {
				t.Fatalf("want a manifest error on %s, got %v", exp.Field, perr)
			}
			for _, fe := range me.Errs {
				if fieldMatches(fe.Field, exp.Field) && fe.Problem != "" && fe.Fix != "" {
					return
				}
			}
			t.Fatalf("no error named %s with a problem and a fix; got %v", exp.Field, perr)
		})
	}
}

func TestProviderManifest_Valid(t *testing.T) {
	m, err := ParseManifest(providerManifest(`{"id":"a","label":"A","acp":{"command":["bin/a"]}}`), "0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.Providers) != 1 || m.Process != nil {
		t.Fatalf("manifest = %+v", m.Contributes)
	}
}

func TestProviderManifest_DuplicateID(t *testing.T) {
	e := `{"id":"a","label":"A","acp":{"command":["bin/a"]}}`
	_, err := ParseManifest(providerManifest(e+","+e), "0.15.0")
	var me *ManifestError
	if !errors.As(err, &me) || !fieldMatches(me.Errs[0].Field, "contributes.providers[1].id") {
		t.Fatalf("err = %v", err)
	}
}

// TestProviderManifest_EnvChecker_SameAsHookEnv: one function backs both the
// hook env refusal and the provider env refusal.
func TestProviderManifest_EnvChecker_SameAsHookEnv(t *testing.T) {
	for _, name := range []string{"PATH", "HOME", "SMIND_X", "ANTHROPIC_BASE_URL", "LD_PRELOAD", "NODE_OPTIONS", "FOO_API_KEY", "HTTPS_PROXY", "foo", "GIT_DIR"} {
		hook := refuseHookEnv(name, "v")
		prov := refuseEnvName(name, false)
		if (hook == "") != (prov == "") {
			t.Errorf("%s: hook=%q provider=%q disagree", name, hook, prov)
		}
		if prov == "" {
			t.Errorf("%s should be refused", name)
		}
	}
	if refuseEnvName("FOO_API_KEY", true) != "" {
		t.Error("a secret slot may end in _KEY")
	}
	if refuseEnvName("HTTPS_PROXY", true) == "" || refuseEnvName("SMIND_X", true) == "" || refuseEnvName("PATH", true) == "" {
		t.Error("a secret slot may not be a proxy, SMIND_* or PATH")
	}
}

func TestPluginInstall_ReservedPluginIDs_Refused(t *testing.T) {
	for _, id := range ReservedPluginIDs {
		_, err := ParseManifest([]byte(`{"id":"`+id+`","name":"X","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"}}`), "0.15.0")
		var me *ManifestError
		if !errors.As(err, &me) {
			t.Fatalf("%s: err = %v", id, err)
		}
		found := false
		for _, fe := range me.Errs {
			found = found || (fe.Field == "id" && strings.Contains(fe.Problem, "reserved_plugin_id"))
		}
		if !found {
			t.Errorf("%s: no reserved_plugin_id error in %v", id, err)
		}
	}
}
