package browser

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// startHookedSession starts a session on a fake CDP answered by hook and
// attaches one ready tab (T1 on session S1).
func startHookedSession(t *testing.T, hook func(f *fakeCDP, c fakeCall) (any, string)) (*Session, *fakeCDP) {
	t.Helper()
	l := &fakeLauncher{t: t, hook: hook}
	pol, err := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: 4648}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(context.Background(), Config{Workspace: "ws1", Launcher: l, Policy: pol, Resolver: sessionResolver, IdleTimeout: time.Minute, CallTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	f, _ := l.last()
	attachReady(t, f, "S1", "T1", 1)
	return s, f
}

// domFake answers the DOM password scan and the AX tree of a snapshot.
type domFake struct {
	queryErr      string
	passwordNodes []int64         // nodeIds DOM.querySelectorAll returns
	backendByNode map[int64]int64 // DOM.describeNode: nodeId -> backendNodeId
	tree          func() []any    // Accessibility.getFullAXTree nodes
}

func (d *domFake) hook(f *fakeCDP, c fakeCall) (any, string) {
	switch c.Method {
	case "DOM.getDocument":
		return map[string]any{"root": map[string]any{"nodeId": 1}}, ""
	case "DOM.querySelectorAll":
		if d.queryErr != "" {
			return nil, d.queryErr
		}
		return map[string]any{"nodeIds": d.passwordNodes}, ""
	case "DOM.describeNode":
		id := int64(c.Params["nodeId"].(float64))
		return map[string]any{"node": map[string]any{"backendNodeId": d.backendByNode[id]}}, ""
	case "Accessibility.getFullAXTree":
		return map[string]any{"nodes": d.tree()}, ""
	}
	return defaultHook(f, c)
}

// axv is the {type, value} wrapper of an AX property.
func axv(v string) map[string]any { return map[string]any{"type": "string", "value": v} }

// axm builds one AX node for the fake tree.
func axm(id, parent, role, name string, backend int64, props map[string]any, children ...string) map[string]any {
	m := map[string]any{
		"nodeId":           id,
		"parentId":         parent,
		"role":             axv(role),
		"name":             axv(name),
		"backendDOMNodeId": backend,
		"childIds":         children,
	}
	if len(props) > 0 {
		ps := make([]any, 0, len(props))
		for k, v := range props {
			ps = append(ps, map[string]any{"name": k, "value": map[string]any{"value": v}})
		}
		m["properties"] = ps
	}
	return m
}

func TestSnapshotRendersTree(t *testing.T) {
	link := axm("l", "r", "link", "Docs", 10, map[string]any{"focused": true, "url": "http://evil.test/x"})
	st := axm("st", "h", "StaticText", "Hello world", 3, nil)
	h := axm("h", "r", "heading", "Welcome", 2, map[string]any{"level": 2}, "st")
	tb := axm("tb", "r", "textbox", "Search", 20, map[string]any{"required": true})
	tb["value"] = axv("q=1")
	cb := axm("cb", "r", "checkbox", "Subscribe", 21, map[string]any{"checked": "mixed"})
	gen := axm("g", "r", "generic", "", 4, nil, "b")
	btn := axm("b", "g", "button", "Go", 22, nil)
	root := axm("r", "", "WebArea", "Home", 1, nil, "h", "l", "tb", "cb", "g")
	d := &domFake{tree: func() []any { return []any{root, h, st, link, tb, cb, gen, btn} }}
	s, _ := startHookedSession(t, d.hook)

	snap, err := s.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		`- WebArea "Home"`,
		`  - heading "Welcome" [level=2]`,
		`    - text "Hello world"`,
		`  - link "Docs" [focused] ref=e1`,
		`  - textbox "Search" value="q=1" [required] ref=e2`,
		`  - checkbox "Subscribe" [checked=mixed] ref=e3`,
		`  - button "Go" ref=e4`,
	}, "\n")
	if snap.Text != want {
		t.Fatalf("rendered tree:\n%s\nwant:\n%s", snap.Text, want)
	}
	if snap.URL != "http://localhost:3000/" || snap.Title != "Home" {
		t.Fatalf("url/title: %q %q", snap.URL, snap.Title)
	}
	if snap.Nodes != 7 || snap.TotalNodes != 7 || snap.Truncated {
		t.Fatalf("counts: %+v", snap)
	}
	if refs := s.refsFor("T1"); !reflect.DeepEqual(refs, map[string]int64{"e1": 10, "e2": 20, "e3": 21, "e4": 22}) {
		t.Fatalf("refs: %v", refs)
	}
	if strings.Contains(snap.Text, "evil.test") {
		t.Fatal("the url property must never be printed")
	}
}

func TestSnapshotRefsPerSnapshot(t *testing.T) {
	rootA := axm("r", "", "WebArea", "Home", 1, nil, "l")
	link := axm("l", "r", "link", "Docs", 10, nil)
	rootB := axm("r", "", "WebArea", "Home", 1, nil, "b1", "b2")
	b1 := axm("b1", "r", "button", "One", 30, nil)
	b2 := axm("b2", "r", "button", "Two", 31, nil)
	cur := []any{rootA, link}
	d := &domFake{tree: func() []any { return cur }}
	s, _ := startHookedSession(t, d.hook)

	if _, err := s.Snapshot(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if refs := s.refsFor("T1"); !reflect.DeepEqual(refs, map[string]int64{"e1": 10}) {
		t.Fatalf("first snapshot refs: %v", refs)
	}
	cur = []any{rootB, b1, b2}
	if _, err := s.Snapshot(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if refs := s.refsFor("T1"); !reflect.DeepEqual(refs, map[string]int64{"e1": 30, "e2": 31}) {
		t.Fatalf("the second snapshot must replace the refs, got %v", refs)
	}
}

func TestSnapshotLimitsTruncateWithMarker(t *testing.T) {
	t.Run("node cap", func(t *testing.T) {
		const n = 2000
		ids := make([]string, 0, n-1)
		nodes := make([]any, 0, n)
		for i := 0; i < n-1; i++ {
			id := fmt.Sprintf("b%d", i)
			ids = append(ids, id)
		}
		nodes = append(nodes, axm("r", "", "WebArea", "P", 1, nil, ids...))
		for i, id := range ids {
			nodes = append(nodes, axm(id, "r", "button", id, int64(1000+i), nil))
		}
		d := &domFake{tree: func() []any { return nodes }}
		s, _ := startHookedSession(t, d.hook)

		snap, err := s.Snapshot(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if snap.Nodes != MaxSnapshotNodes || snap.TotalNodes != n || !snap.Truncated {
			t.Fatalf("counts: %+v", snap)
		}
		lines := strings.Split(snap.Text, "\n")
		if len(lines) != MaxSnapshotNodes+1 {
			t.Fatalf("lines: %d", len(lines))
		}
		if want := "[truncated: 800 more nodes not shown]"; lines[len(lines)-1] != want {
			t.Fatalf("marker: %q want %q", lines[len(lines)-1], want)
		}
		if refs := s.refsFor("T1"); len(refs) != MaxSnapshotNodes-1 {
			t.Fatalf("only rendered nodes get refs, got %d", len(refs))
		}
	})
	t.Run("char cap", func(t *testing.T) {
		const n = 300
		long := strings.Repeat("x", 190)
		ids := make([]string, 0, n)
		nodes := make([]any, 0, n+1)
		for i := 0; i < n; i++ {
			ids = append(ids, fmt.Sprintf("b%d", i))
		}
		nodes = append(nodes, axm("r", "", "WebArea", "P", 1, nil, ids...))
		for i, id := range ids {
			nodes = append(nodes, axm(id, "r", "button", long, int64(1000+i), nil))
		}
		d := &domFake{tree: func() []any { return nodes }}
		s, _ := startHookedSession(t, d.hook)

		snap, err := s.Snapshot(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if !snap.Truncated || snap.Nodes >= n+1 || snap.TotalNodes != n+1 {
			t.Fatalf("counts: %+v", snap)
		}
		lines := strings.Split(snap.Text, "\n")
		if want := fmt.Sprintf("[truncated: %d more nodes not shown]", snap.TotalNodes-snap.Nodes); lines[len(lines)-1] != want {
			t.Fatalf("marker: %q want %q", lines[len(lines)-1], want)
		}
		if body := strings.Join(lines[:len(lines)-1], "\n"); len(body) > MaxSnapshotChars {
			t.Fatalf("rendered text is %d chars, over the cap", len(body))
		}
	})
}

func TestSnapshotPasswordValueNeverReturned(t *testing.T) {
	t.Run("value and name hidden", func(t *testing.T) {
		pw := axm("pw", "r", "textbox", "Password", 55, map[string]any{"required": true})
		pw["value"] = axv("hunter2")
		root := axm("r", "", "WebArea", "Home", 1, nil, "pw")
		d := &domFake{
			passwordNodes: []int64{7},
			backendByNode: map[int64]int64{7: 55},
			tree:          func() []any { return []any{root, pw} },
		}
		s, _ := startHookedSession(t, d.hook)

		snap, err := s.Snapshot(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(snap.Text, "hunter2") {
			t.Fatal("a password value must never be rendered")
		}
		if strings.Contains(snap.Text, "Password") {
			t.Fatal("a password field's name must never be rendered")
		}
		if !strings.Contains(snap.Text, "- textbox (password)") {
			t.Fatalf("the field must be marked: %s", snap.Text)
		}
		if refs := s.refsFor("T1"); !reflect.DeepEqual(refs, map[string]int64{"e1": 55}) {
			t.Fatalf("the field keeps an actionable ref: %v", refs)
		}
	})
	t.Run("scan error fails closed", func(t *testing.T) {
		root := axm("r", "", "WebArea", "Home", 1, nil)
		d := &domFake{queryErr: "boom", tree: func() []any { return []any{root} }}
		s, f := startHookedSession(t, d.hook)

		if _, err := s.Snapshot(context.Background(), ""); err == nil {
			t.Fatal("a failed password scan must fail the snapshot")
		}
		if len(f.callsFor("Accessibility.getFullAXTree")) != 0 {
			t.Fatal("no tree may be fetched when the password scan failed")
		}
	})
}

func TestSnapshotNeverIncludesLinkURLs(t *testing.T) {
	link := axm("l", "r", "link", "Docs", 10, map[string]any{"url": "http://evil.test/x?token=1", "focused": true})
	root := axm("r", "", "WebArea", "Home", 1, nil, "l")
	d := &domFake{tree: func() []any { return []any{root, link} }}
	s, _ := startHookedSession(t, d.hook)

	snap, err := s.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snap.Text, "evil.test") || strings.Contains(snap.Text, "http") {
		t.Fatalf("a link url must never be printed: %s", snap.Text)
	}
	if !strings.Contains(snap.Text, `- link "Docs" [focused] ref=e1`) {
		t.Fatalf("the link itself renders: %s", snap.Text)
	}
}

func TestSnapshotCleansPageText(t *testing.T) {
	tb := axm("tb", "r", "textbox", "a\u202eb\u200bc\nd", 20, nil)
	tb["value"] = axv("v\u202e")
	st := axm("st", "r", "StaticText", "keep\u200bdrop", 3, nil)
	root := axm("r", "", "WebArea", "Home", 1, nil, "tb", "st")
	d := &domFake{tree: func() []any { return []any{root, tb, st} }}
	s, _ := startHookedSession(t, d.hook)

	snap, err := s.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snap.Text, `textbox "abc d" value="v"`) || !strings.Contains(snap.Text, `text "keepdrop"`) {
		t.Fatalf("names and values must be cleaned: %s", snap.Text)
	}
	if strings.ContainsAny(snap.Text, "\u202e\u200b") {
		t.Fatalf("bidi and zero-width characters must be stripped: %q", snap.Text)
	}
}
