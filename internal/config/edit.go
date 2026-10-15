package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/securefile"
)

// This file is `smind config get|set|unset` (ADR-0031 D10): a node-level YAML
// edit, so comments, key order and unrelated sections of a hand-written
// config.yaml survive. Only the keys in knownKeys can be edited; everything
// else is rejected rather than silently written (a typo in config.yaml is a
// setting that never applies).

type keyKind int

const (
	kindInt keyKind = iota
	kindString
	kindList
	kindSecret // passwordHash: never printed, only removable here
)

type keyInfo struct {
	kind keyKind
	help string
}

var knownKeys = map[string]keyInfo{
	"server.port":         {kindInt, "TCP port of the default listen address (4648)"},
	"server.listen":       {kindString, "host:port to bind; a non-loopback host needs a password (smind set-password)"},
	"server.hostnames":    {kindList, "extra Host header names, comma separated; a leading . matches subdomains"},
	"server.passwordHash": {kindSecret, "set with `smind set-password`; shown only as <set>"},
}

// KnownKeys lists the editable keys, sorted, for help and error messages.
func KnownKeys() []string {
	keys := make([]string, 0, len(knownKeys))
	for k := range knownKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func lookupKey(path string) (keyInfo, error) {
	ki, ok := knownKeys[path]
	if !ok {
		return keyInfo{}, fmt.Errorf("unknown config key %q (known keys: %s)", path, strings.Join(KnownKeys(), ", "))
	}
	return ki, nil
}

// Get returns the value of one known key as stored in config.yaml: a scalar,
// a comma-joined list, or "" when it is not set. passwordHash is reported as
// "<set>" / "" and never returned. Environment overrides are not applied; the
// caller may mention them.
func Get(path string) (string, error) {
	ki, err := lookupKey(path)
	if err != nil {
		return "", err
	}
	root, err := readDoc()
	if err != nil {
		return "", err
	}
	n := find(root, strings.Split(path, "."))
	if n == nil {
		return "", nil
	}
	switch {
	case ki.kind == kindSecret:
		return "<set>", nil
	case n.Kind == yaml.SequenceNode:
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			parts = append(parts, c.Value)
		}
		return strings.Join(parts, ","), nil
	}
	return n.Value, nil
}

// GetAll returns every known key with its stored value (see Get), in key order.
func GetAll() ([][2]string, error) {
	var out [][2]string
	for _, k := range KnownKeys() {
		v, err := Get(k)
		if err != nil {
			return nil, err
		}
		out = append(out, [2]string{k, v})
	}
	return out, nil
}

// Set writes one known key. The value is validated for the key's type, and the
// resulting file must still load. passwordHash cannot be set here: it is
// written only by SetPasswordHash, from a password that passed the strength
// rules.
func Set(path, value string) error {
	ki, err := lookupKey(path)
	if err != nil {
		return err
	}
	var node *yaml.Node
	switch ki.kind {
	case kindSecret:
		return fmt.Errorf("%s cannot be set directly; run `smind set-password`", path)
	case kindInt:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%s: %q is not a valid port (1-65535)", path, value)
		}
		node = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
	case kindString:
		v := strings.TrimSpace(value)
		if v == "" {
			return fmt.Errorf("%s: empty value (use `smind config unset %s` to remove it)", path, path)
		}
		node = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	case kindList:
		node = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, h := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			h = strings.ToLower(h)
			if err := ValidateHostname(h); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: h})
		}
		if len(node.Content) == 0 {
			return fmt.Errorf("%s: no hostnames given (use `smind config unset %s` to clear)", path, path)
		}
	}
	return mutate(func(root *yaml.Node) error {
		setNode(root, strings.Split(path, "."), node)
		return nil
	})
}

// Unset removes one known key (so its default applies again). Removing a key
// that is not set is not an error.
func Unset(path string) error {
	if _, err := lookupKey(path); err != nil {
		return err
	}
	return mutate(func(root *yaml.Node) error {
		removeNode(root, strings.Split(path, "."))
		return nil
	})
}

// SetPasswordHash stores a bcrypt hash as server.passwordHash.
func SetPasswordHash(hash string) error {
	if !strings.HasPrefix(hash, "$2") {
		return fmt.Errorf("refusing to store something that is not a bcrypt hash")
	}
	return mutate(func(root *yaml.Node) error {
		setNode(root, []string{"server", "passwordHash"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: hash})
		return nil
	})
}

// mutate reads config.yaml (an absent file is an empty document), applies fn,
// refuses the change when the result would not load, and rewrites the file
// atomically with owner-only permissions.
func mutate(fn func(root *yaml.Node) error) error {
	root, err := readDoc()
	if err != nil {
		return err
	}
	if err := fn(root); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return err
	}
	// The edited file must still be valid configuration (checked on the file
	// contents alone: env overrides must not mask or cause an error here).
	cfg := Default()
	if err := yaml.Unmarshal(buf.Bytes(), &cfg); err != nil {
		return fmt.Errorf("the edited config would not parse: %w", err)
	}
	if err := validateServer(cfg.Server); err != nil {
		return err
	}
	return writeAtomic(Path(), buf.Bytes())
}

func readDoc() (*yaml.Node, error) {
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", Path(), err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config %s: the top level must be a mapping", Path())
	}
	return doc.Content[0], nil
}

func mapGet(m *yaml.Node, key string) (k, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func find(root *yaml.Node, path []string) *yaml.Node {
	cur := root
	for _, p := range path {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return nil
		}
		_, cur = mapGet(cur, p)
	}
	return cur
}

// setNode sets path under root to val, creating mappings as needed. An
// existing value node is updated in place so its comments stay attached.
func setNode(root *yaml.Node, path []string, val *yaml.Node) {
	cur := root
	for i, p := range path {
		k, v := mapGet(cur, p)
		last := i == len(path)-1
		switch {
		case last && v != nil:
			val.HeadComment, val.LineComment, val.FootComment = v.HeadComment, v.LineComment, v.FootComment
			*v = *val
			return
		case last:
			cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p}, val)
			return
		case v != nil && v.Kind == yaml.MappingNode:
			cur = v
		default:
			_ = k
			child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if v != nil { // replace a non-mapping (e.g. `server: null`)
				*v = *child
				cur = v
			} else {
				cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p}, child)
				cur = child
			}
		}
	}
}

func removeNode(root *yaml.Node, path []string) {
	cur := root
	for i, p := range path {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return
		}
		if i == len(path)-1 {
			for j := 0; j+1 < len(cur.Content); j += 2 {
				if cur.Content[j].Value == p {
					cur.Content = append(cur.Content[:j], cur.Content[j+2:]...)
					return
				}
			}
			return
		}
		_, cur = mapGet(cur, p)
	}
}

// writeAtomic writes data next to path and renames it into place, so a crash
// never leaves a truncated config.yaml. The file is owner-only (it may hold the
// password hash).
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := securefile.WriteFile(tmp, data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
