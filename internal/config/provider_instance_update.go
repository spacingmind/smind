package config

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// InstanceUpdate is one provider.updateInstance request (ADR-0046
// amendment): a nil field is left alone, a non-nil empty list (or false)
// removes the key from the entry, a non-nil value replaces it.
type InstanceUpdate struct {
	Models              *[]CustomModel
	AdditionalModels    *[]CustomModel
	DisallowedTools     *[]string
	InheritClaudeConfig *bool
}

// UpdateProviderInstance patches the providers.custom entry id in
// config.yaml (models, additionalModels, disallowedTools,
// inheritClaudeConfig) and returns the entry as it is after the update and
// whether the file changed. Unlike mutate (which re-encodes the whole file
// and drops blank lines), it splices the bytes of the changed key blocks
// only: every other byte — other entries, comments, ordering — is identical.
// The resulting entry is validated with the config-load rules, and the
// patched bytes are decoded again and checked before anything is written.
func UpdateProviderInstance(id string, u InstanceUpdate) (CustomProvider, bool, error) {
	if u.Models == nil && u.AdditionalModels == nil && u.DisallowedTools == nil && u.InheritClaudeConfig == nil {
		return CustomProvider{}, false, fmt.Errorf("provider %q: nothing to update", id)
	}
	raw, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return CustomProvider{}, false, fmt.Errorf("provider %q not found in providers.custom", id)
		}
		return CustomProvider{}, false, fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return CustomProvider{}, false, fmt.Errorf("parse config %s: %w", Path(), err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return CustomProvider{}, false, fmt.Errorf("provider %q not found in providers.custom", id)
	}
	seq := find(doc.Content[0], []string{"providers", "custom"})
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return CustomProvider{}, false, fmt.Errorf("provider %q not found in providers.custom", id)
	}
	var entry *yaml.Node
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if k, _ := mapGet(item, "id"); k != nil && k.Value == id {
			entry = item
			break
		}
	}
	if entry == nil {
		return CustomProvider{}, false, fmt.Errorf("provider %q not found in providers.custom", id)
	}
	var cur CustomProvider
	if err := entry.Decode(&cur); err != nil {
		return CustomProvider{}, false, fmt.Errorf("provider %q: %w", id, err)
	}
	if cur.Extends == "" {
		return CustomProvider{}, false, fmt.Errorf("provider %q is not an instance (only an entry with extends can be updated)", id)
	}
	next := cur
	applyInstanceUpdate(&next, u)
	var all []CustomProvider
	if err := seq.Decode(&all); err != nil {
		return CustomProvider{}, false, fmt.Errorf("providers.custom: %w", err)
	}
	idx := -1
	for i := range all {
		if all[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return CustomProvider{}, false, fmt.Errorf("provider %q not found in providers.custom", id)
	}
	all[idx] = next
	if err := ValidateCustomProviders(all); err != nil {
		return CustomProvider{}, false, err
	}
	if instanceFieldsEqual(cur, next) {
		return next, false, nil
	}
	patched, err := patchInstanceEntry(raw, &doc, entry, cur, next, id)
	if err != nil {
		return CustomProvider{}, false, err
	}
	if err := verifyInstancePatch(seq, patched, idx, cur, next); err != nil {
		return CustomProvider{}, false, fmt.Errorf("provider %q: internal edit check failed: %w", id, err)
	}
	if err := writeAtomic(Path(), patched); err != nil {
		return CustomProvider{}, false, err
	}
	return next, true, nil
}

// verifyInstancePatch is the safety net before the atomic write: the patched
// bytes must parse, the entry must decode to exactly next (with every
// non-editable field still cur's), every other entry must be unchanged, and
// the whole list must still validate. Nothing is written otherwise.
func verifyInstancePatch(origSeq *yaml.Node, patched []byte, idx int, cur, next CustomProvider) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(patched, &doc); err != nil {
		return fmt.Errorf("the patched file does not parse: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("the top level is no longer a mapping")
	}
	seq := find(doc.Content[0], []string{"providers", "custom"})
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return fmt.Errorf("providers.custom is gone")
	}
	var after []CustomProvider
	if err := seq.Decode(&after); err != nil {
		return fmt.Errorf("providers.custom: %w", err)
	}
	var before []CustomProvider
	if err := origSeq.Decode(&before); err != nil {
		return fmt.Errorf("providers.custom: %w", err)
	}
	if len(after) != len(before) {
		return fmt.Errorf("providers.custom has %d entries, want %d", len(after), len(before))
	}
	if idx >= len(after) {
		return fmt.Errorf("entry %d is gone", idx)
	}
	got := after[idx]
	if got.ID != cur.ID {
		return fmt.Errorf("entry %d is %q, want %q", idx, got.ID, cur.ID)
	}
	if !modelsEqual(got.Models, next.Models) || !modelsEqual(got.AdditionalModels, next.AdditionalModels) ||
		!stringsEqual(got.DisallowedTools, next.DisallowedTools) || got.InheritClaudeConfig != next.InheritClaudeConfig {
		return fmt.Errorf("the edited fields did not land as expected")
	}
	if got.Extends != cur.Extends || got.Label != cur.Label || got.Command != cur.Command ||
		got.Recipe != cur.Recipe || got.Vendor != cur.Vendor || got.Thinking != cur.Thinking ||
		!reflect.DeepEqual(got.Env, cur.Env) || !reflect.DeepEqual(got.Endpoint, cur.Endpoint) {
		return fmt.Errorf("a field outside the editable set changed")
	}
	for i := range after {
		if i == idx {
			continue
		}
		if !reflect.DeepEqual(after[i], before[i]) {
			return fmt.Errorf("entry %d (%q) changed", i, before[i].ID)
		}
	}
	return ValidateCustomProviders(after)
}

// applyInstanceUpdate copies the update onto p: nil leaves the field, an
// empty slice (or false) clears it, a value replaces it.
func applyInstanceUpdate(p *CustomProvider, u InstanceUpdate) {
	if u.Models != nil {
		p.Models = append([]CustomModel(nil), *u.Models...)
	}
	if u.AdditionalModels != nil {
		p.AdditionalModels = append([]CustomModel(nil), *u.AdditionalModels...)
	}
	if u.DisallowedTools != nil {
		p.DisallowedTools = append([]string(nil), *u.DisallowedTools...)
	}
	if u.InheritClaudeConfig != nil {
		p.InheritClaudeConfig = *u.InheritClaudeConfig
	}
}

// instanceFieldsEqual compares the four editable fields, nil and empty
// counting as the same value (nothing stored vs stored empty).
func instanceFieldsEqual(a, b CustomProvider) bool {
	return modelsEqual(a.Models, b.Models) &&
		modelsEqual(a.AdditionalModels, b.AdditionalModels) &&
		stringsEqual(a.DisallowedTools, b.DisallowedTools) &&
		a.InheritClaudeConfig == b.InheritClaudeConfig
}

func modelsEqual(a, b []CustomModel) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func stringsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// lineEdit replaces the 1-based line range [start, end) with text. start ==
// end inserts before line start.
type lineEdit struct {
	start, end int
	text       string
}

func patchInstanceEntry(raw []byte, doc, entry *yaml.Node, cur, next CustomProvider, id string) ([]byte, error) {
	if entry.Style&yaml.FlowStyle != 0 {
		return nil, layoutError(id)
	}
	type fieldPatch struct {
		key   string
		value *yaml.Node
		empty bool // the new value is empty: the key is removed
	}
	var patches []fieldPatch
	patch := func(key string, value *yaml.Node, empty bool) {
		patches = append(patches, fieldPatch{key: key, value: value, empty: empty})
	}
	if !modelsEqual(cur.Models, next.Models) {
		v, err := modelsValueNode(next.Models)
		if err != nil {
			return nil, err
		}
		patch("models", v, len(next.Models) == 0)
	}
	if !modelsEqual(cur.AdditionalModels, next.AdditionalModels) {
		v, err := modelsValueNode(next.AdditionalModels)
		if err != nil {
			return nil, err
		}
		patch("additionalModels", v, len(next.AdditionalModels) == 0)
	}
	if !stringsEqual(cur.DisallowedTools, next.DisallowedTools) {
		patch("disallowedTools", stringsValueNode(next.DisallowedTools), len(next.DisallowedTools) == 0)
	}
	if cur.InheritClaudeConfig != next.InheritClaudeConfig {
		patch("inheritClaudeConfig", boolValueNode(), !next.InheritClaudeConfig)
	}
	if len(patches) == 0 {
		return nil, fmt.Errorf("provider %q: nothing to update", id)
	}

	lines := splitLines(raw)
	// The line range of one key: [k.Line, bound), with trailing blank and
	// comment lines given back to whatever follows, but never above the last
	// line of the value itself.
	keyRange := func(k, v *yaml.Node) (int, int, error) {
		if err := keyOnOwnLine(lines, k); err != nil {
			return 0, 0, err
		}
		bound := docBound(doc, v, k.Line)
		if bound == 0 {
			bound = len(lines) + 1
		}
		end := bound
		for end-1 >= 1 && end-1 > k.Line {
			if t := strings.TrimSpace(lines[end-2]); t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			end--
		}
		if floor := subtreeMaxLine(v) + 1; end < floor {
			end = floor
		}
		return k.Line, end, nil
	}

	var edits []lineEdit
	// Absent keys being added all land at the same place — the end of the
	// entry — and that point may also be the end of a replaced block, so
	// they are merged into one insert (or folded into that block) below.
	insertAt := 0
	var insertText strings.Builder
	for _, p := range patches {
		k, v := mapGet(entry, p.key)
		if k == nil {
			// Absent key being added: it goes at the end of the entry,
			// with the entry keys' own indentation. An absent key with an
			// empty value needs no edit at all.
			if p.empty {
				continue
			}
			lastKey, lastVal := entry.Content[len(entry.Content)-2], entry.Content[len(entry.Content)-1]
			_, end, err := keyRange(lastKey, lastVal)
			if err != nil {
				return nil, layoutError(id)
			}
			text, err := encodeKeyed(p.key, p.value, entry.Content[0].Column-1)
			if err != nil {
				return nil, err
			}
			insertAt = end
			insertText.WriteString(text)
			continue
		}
		if err := noBlockScalars(v); err != nil {
			return nil, layoutError(id)
		}
		start, end, err := keyRange(k, v)
		if err != nil {
			return nil, layoutError(id)
		}
		text := ""
		if !p.empty {
			text, err = encodeKeyed(p.key, p.value, k.Column-1)
			if err != nil {
				return nil, err
			}
		}
		edits = append(edits, lineEdit{start: start, end: end, text: text})
	}
	if insertText.Len() > 0 {
		// The insert lands at the entry's last line: the file must end that
		// line with a newline before anything can follow it.
		if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
			lines[n-1] += "\n"
		}
		folded := false
		for i, e := range edits {
			if e.start < e.end && e.end == insertAt {
				edits[i].text += insertText.String()
				folded = true
				break
			}
		}
		if !folded {
			edits = append(edits, lineEdit{start: insertAt, end: insertAt, text: insertText.String()})
		}
	}

	// Bottom to top, so earlier line numbers stay valid.
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for i := 1; i < len(edits); i++ {
		if edits[i-1].start < edits[i].end {
			return nil, fmt.Errorf("provider %q: internal edit check failed: overlapping edits", id)
		}
	}
	var out strings.Builder
	prev := 1
	for _, e := range edits {
		for i := prev; i < e.start && i <= len(lines); i++ {
			out.WriteString(lines[i-1])
		}
		out.WriteString(e.text)
		prev = e.end
	}
	for i := prev; i <= len(lines); i++ {
		out.WriteString(lines[i-1])
	}
	return []byte(out.String()), nil
}

func layoutError(id string) error {
	return fmt.Errorf("provider %q: config.yaml layout not supported for in-place edit (flow style or a key on the \"- \" line); edit config.yaml by hand", id)
}

// splitLines splits b into lines, each keeping its trailing "\n" (the last
// line may have none). "\r", if any, stays part of the line.
func splitLines(b []byte) []string {
	s := string(b)
	var lines []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
	return lines
}

// keyOnOwnLine checks that k is the first non-space character on its line:
// a key on the "- " line of the entry cannot be spliced without rewriting
// the dash too.
func keyOnOwnLine(lines []string, k *yaml.Node) error {
	if k.Line < 1 || k.Line > len(lines) || k.Column < 1 {
		return fmt.Errorf("config.yaml line %d: key position out of range", k.Line)
	}
	line := strings.TrimSuffix(strings.TrimSuffix(lines[k.Line-1], "\n"), "\r")
	if len(line) < k.Column-1 || strings.TrimLeft(line[:k.Column-1], " ") != "" {
		return errKeyNotOwnLine
	}
	return nil
}

var errKeyNotOwnLine = fmt.Errorf("key is not the first character on its line")

// docBound returns the smallest Line greater than after among all document
// nodes that are not inside v's subtree, or 0 when there is none.
func docBound(doc, v *yaml.Node, after int) int {
	bound := 0
	var walk func(n *yaml.Node, inside bool)
	walk = func(n *yaml.Node, inside bool) {
		if !inside && n.Line > after && (bound == 0 || n.Line < bound) {
			bound = n.Line
		}
		next := inside || n == v
		for _, c := range n.Content {
			walk(c, next)
		}
	}
	walk(doc, false)
	return bound
}

func subtreeMaxLine(v *yaml.Node) int {
	max := v.Line
	for _, c := range v.Content {
		if m := subtreeMaxLine(c); m > max {
			max = m
		}
	}
	return max
}

// noBlockScalars refuses a value we cannot splice around: a literal or
// folded block scalar has no reliable node line span.
func noBlockScalars(v *yaml.Node) error {
	if v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return errBlockScalar
	}
	for _, c := range v.Content {
		if err := noBlockScalars(c); err != nil {
			return err
		}
	}
	return nil
}

var errBlockScalar = fmt.Errorf("value is a block scalar")

// modelFields is the wire form of CustomModel for writing config.yaml: only
// the set fields, in a fixed order (CustomProvider's own tags are not
// touched).
type modelFields struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label,omitempty"`
	Description string `yaml:"description,omitempty"`
	Default     bool   `yaml:"default,omitempty"`
}

func modelsValueNode(list []CustomModel) (*yaml.Node, error) {
	fields := make([]modelFields, len(list))
	for i, m := range list {
		fields[i] = modelFields{ID: m.ID, Label: m.Label, Description: m.Description, Default: m.Default}
	}
	b, err := yaml.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func stringsValueNode(list []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, s := range list {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s})
	}
	return seq
}

func boolValueNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
}

// encodeKeyed writes `key: value` (block style, indent 2) and prefixes every
// line with indent spaces, so the result drops in at the key's column.
func encodeKeyed(key string, value *yaml.Node, indent int) (string, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value,
	}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	pad := strings.Repeat(" ", indent)
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if line != "" {
			out.WriteString(pad)
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String(), nil
}
