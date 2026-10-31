package uiview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Validate parses a view document, enforces the ui:1 schema and the caps,
// sanitises what needs sanitising (markdown text, inline svg icons, openUrl
// targets) and returns the normalised document: compact JSON with a
// deterministic key order, produced by marshalling fresh maps. Anything the
// schema does not name is an *Error carrying the JSON path of the offending
// value; nothing is ever truncated or silently dropped.
func Validate(doc []byte, env Env) ([]byte, error) {
	if len(doc) > MaxDocBytes {
		return nil, &Error{"$", fmt.Sprintf("document is %d bytes, over the %d-byte cap", len(doc), MaxDocBytes)}
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, &Error{"$", "invalid JSON: " + err.Error()}
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, &Error{"$", "trailing data after the document"}
	}
	top, ok := raw.(map[string]any)
	if !ok {
		return nil, &Error{"$", "document must be a JSON object"}
	}
	out := map[string]any{}
	for k, val := range top {
		switch k {
		case "ui":
			n, ok := val.(json.Number)
			if !ok || n.String() != "1" {
				return nil, &Error{"$.ui", "must be 1"}
			}
			out["ui"] = json.Number("1")
		case "title":
			s, ok := val.(string)
			if !ok {
				return nil, &Error{"$.title", "must be a string"}
			}
			if e := checkString(s, "$.title"); e != nil {
				return nil, e
			}
			if utf8.RuneCountInString(s) > 120 {
				return nil, &Error{"$.title", "exceeds 120 characters"}
			}
			out["title"] = s
		case "root":
			root, err := (&validator{env: env}).node(val, "$.root", 1)
			if err != nil {
				return nil, err
			}
			out["root"] = root
		default:
			return nil, &Error{"$." + k, "unknown field"}
		}
	}
	if _, ok := out["ui"]; !ok {
		return nil, &Error{"$.ui", "is required and must be 1"}
	}
	if _, ok := out["root"]; !ok {
		return nil, &Error{"$.root", "is required"}
	}
	normalised, err := json.Marshal(out)
	if err != nil {
		return nil, &Error{"$", "cannot encode the normalised document: " + err.Error()}
	}
	if len(normalised) > MaxDocBytes {
		return nil, &Error{"$", fmt.Sprintf("normalised document is %d bytes, over the %d-byte cap", len(normalised), MaxDocBytes)}
	}
	return normalised, nil
}

type validator struct {
	env   Env
	nodes int
}

// node validates one node at path and returns its normalised form. Depth
// counts the node itself, so the root sits at depth 1.
func (v *validator) node(val any, path string, depth int) (map[string]any, error) {
	if depth > MaxDepth {
		return nil, &Error{path, fmt.Sprintf("depth %d exceeds the maximum of %d", depth, MaxDepth)}
	}
	v.nodes++
	if v.nodes > MaxNodes {
		return nil, &Error{path, fmt.Sprintf("document has more than %d nodes", MaxNodes)}
	}
	m, ok := val.(map[string]any)
	if !ok {
		return nil, &Error{path, "node must be an object"}
	}
	typ, ok := m["type"].(string)
	if !ok {
		return nil, &Error{path + ".type", "must be a node type"}
	}
	if !nodeTypeSet[typ] {
		// The clients render a placeholder for an unknown type; the daemon
		// refuses the document outright instead.
		return nil, &Error{path + ".type", "unknown node type " + strconv.Quote(typ)}
	}
	out := map[string]any{"type": typ}
	if raw, ok := m["id"]; ok {
		s, ok := raw.(string)
		if !ok || !nodeIDRe.MatchString(s) {
			return nil, &Error{path + ".id", "must match [A-Za-z0-9._-] and be at most 64 characters"}
		}
		out["id"] = s
	}
	fields, err := v.nodeFields(m, typ, path, depth)
	if err != nil {
		return nil, err
	}
	for k, val := range fields {
		out[k] = val
	}
	return out, nil
}

func (v *validator) nodeFields(m map[string]any, typ, path string, depth int) (map[string]any, error) {
	switch typ {
	case "stack", "row":
		if e := checkFields(m, path, "type", "id", "children", "gap", "wrap"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "children"); e != nil {
			return nil, e
		}
		out := map[string]any{}
		children, err := v.children(m["children"], path, depth)
		if err != nil {
			return nil, err
		}
		out["children"] = children
		if s, present, err := getString(m, "gap", path+".gap"); err != nil {
			return nil, err
		} else if present {
			if !gapSet[s] {
				return nil, &Error{path + ".gap", "must be one of sm, md, lg"}
			}
			out["gap"] = s
		}
		if typ == "row" {
			if b, present, err := getBool(m, "wrap", path+".wrap"); err != nil {
				return nil, err
			} else if present {
				out["wrap"] = b
			}
		}
		return out, nil

	case "heading":
		if e := checkFields(m, path, "type", "id", "text", "level"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		out := map[string]any{}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		out["text"] = s
		if n, present, err := getInt(m, "level", path+".level"); err != nil {
			return nil, err
		} else if present {
			if n < 1 || n > 3 {
				return nil, &Error{path + ".level", "must be 1, 2 or 3"}
			}
			out["level"] = json.Number(strconv.Itoa(n))
		}
		return out, nil

	case "text":
		return v.textLike(m, path, "text", "tone", "size")

	case "markdown":
		if e := checkFields(m, path, "type", "id", "text"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		return map[string]any{"text": SanitizeMarkdown(s)}, nil

	case "keyValue":
		if e := checkFields(m, path, "type", "id", "items"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "items"); e != nil {
			return nil, e
		}
		items, err := arrayField(m, "items", path, MaxRows)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(items))
		for i, raw := range items {
			p := fmt.Sprintf("%s.items[%d]", path, i)
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, &Error{p, "must be an object"}
			}
			if e := checkFields(item, p, "key", "value", "tone"); e != nil {
				return nil, e
			}
			if e := requireFields(item, p, "key", "value"); e != nil {
				return nil, e
			}
			norm := map[string]any{}
			for _, f := range []string{"key", "value"} {
				s, _, err := getString(item, f, p+"."+f)
				if err != nil {
					return nil, err
				}
				norm[f] = s
			}
			if raw, ok := item["tone"]; ok {
				if e := validateTone(raw, p+".tone"); e != nil {
					return nil, e
				}
				norm["tone"] = raw
			}
			out = append(out, norm)
		}
		return map[string]any{"items": out}, nil

	case "list":
		if e := checkFields(m, path, "type", "id", "items"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "items"); e != nil {
			return nil, e
		}
		items, err := arrayField(m, "items", path, MaxRows)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(items))
		for i, raw := range items {
			p := fmt.Sprintf("%s.items[%d]", path, i)
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, &Error{p, "must be an object"}
			}
			if e := checkFields(item, p, "title", "subtitle", "icon", "tone", "badge", "action"); e != nil {
				return nil, e
			}
			if e := requireFields(item, p, "title"); e != nil {
				return nil, e
			}
			norm := map[string]any{}
			title, _, err := getString(item, "title", p+".title")
			if err != nil {
				return nil, err
			}
			norm["title"] = title
			if s, present, err := getString(item, "subtitle", p+".subtitle"); err != nil {
				return nil, err
			} else if present {
				norm["subtitle"] = s
			}
			if raw, ok := item["badge"]; ok {
				s, ok := raw.(string)
				if !ok {
					return nil, &Error{p + ".badge", "must be a string"}
				}
				if e := checkString(s, p+".badge"); e != nil {
					return nil, e
				}
				norm["badge"] = s
			}
			if raw, ok := item["tone"]; ok {
				if e := validateTone(raw, p+".tone"); e != nil {
					return nil, e
				}
				norm["tone"] = raw
			}
			if raw, ok := item["icon"]; ok {
				if e := validateIcon(raw, p+".icon"); e != nil {
					return nil, e
				}
				norm["icon"] = raw
			}
			if raw, ok := item["action"]; ok {
				action, err := v.action(raw, p+".action")
				if err != nil {
					return nil, err
				}
				norm["action"] = action
			}
			out = append(out, norm)
		}
		return map[string]any{"items": out}, nil

	case "table":
		return v.table(m, path)

	case "badge":
		if e := checkFields(m, path, "type", "id", "text", "tone", "icon"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		out := map[string]any{}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		out["text"] = s
		if raw, ok := m["tone"]; ok {
			if e := validateTone(raw, path+".tone"); e != nil {
				return nil, e
			}
			out["tone"] = raw
		}
		if raw, ok := m["icon"]; ok {
			if e := validateIcon(raw, path+".icon"); e != nil {
				return nil, e
			}
			out["icon"] = raw
		}
		return out, nil

	case "progress":
		if e := checkFields(m, path, "type", "id", "value", "label", "tone"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "value"); e != nil {
			return nil, e
		}
		n, ok := m["value"].(json.Number)
		if !ok {
			return nil, &Error{path + ".value", "must be a number"}
		}
		f, err := n.Float64()
		if err != nil || f < 0 || f > 1 {
			return nil, &Error{path + ".value", "must be between 0 and 1"}
		}
		out := map[string]any{"value": n}
		if s, present, err := getString(m, "label", path+".label"); err != nil {
			return nil, err
		} else if present {
			out["label"] = s
		}
		if raw, ok := m["tone"]; ok {
			if e := validateTone(raw, path+".tone"); e != nil {
				return nil, e
			}
			out["tone"] = raw
		}
		return out, nil

	case "code":
		if e := checkFields(m, path, "type", "id", "text", "language"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		out := map[string]any{}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		out["text"] = s
		if s, present, err := getString(m, "language", path+".language"); err != nil {
			return nil, err
		} else if present {
			if !languageRe.MatchString(s) {
				return nil, &Error{path + ".language", "must match [A-Za-z0-9+#._-] and be at most 32 characters"}
			}
			out["language"] = s
		}
		return out, nil

	case "diff":
		if e := checkFields(m, path, "type", "id", "text"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		return map[string]any{"text": s}, nil

	case "form":
		return v.form(m, path)

	case "button":
		if e := checkFields(m, path, "type", "id", "label", "action", "tone", "icon", "disabled"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "label", "action"); e != nil {
			return nil, e
		}
		out := map[string]any{}
		s, _, err := getString(m, "label", path+".label")
		if err != nil {
			return nil, err
		}
		out["label"] = s
		action, err := v.action(m["action"], path+".action")
		if err != nil {
			return nil, err
		}
		out["action"] = action
		if raw, ok := m["tone"]; ok {
			if e := validateTone(raw, path+".tone"); e != nil {
				return nil, e
			}
			out["tone"] = raw
		}
		if raw, ok := m["icon"]; ok {
			if e := validateIcon(raw, path+".icon"); e != nil {
				return nil, e
			}
			out["icon"] = raw
		}
		if b, present, err := getBool(m, "disabled", path+".disabled"); err != nil {
			return nil, err
		} else if present {
			out["disabled"] = b
		}
		return out, nil

	case "empty":
		// error is the same shape minus the icon.
		if e := checkFields(m, path, "type", "id", "title", "text", "icon"); e != nil {
			return nil, e
		}
		out, err := v.titleText(m, path)
		if err != nil {
			return nil, err
		}
		if raw, ok := m["icon"]; ok {
			if e := validateIcon(raw, path+".icon"); e != nil {
				return nil, e
			}
			out["icon"] = raw
		}
		return out, nil

	case "error":
		if e := checkFields(m, path, "type", "id", "title", "text"); e != nil {
			return nil, e
		}
		return v.titleText(m, path)
	}
	return nil, &Error{path + ".type", "unknown node type " + strconv.Quote(typ)}
}

// titleText validates the shared required title / optional text pair.
func (v *validator) titleText(m map[string]any, path string) (map[string]any, error) {
	if e := requireFields(m, path, "title"); e != nil {
		return nil, e
	}
	out := map[string]any{}
	s, _, err := getString(m, "title", path+".title")
	if err != nil {
		return nil, err
	}
	out["title"] = s
	if s, present, err := getString(m, "text", path+".text"); err != nil {
		return nil, err
	} else if present {
		out["text"] = s
	}
	return out, nil
}

// textLike validates the shared shape of text nodes: text plus optional
// enumerated extras.
func (v *validator) textLike(m map[string]any, path string, extras ...string) (map[string]any, error) {
	allowed := append([]string{"type", "id", "text"}, extras...)
	if e := checkFields(m, path, allowed...); e != nil {
		return nil, e
	}
	if e := requireFields(m, path, "text"); e != nil {
		return nil, e
	}
	out := map[string]any{}
	s, _, err := getString(m, "text", path+".text")
	if err != nil {
		return nil, err
	}
	out["text"] = s
	if raw, ok := m["tone"]; ok {
		if e := validateTone(raw, path+".tone"); e != nil {
			return nil, e
		}
		out["tone"] = raw
	}
	if raw, ok := m["size"]; ok {
		str, ok := raw.(string)
		if !ok || !sizeSet[str] {
			return nil, &Error{path + ".size", "must be one of sm, md"}
		}
		out["size"] = str
	}
	return out, nil
}

func (v *validator) children(raw any, path string, depth int) ([]any, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, &Error{path + ".children", "must be an array of nodes"}
	}
	out := make([]any, 0, len(arr))
	for i, child := range arr {
		norm, err := v.node(child, fmt.Sprintf("%s.children[%d]", path, i), depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, norm)
	}
	return out, nil
}

func (v *validator) table(m map[string]any, path string) (map[string]any, error) {
	if e := checkFields(m, path, "type", "id", "columns", "rows"); e != nil {
		return nil, e
	}
	if e := requireFields(m, path, "columns", "rows"); e != nil {
		return nil, e
	}
	columns, err := arrayField(m, "columns", path, MaxColumns)
	if err != nil {
		return nil, err
	}
	if len(columns) < 1 {
		return nil, &Error{path + ".columns", "must have at least one column"}
	}
	keys := map[string]bool{}
	normCols := make([]map[string]any, 0, len(columns))
	for i, raw := range columns {
		p := fmt.Sprintf("%s.columns[%d]", path, i)
		col, ok := raw.(map[string]any)
		if !ok {
			return nil, &Error{p, "must be an object"}
		}
		if e := checkFields(col, p, "key", "label", "align"); e != nil {
			return nil, e
		}
		if e := requireFields(col, p, "key", "label"); e != nil {
			return nil, e
		}
		norm := map[string]any{}
		for _, f := range []string{"key", "label"} {
			s, _, err := getString(col, f, p+"."+f)
			if err != nil {
				return nil, err
			}
			norm[f] = s
		}
		if keys[norm["key"].(string)] {
			return nil, &Error{p + ".key", "duplicate column key"}
		}
		keys[norm["key"].(string)] = true
		if raw, ok := col["align"]; ok {
			s, ok := raw.(string)
			if !ok || !alignSet[s] {
				return nil, &Error{p + ".align", "must be one of start, end"}
			}
			norm["align"] = s
		}
		normCols = append(normCols, norm)
	}
	rows, err := arrayField(m, "rows", path, MaxRows)
	if err != nil {
		return nil, err
	}
	normRows := make([]map[string]any, 0, len(rows))
	for i, raw := range rows {
		p := fmt.Sprintf("%s.rows[%d]", path, i)
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, &Error{p, "must be an object"}
		}
		norm := map[string]any{}
		for k, cell := range row {
			if !keys[k] {
				return nil, &Error{p + "." + k, "is not a declared column"}
			}
			switch t := cell.(type) {
			case string:
				if e := checkString(t, p+"."+k); e != nil {
					return nil, e
				}
				norm[k] = t
			case json.Number, bool, nil:
				norm[k] = cell
			default:
				return nil, &Error{p + "." + k, "must be a string, number, boolean or null"}
			}
		}
		normRows = append(normRows, norm)
	}
	return map[string]any{"columns": normCols, "rows": normRows}, nil
}

func (v *validator) form(m map[string]any, path string) (map[string]any, error) {
	if e := checkFields(m, path, "type", "id", "fields", "submit"); e != nil {
		return nil, e
	}
	if e := requireFields(m, path, "id", "fields", "submit"); e != nil {
		return nil, e
	}
	id, ok := m["id"].(string)
	if !ok || !nodeIDRe.MatchString(id) {
		return nil, &Error{path + ".id", "must match [A-Za-z0-9._-] and be 1 to 64 characters"}
	}
	fields, err := arrayField(m, "fields", path, MaxFormFields)
	if err != nil {
		return nil, err
	}
	if len(fields) < 1 {
		return nil, &Error{path + ".fields", "must have at least one field"}
	}
	names := map[string]bool{}
	normFields := make([]map[string]any, 0, len(fields))
	for i, raw := range fields {
		p := fmt.Sprintf("%s.fields[%d]", path, i)
		f, ok := raw.(map[string]any)
		if !ok {
			return nil, &Error{p, "must be an object"}
		}
		if e := checkFields(f, p, "name", "type", "label", "placeholder", "required", "options", "default"); e != nil {
			return nil, e
		}
		if e := requireFields(f, p, "name", "type", "label"); e != nil {
			return nil, e
		}
		name, ok := f["name"].(string)
		if !ok || !fieldNameRe.MatchString(name) {
			return nil, &Error{p + ".name", "must match [A-Za-z][A-Za-z0-9_]{0,39}"}
		}
		if names[name] {
			return nil, &Error{p + ".name", "duplicate field name"}
		}
		names[name] = true
		typ, ok := f["type"].(string)
		if !ok || !fieldTypeSet[typ] {
			return nil, &Error{p + ".type", "must be one of text, select, toggle, secret"}
		}
		norm := map[string]any{"name": name, "type": typ}
		label, _, err := getString(f, "label", p+".label")
		if err != nil {
			return nil, err
		}
		norm["label"] = label
		if s, present, err := getString(f, "placeholder", p+".placeholder"); err != nil {
			return nil, err
		} else if present {
			norm["placeholder"] = s
		}
		if b, present, err := getBool(f, "required", p+".required"); err != nil {
			return nil, err
		} else if present {
			norm["required"] = b
		}
		if raw, ok := f["options"]; ok {
			if typ != "select" {
				return nil, &Error{p + ".options", "only a select field may have options"}
			}
			opts, ok := raw.([]any)
			if !ok {
				return nil, &Error{p + ".options", "must be an array"}
			}
			if len(opts) < 1 {
				return nil, &Error{p + ".options", "a select must have at least one option"}
			}
			if len(opts) > 64 {
				return nil, &Error{p + ".options", "must have at most 64 options"}
			}
			normOpts := make([]map[string]any, 0, len(opts))
			for j, oraw := range opts {
				op := fmt.Sprintf("%s.options[%d]", p, j)
				o, ok := oraw.(map[string]any)
				if !ok {
					return nil, &Error{op, "must be an object"}
				}
				if e := checkFields(o, op, "value", "label"); e != nil {
					return nil, e
				}
				if e := requireFields(o, op, "value", "label"); e != nil {
					return nil, e
				}
				no := map[string]any{}
				for _, k := range []string{"value", "label"} {
					s, _, err := getString(o, k, op+"."+k)
					if err != nil {
						return nil, err
					}
					no[k] = s
				}
				normOpts = append(normOpts, no)
			}
			norm["options"] = normOpts
		} else if typ == "select" {
			return nil, &Error{p + ".options", "a select must declare its options"}
		}
		if raw, ok := f["default"]; ok {
			switch typ {
			case "secret":
				return nil, &Error{p + ".default", "a secret field may not have a default"}
			case "toggle":
				b, ok := raw.(bool)
				if !ok {
					return nil, &Error{p + ".default", "must be a boolean"}
				}
				norm["default"] = b
			default:
				s, ok := raw.(string)
				if !ok {
					return nil, &Error{p + ".default", "must be a string"}
				}
				if e := checkString(s, p+".default"); e != nil {
					return nil, e
				}
				norm["default"] = s
			}
		}
		normFields = append(normFields, norm)
	}
	submit, ok := m["submit"].(map[string]any)
	if !ok {
		return nil, &Error{path + ".submit", "must be an object"}
	}
	if e := checkFields(submit, path+".submit", "label", "action"); e != nil {
		return nil, e
	}
	if e := requireFields(submit, path+".submit", "label", "action"); e != nil {
		return nil, e
	}
	label, _, err := getString(submit, "label", path+".submit.label")
	if err != nil {
		return nil, err
	}
	action, err := v.action(submit["action"], path+".submit.action")
	if err != nil {
		return nil, err
	}
	if action["kind"] != "rpc" {
		return nil, &Error{path + ".submit.action.kind", "must be \"rpc\""}
	}
	return map[string]any{
		"id":     id,
		"fields": normFields,
		"submit": map[string]any{"label": label, "action": action},
	}, nil
}

// action validates one action object at path and returns its normalised
// form. openUrl gains the host the client shows in its confirm dialog; a
// plugin-supplied "host" is rejected as an unknown field before that.
func (v *validator) action(raw any, path string) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, &Error{path, "must be an object"}
	}
	kind, ok := m["kind"].(string)
	if !ok {
		return nil, &Error{path + ".kind", "must be an action kind"}
	}
	if !actionKindSet[kind] {
		return nil, &Error{path + ".kind", "unknown action kind " + strconv.Quote(kind)}
	}
	switch kind {
	case "rpc":
		if e := checkFields(m, path, "kind", "rpc", "params"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "rpc"); e != nil {
			return nil, e
		}
		name, _, err := getString(m, "rpc", path+".rpc")
		if err != nil {
			return nil, err
		}
		if !v.env.RPCs[name] {
			return nil, &Error{path + ".rpc", "not declared in contributes.rpc"}
		}
		out := map[string]any{"kind": "rpc", "rpc": name}
		if raw, ok := m["params"]; ok {
			if e := v.params(raw, path+".params"); e != nil {
				return nil, e
			}
			out["params"] = raw
		}
		return out, nil

	case "prompt":
		if e := checkFields(m, path, "kind", "text"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "text"); e != nil {
			return nil, e
		}
		s, _, err := getString(m, "text", path+".text")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, &Error{path + ".text", "must not be empty"}
		}
		return map[string]any{"kind": "prompt", "text": s}, nil

	case "command":
		if e := checkFields(m, path, "kind", "name"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "name"); e != nil {
			return nil, e
		}
		s, _, err := getString(m, "name", path+".name")
		if err != nil {
			return nil, err
		}
		if !commandName.MatchString(s) {
			return nil, &Error{path + ".name", "is not a valid command name"}
		}
		return map[string]any{"kind": "command", "name": s}, nil

	case "openPanel":
		if e := checkFields(m, path, "kind", "panel", "context"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "panel"); e != nil {
			return nil, e
		}
		panel, _, err := getString(m, "panel", path+".panel")
		if err != nil {
			return nil, err
		}
		if !v.env.Panels[panel] {
			return nil, &Error{path + ".panel", "not declared in contributes.panels"}
		}
		out := map[string]any{"kind": "openPanel", "panel": panel}
		if raw, ok := m["context"]; ok {
			if e := v.params(raw, path+".context"); e != nil {
				return nil, e
			}
			out["context"] = raw
		}
		return out, nil

	case "openUrl":
		// "host" is not in the allowed list on purpose: the daemon adds it,
		// a plugin cannot.
		if e := checkFields(m, path, "kind", "url"); e != nil {
			return nil, e
		}
		if e := requireFields(m, path, "url"); e != nil {
			return nil, e
		}
		raw, ok := m["url"].(string)
		if !ok {
			return nil, &Error{path + ".url", "must be a string"}
		}
		host, e := sanitizeOpenURL(raw)
		if e != nil {
			return nil, &Error{path + ".url", e.Msg}
		}
		return map[string]any{"kind": "openUrl", "url": raw, "host": host}, nil
	}
	return nil, &Error{path + ".kind", "unknown action kind " + strconv.Quote(kind)}
}

// params checks a params/context object: any JSON shape, but every string
// (keys included) follows the document string rules and the re-encoded
// object must fit MaxParamsBytes.
func (v *validator) params(raw any, path string) error {
	m, ok := raw.(map[string]any)
	if !ok {
		return &Error{path, "must be an object"}
	}
	if e := walkStrings(m, path); e != nil {
		return e
	}
	b, err := json.Marshal(m)
	if err != nil {
		return &Error{path, "cannot be encoded"}
	}
	if len(b) > MaxParamsBytes {
		return &Error{path, "exceeds the 16 KiB cap"}
	}
	return nil
}

func walkStrings(x any, path string) *Error {
	switch t := x.(type) {
	case string:
		return checkString(t, path)
	case map[string]any:
		for k, val := range t {
			if e := checkString(k, path+"."+k); e != nil {
				return e
			}
			if e := walkStrings(val, path+"."+k); e != nil {
				return e
			}
		}
	case []any:
		for i, el := range t {
			if e := walkStrings(el, fmt.Sprintf("%s[%d]", path, i)); e != nil {
				return e
			}
		}
	}
	return nil
}

// checkFields rejects any key not named in allowed, naming its path, so a
// misspelled or smuggled field can never pass silently.
func checkFields(m map[string]any, path string, allowed ...string) error {
	for k := range m {
		seen := false
		for _, a := range allowed {
			if k == a {
				seen = true
				break
			}
		}
		if !seen {
			return &Error{path + "." + k, "unknown field"}
		}
	}
	return nil
}

func requireFields(m map[string]any, path string, required ...string) error {
	for _, r := range required {
		if _, ok := m[r]; !ok {
			return &Error{path + "." + r, "is required"}
		}
	}
	return nil
}

func getString(m map[string]any, key, path string) (string, bool, error) {
	raw, ok := m[key]
	if !ok {
		return "", false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", true, &Error{path, "must be a string"}
	}
	if e := checkString(s, path); e != nil {
		return "", true, e
	}
	return s, true, nil
}

func getBool(m map[string]any, key, path string) (bool, bool, error) {
	raw, ok := m[key]
	if !ok {
		return false, false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, true, &Error{path, "must be a boolean"}
	}
	return b, true, nil
}

func getInt(m map[string]any, key, path string) (int, bool, error) {
	raw, ok := m[key]
	if !ok {
		return 0, false, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, true, &Error{path, "must be a number"}
	}
	i, err := strconv.Atoi(n.String())
	if err != nil {
		return 0, true, &Error{path, "must be a whole number"}
	}
	return i, true, nil
}

// arrayField returns m[key] as an array, enforcing the given cap so a huge
// list is refused whole instead of trimmed.
func arrayField(m map[string]any, key, path string, max int) ([]any, error) {
	raw, ok := m[key]
	if !ok {
		return nil, &Error{path + "." + key, "is required"}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, &Error{path + "." + key, "must be an array"}
	}
	if len(arr) > max {
		return nil, &Error{fmt.Sprintf("%s.%s[%d]", path, key, max), fmt.Sprintf("exceeds the cap of %d", max)}
	}
	return arr, nil
}
