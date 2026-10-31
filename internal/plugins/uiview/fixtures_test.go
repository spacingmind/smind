package uiview

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the normalised document inside every valid fixture; the
// committed files carry it, and a plain run asserts it still matches.
var update = flag.Bool("update", false, "rewrite the normalised documents inside the fixture files")

// fixture is one file of the shared suite (plan D7): the web and mobile
// renderers read the very same files, so a change here is visible on every
// client.
type fixture struct {
	Name string `json:"name"`
	Env  struct {
		RPCs   []string `json:"rpcs"`
		Panels []string `json:"panels"`
	} `json:"env"`
	Doc    json.RawMessage `json:"doc"`
	Expect struct {
		Valid      *bool           `json:"valid,omitempty"`
		Invalid    string          `json:"invalid,omitempty"`
		Normalized json.RawMessage `json:"normalized,omitempty"`
	} `json:"expect"`
}

func (f fixture) env() Env {
	e := Env{RPCs: map[string]bool{}, Panels: map[string]bool{}}
	for _, r := range f.Env.RPCs {
		e.RPCs[r] = true
	}
	for _, p := range f.Env.Panels {
		e.Panels[p] = true
	}
	return e
}

func TestViewSchema_FixtureSuite_Go(t *testing.T) {
	files := fixtureFiles(t)
	if len(files) == 0 {
		t.Fatal("no fixtures in testdata/fixtures")
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx fixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			name := strings.TrimSuffix(filepath.Base(path), ".json")
			if fx.Name != name {
				t.Fatalf("fixture name %q does not match its file %s.json", fx.Name, name)
			}
			out, err := Validate(fx.Doc, fx.env())
			if fx.Expect.Invalid != "" {
				if out != nil {
					t.Fatal("an invalid fixture must produce no output")
				}
				if got := errPath(t, err); got != fx.Expect.Invalid {
					t.Fatalf("error path = %q, want %q (msg: %v)", got, fx.Expect.Invalid, err)
				}
				return
			}
			if fx.Expect.Valid == nil || !*fx.Expect.Valid {
				t.Fatalf("fixture must state expect.valid true or expect.invalid")
			}
			if err != nil {
				t.Fatalf("expected a valid fixture, got %v", err)
			}
			if *update {
				fx.Expect.Normalized = json.RawMessage(out)
				b, err := json.MarshalIndent(fx, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if len(fx.Expect.Normalized) == 0 {
				t.Fatalf("no normalised document; run go test ./internal/plugins/uiview -run FixtureSuite -update")
			}
			var want, got bytes.Buffer
			if err := json.Compact(&want, fx.Expect.Normalized); err != nil {
				t.Fatal(err)
			}
			if err := json.Compact(&got, out); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want.Bytes(), got.Bytes()) {
				t.Fatalf("normalised document changed:\n fixture: %s\n validate: %s", want.Bytes(), got.Bytes())
			}
		})
	}
}
