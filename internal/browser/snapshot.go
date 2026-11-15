package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Snapshot limits: a page can hold tens of thousands of AX nodes; the agent
// gets a bounded rendering, never the whole tree.
const (
	MaxSnapshotNodes = 1200
	MaxSnapshotChars = 40000
	maxPasswordScan  = 100
	// maxSnapshotDepth bounds the recursion: a hostile page can nest nodes
	// without limit, and a Go stack overflow is fatal for the whole daemon.
	maxSnapshotDepth = 100
)

// Snapshot is the rendered accessibility tree of one tab.
type Snapshot struct {
	URL, Title string
	Text       string
	Nodes      int // lines rendered
	TotalNodes int // rendered-candidate nodes in the whole tree
	Truncated  bool
}

// axValue is the {type, value} wrapper CDP uses for AX strings and values.
type axValue struct {
	Value json.RawMessage `json:"value"`
}

// str returns the value as text: strings unquoted, numbers and bools raw.
func (v axValue) str() string {
	if len(v.Value) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(v.Value, &s) == nil {
		return s
	}
	return string(v.Value)
}

type axProp struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}

type axNode struct {
	NodeID           string   `json:"nodeId"`
	Ignored          bool     `json:"ignored"`
	Role             axValue  `json:"role"`
	Name             axValue  `json:"name"`
	Value            axValue  `json:"value"`
	Properties       []axProp `json:"properties"`
	ChildIDs         []string `json:"childIds"`
	ParentID         string   `json:"parentId"`
	BackendDOMNodeID int64    `json:"backendDOMNodeId"`
}

// Roles whose value may be shown (never for a password field), and roles
// that get a ref the agent can act on.
var snapshotValueRoles = map[string]bool{
	"textbox": true, "searchbox": true, "combobox": true, "spinbutton": true, "slider": true,
}

var snapshotRefRoles = map[string]bool{
	"link": true, "button": true, "textbox": true, "searchbox": true, "combobox": true,
	"checkbox": true, "radio": true, "switch": true, "tab": true, "menuitem": true,
	"option": true, "slider": true, "spinbutton": true, "treeitem": true,
}

// Only these properties are ever read; the url property and anything
// href-like is deliberately absent.
var snapshotStateOrder = []string{"focused", "disabled", "checked", "expanded", "selected", "required", "readonly", "invalid", "pressed", "level"}

func axStates(props []axProp) []string {
	have := make(map[string]string, len(props))
	for _, p := range props {
		if _, dup := have[p.Name]; !dup {
			have[p.Name] = p.Value.str()
		}
	}
	var out []string
	for _, k := range snapshotStateOrder {
		v, ok := have[k]
		if !ok {
			continue
		}
		// checked/expanded/level are tri-state or numeric: printed whenever
		// valued; the rest only when true.
		if k == "checked" || k == "expanded" || k == "level" {
			if v != "" {
				out = append(out, k+"="+cleanText(v, 200))
			}
			continue
		}
		if v == "true" {
			out = append(out, k)
		}
	}
	return out
}

// Snapshot renders the tab's accessibility tree. Password fields are located
// FIRST and any error there fails the whole call: a snapshot is never
// rendered without knowing which fields must stay hidden.
func (s *Session) Snapshot(ctx context.Context, tabID string) (Snapshot, error) {
	ctx, cancel, err := s.begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer cancel()
	tab, err := s.tabFor(tabID)
	if err != nil {
		return Snapshot{}, err
	}
	passwords, err := s.passwordBackendNodes(ctx, tab)
	if err != nil {
		return Snapshot{}, err
	}
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := s.tabCall(ctx, tab, "Accessibility.getFullAXTree", nil, &tree); err != nil {
		return Snapshot{}, err
	}
	if len(tree.Nodes) == 0 {
		return Snapshot{}, errors.New("browser: the accessibility tree is empty")
	}
	r := &snapRender{pw: passwords, byID: make(map[string]*axNode, len(tree.Nodes)), seen: map[string]bool{}, refs: map[string]int64{}}
	for i := range tree.Nodes {
		r.byID[tree.Nodes[i].NodeID] = &tree.Nodes[i]
	}
	root := &tree.Nodes[0]
	for i := range tree.Nodes {
		if tree.Nodes[i].ParentID == "" {
			root = &tree.Nodes[i]
			break
		}
	}
	r.visit(root, 0)
	if r.full {
		r.lines = append(r.lines, fmt.Sprintf("[truncated: %d more nodes not shown]", r.total-r.shown))
	}
	s.setRefs(tab.targetID, r.refs)
	info, err := s.tabInfo(ctx, tab.targetID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		URL:        info.URL,
		Title:      info.Title,
		Text:       strings.Join(r.lines, "\n"),
		Nodes:      r.shown,
		TotalNodes: r.total,
		Truncated:  r.full,
	}, nil
}

// passwordBackendNodes collects the backend node ids of the tab's password
// inputs. Any failure is an error: the caller must not render a snapshot.
func (s *Session) passwordBackendNodes(ctx context.Context, t *tabState) (map[int64]bool, error) {
	var doc struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := s.tabCall(ctx, t, "DOM.getDocument", map[string]any{"depth": 0}, &doc); err != nil {
		return nil, fmt.Errorf("browser: password scan (DOM.getDocument): %w", err)
	}
	var sel struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := s.tabCall(ctx, t, "DOM.querySelectorAll", map[string]any{"nodeId": doc.Root.NodeID, "selector": "input[type=password i]"}, &sel); err != nil {
		return nil, fmt.Errorf("browser: password scan (DOM.querySelectorAll): %w", err)
	}
	n := len(sel.NodeIDs)
	if n > maxPasswordScan {
		n = maxPasswordScan
	}
	set := make(map[int64]bool, n)
	for _, id := range sel.NodeIDs[:n] {
		var d struct {
			Node struct {
				BackendNodeID int64 `json:"backendNodeId"`
			} `json:"node"`
		}
		if err := s.tabCall(ctx, t, "DOM.describeNode", map[string]any{"nodeId": id}, &d); err != nil {
			return nil, fmt.Errorf("browser: password scan (DOM.describeNode): %w", err)
		}
		if d.Node.BackendNodeID != 0 {
			set[d.Node.BackendNodeID] = true
		}
	}
	return set, nil
}

// snapRender walks the AX tree depth-first and builds the text, counting
// every rendered-candidate node even after a limit stopped the rendering.
type snapRender struct {
	pw     map[int64]bool
	byID   map[string]*axNode
	seen   map[string]bool
	lines  []string
	chars  int
	refs   map[string]int64
	refSeq int
	shown  int
	total  int
	full   bool
}

func (r *snapRender) visit(n *axNode, depth int) {
	if n == nil || r.seen[n.NodeID] {
		return
	}
	r.seen[n.NodeID] = true
	if depth > maxSnapshotDepth {
		r.total++ // counted in the truncation marker, never rendered, never descended into
		r.full = true
		return
	}
	role := cleanText(n.Role.str(), 200)
	skip := n.Ignored || ((role == "none" || role == "generic" || role == "InlineTextBox" || role == "LineBreak") && cleanText(n.Name.str(), 200) == "")
	if !skip {
		r.total++
		if !r.full {
			ref := ""
			if n.BackendDOMNodeID != 0 && snapshotRefRoles[role] {
				ref = "e" + strconv.Itoa(r.refSeq+1)
			}
			line := r.line(n, role, ref, depth)
			sep := 0
			if len(r.lines) > 0 {
				sep = 1
			}
			if r.shown >= MaxSnapshotNodes || r.chars+sep+len(line) > MaxSnapshotChars {
				r.full = true
			} else {
				r.lines = append(r.lines, line)
				r.chars += sep + len(line)
				r.shown++
				if ref != "" {
					r.refSeq++
					r.refs[ref] = n.BackendDOMNodeID
				}
			}
		}
	}
	childDepth := depth
	if !skip {
		childDepth = depth + 1
	}
	for _, id := range n.ChildIDs {
		r.visit(r.byID[id], childDepth)
	}
}

func (r *snapRender) line(n *axNode, role, ref string, depth int) string {
	display := role
	if role == "StaticText" {
		display = "text"
	}
	pw := n.BackendDOMNodeID != 0 && r.pw[n.BackendDOMNodeID]
	var b strings.Builder
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString("- ")
	b.WriteString(display)
	if pw {
		b.WriteString(" (password)")
	} else if name := cleanText(n.Name.str(), 200); name != "" {
		b.WriteString(" \"")
		b.WriteString(name)
		b.WriteString("\"")
	}
	if !pw && snapshotValueRoles[role] {
		if v := cleanText(n.Value.str(), 200); v != "" {
			b.WriteString(" value=\"")
			b.WriteString(v)
			b.WriteString("\"")
		}
	}
	if states := axStates(n.Properties); len(states) > 0 {
		b.WriteString(" [")
		b.WriteString(strings.Join(states, ", "))
		b.WriteString("]")
	}
	if ref != "" {
		b.WriteString(" ref=")
		b.WriteString(ref)
	}
	return b.String()
}
