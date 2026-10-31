package uiview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var rpcEnv = Env{RPCs: map[string]bool{"refresh": true}}

// errPath returns the JSON path of a *Error, failing the test when err is
// nil or carries no path.
func errPath(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got none")
	}
	var ve *Error
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *uiview.Error, got %T: %v", err, err)
	}
	if ve.Path == "" {
		t.Fatalf("expected an error with a JSON path, got %q", ve.Msg)
	}
	return ve.Path
}

func nodeDoc(node string) []byte {
	return []byte(`{"ui":1,"root":` + node + `}`)
}

// nestedStacks nests n nodes: n-1 stacks around a text leaf, so the leaf
// sits at depth n and the whole chain holds n nodes.
func nestedStacks(n int) []byte {
	chain := `{"type":"text","text":"deep"}`
	for i := 1; i < n; i++ {
		chain = `{"type":"stack","children":[` + chain + `]}`
	}
	return nodeDoc(chain)
}

// childrenDoc builds a stack with n text children (n+1 nodes in total).
func childrenDoc(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"stack","children":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"type":"text","text":"n%d"}`, i)
	}
	b.WriteString(`]}`)
	return nodeDoc(b.String())
}

// markdownStackDoc builds a stack of markdown nodes whose texts fill MaxString
// each, sized so the whole document is exactly total bytes (the last text is
// the remainder). One byte over MaxDocBytes is what the cap test needs.
func markdownStackDoc(t *testing.T, total int) []byte {
	t.Helper()
	const head = `{"ui":1,"root":{"type":"stack","children":[`
	const tail = `]}}`
	full := `{"type":"markdown","text":"` + strings.Repeat("a", MaxString) + `"},`
	n := (total - len(head) - len(tail)) / len(full)
	last := total - len(head) - len(tail) - n*len(full) - len(`{"type":"markdown","text":""}`)
	if last < 0 || last > MaxString {
		t.Fatalf("cannot build a %d-byte document (last text %d)", total, last)
	}
	var b strings.Builder
	b.WriteString(head)
	for i := 0; i < n; i++ {
		b.WriteString(full)
	}
	fmt.Fprintf(&b, `{"type":"markdown","text":"%s"}`, strings.Repeat("a", last))
	b.WriteString(tail)
	doc := []byte(b.String())
	if len(doc) != total {
		t.Fatalf("built %d bytes, wanted %d", len(doc), total)
	}
	return doc
}

func keyValueItems(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"keyValue","items":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"key":"k%d","value":"v%d"}`, i, i)
	}
	b.WriteString(`]}`)
	return nodeDoc(b.String())
}

func tableColumns(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"table","columns":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"key":"c%d","label":"C%d"}`, i, i)
	}
	b.WriteString(`],"rows":[{"c0":"x"}]}`)
	return nodeDoc(b.String())
}

func formFields(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"form","id":"f","fields":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"f%d","type":"text","label":"F%d"}`, i, i)
	}
	b.WriteString(`],"submit":{"label":"Go","action":{"kind":"rpc","rpc":"refresh"}}}`)
	return nodeDoc(b.String())
}

func TestViewSchema_Validate_CapsDepthNodesBytes(t *testing.T) {
	depth13 := "$.root" + strings.Repeat(".children[0]", 12)
	cases := []struct {
		name    string
		doc     []byte
		wantErr string // "" means the document must validate
	}{
		{"depth 12 nodes ok", nestedStacks(12), ""},
		{"depth 13 over cap", nestedStacks(13), depth13},
		{"1000 nodes ok", childrenDoc(999), ""},
		{"1001 nodes over cap", childrenDoc(1000), "$.root.children[999]"},
		{"doc of exactly MaxDocBytes ok", markdownStackDoc(t, MaxDocBytes), ""},
		{"doc over MaxDocBytes", markdownStackDoc(t, MaxDocBytes+1), "$"},
		{"string of exactly MaxString ok", nodeDoc(`{"type":"text","text":"` + strings.Repeat("a", MaxString) + `"}`), ""},
		{"string over MaxString", nodeDoc(`{"type":"text","text":"` + strings.Repeat("a", MaxString+1) + `"}`), "$.root.text"},
		{"500 rows ok", keyValueItems(500), ""},
		{"501 rows over cap", keyValueItems(501), "$.root.items[500]"},
		{"12 columns ok", tableColumns(12), ""},
		{"13 columns over cap", tableColumns(13), "$.root.columns[12]"},
		{"24 form fields ok", formFields(24), ""},
		{"25 form fields over cap", formFields(25), "$.root.fields[24]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Validate(tc.doc, rpcEnv)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				if len(out) == 0 {
					t.Fatal("expected a normalised document")
				}
				return
			}
			// Over a cap is an error, never a truncated document.
			if out != nil {
				t.Fatalf("expected no output for an over-cap document, got %d bytes", len(out))
			}
			if got := errPath(t, err); got != tc.wantErr {
				t.Fatalf("error path = %q, want %q (msg: %s)", got, tc.wantErr, err)
			}
		})
	}
}

// fixtureFiles lists the shared fixture suite (D7): the same files the web
// and mobile renderers are tested against.
func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func TestViewSchema_EveryNodeTypeHasFixture(t *testing.T) {
	files := fixtureFiles(t)
	if len(files) == 0 {
		t.Skip("fixture suite not generated yet")
	}
	var corpus []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			Doc    json.RawMessage `json:"doc"`
			Expect struct {
				Valid *bool `json:"valid"`
			} `json:"expect"`
		}
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if fx.Expect.Valid == nil || !*fx.Expect.Valid {
			continue
		}
		// The files are pretty-printed; compact the doc so the exact
		// `"type":"x"` / `"kind":"x"` forms below are what is matched.
		compact, err := json.Marshal(fx.Doc)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		corpus = append(corpus, string(compact))
	}
	joined := strings.Join(corpus, "\n")
	for _, typ := range NodeTypes {
		if !strings.Contains(joined, `"type":"`+typ+`"`) {
			t.Errorf("no valid fixture contains node type %q", typ)
		}
	}
	for _, kind := range ActionKinds {
		if !strings.Contains(joined, `"kind":"`+kind+`"`) {
			t.Errorf("no valid fixture contains action kind %q", kind)
		}
	}
}

func TestViewSchema_UnknownNodeType_IsInvalidForGo_ButClientsPlaceholder(t *testing.T) {
	// The daemon refuses an unknown type outright; the placeholder a client
	// renders instead is PC2/PC3 behaviour, so "card" must not exist here.
	if nodeTypeSet["card"] {
		t.Fatal("card must not be a node type")
	}
	cases := []struct {
		name string
		doc  []byte
		path string
	}{
		{"root", nodeDoc(`{"type":"card","text":"x"}`), "$.root.type"},
		{"nested", nodeDoc(`{"type":"stack","children":[{"type":"neon"}]}`), "$.root.children[0].type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Validate(tc.doc, rpcEnv)
			if out != nil {
				t.Fatal("an unknown node type must not validate")
			}
			if got := errPath(t, err); got != tc.path {
				t.Fatalf("error path = %q, want %q", got, tc.path)
			}
			if !strings.HasPrefix(err.Error(), "view_invalid at ") {
				t.Fatalf("error text = %q", err.Error())
			}
		})
	}
}

func TestViewSchema_Validate_Deterministic(t *testing.T) {
	// Keys deliberately out of order; the normalised form must not depend on it.
	doc := []byte(`{
		"root": {
			"children": [
				{"text": "b", "type": "text", "tone": "muted"},
				{"type": "table",
				 "rows": [{"b": 1, "a": "x", "c": null, "d": true}],
				 "columns": [{"label": "B", "key": "b"}, {"key": "a", "label": "A"},
				             {"key": "c", "label": "C", "align": "end"}, {"key": "d", "label": "D"}]},
				{"type": "button", "action": {"url": "https://Example.com:8443/a?x=1", "kind": "openUrl"}, "label": "Open"}
			],
			"type": "stack", "gap": "md"
		},
		"title": "Panel",
		"ui": 1
	}`)
	first, err := Validate(doc, Env{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Validate(doc, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("two runs differ:\n%s\n%s", first, second)
	}
	if !json.Valid(first) {
		t.Fatal("output is not valid JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, first); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), first) {
		t.Fatal("output is not compact JSON")
	}
	// Re-encoding the decoded output must reproduce it byte for byte: the
	// key order is canonical, not an accident of insertion order.
	var round map[string]any
	dec := json.NewDecoder(bytes.NewReader(first))
	dec.UseNumber()
	if err := dec.Decode(&round); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(round)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, first) {
		t.Fatalf("output key order is not canonical:\n%s\n%s", again, first)
	}
}
