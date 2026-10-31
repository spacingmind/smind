// Package uiview is the single definition of the ui:1 plugin view-document
// schema (ADR-0042 section 5): the node vocabulary, the caps, the validation
// and the sanitisers the daemon applies before a view document reaches any
// client, plus the fixture suite (testdata/fixtures) the web and mobile
// renderers are tested against so the two native implementations cannot
// drift apart.
package uiview

import "fmt"

// Level is the view schema level this package defines. A client states the
// level it supports with every panels/view request; the daemon withholds
// anything above it.
const Level = 1

// Caps enforced by Validate. Going over any of them is an *Error naming the
// JSON path: nothing is ever truncated or silently dropped.
const (
	MaxDocBytes    = 256 << 10 // a whole view document
	MaxDepth       = 12        // nested nodes, the root being depth 1
	MaxNodes       = 1000      // nodes in one document
	MaxString      = 8 << 10   // any single string, in bytes
	MaxRows        = 500       // keyValue/list items and table rows
	MaxColumns     = 12        // table columns
	MaxFormFields  = 24        // form fields
	MaxSVGBytes    = 64 << 10  // one inline icon svg
	MaxParamsBytes = 16 << 10  // rpc params / openPanel context, re-encoded
)

// Env names what a view may reference: the RPC names the plugin declared in
// contributes.rpc and the panel ids it declared in contributes.panels. The
// daemon fills it from the plugin's manifest, so a view can never carry an
// action target the plugin never declared.
type Env struct {
	RPCs   map[string]bool
	Panels map[string]bool
}

// Error is a validation failure. Path is a JSON path like
// "$.root.children[2].text" pointing at the offending value.
type Error struct{ Path, Msg string }

func (e *Error) Error() string { return fmt.Sprintf("view_invalid at %s: %s", e.Path, e.Msg) }

// NodeTypes lists the v1 node types, sorted.
var NodeTypes = []string{
	"badge", "button", "code", "diff", "empty", "error", "form",
	"heading", "keyValue", "list", "markdown", "progress", "row",
	"stack", "table", "text",
}

// ActionKinds lists the action kinds, sorted.
var ActionKinds = []string{"command", "openPanel", "openUrl", "prompt", "rpc"}

// The closed vocabularies below are the lookup forms of the public lists and
// of the small enumerated fields. Anything outside them is an *Error, so a
// view can never smuggle a raw colour, an unknown node or an unlisted action
// kind past the daemon.
var (
	nodeTypeSet   = toSet(NodeTypes)
	actionKindSet = toSet(ActionKinds)
	toneSet       = toSet([]string{"success", "warning", "danger", "muted"})
	gapSet        = toSet([]string{"sm", "md", "lg"})
	sizeSet       = toSet([]string{"sm", "md"})
	alignSet      = toSet([]string{"start", "end"})
	fieldTypeSet  = toSet([]string{"text", "select", "toggle", "secret"})

	// svgElemSet is the allowlist of inline-svg element local names
	// (SanitizeSVG); script, style, foreignObject, image, a, animate and
	// friends are absent on purpose.
	svgElemSet = toSet([]string{
		"svg", "g", "path", "circle", "ellipse", "rect", "line", "polyline",
		"polygon", "defs", "linearGradient", "radialGradient", "stop",
		"clipPath", "title", "desc", "use", "symbol",
	})
)

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
