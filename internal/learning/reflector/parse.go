// Package reflector holds the pure parts of the learning reflector and
// curator jobs (ADR-0026 sections 3 and 7): the system prompts, the user
// prompt composers and the strict parser for the model's final message. It
// calls no model and touches no database; the deterministic gate remains the
// only code that can bless an intent.
package reflector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/learning/gate"
)

const (
	// MaxOutputBytes caps the model message the parser will look at.
	MaxOutputBytes = 64 * 1024
	// MaxIntents caps the intents accepted from one message.
	MaxIntents = 5

	maxEvidence   = 3
	maxQuoteRunes = 500
	// maxDepth is the deepest delimiter nesting the schema needs: object,
	// intents array, intent object, evidence array, evidence object.
	maxDepth = 5
)

// ErrBadOutput is wrapped by every ParseOutput failure.
var ErrBadOutput = errors.New("reflector: invalid output")

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadOutput, fmt.Sprintf(format, args...))
}

// ParseOutput accepts ONLY a single JSON object {"intents":[...]} as the
// entire message (surrounding ASCII whitespace aside) and returns its intents.
// Any violation returns an error wrapping ErrBadOutput and a nil slice: nothing
// partial is ever returned, and one bad intent fails the whole message. It
// does not run the gate and relaxes none of the gate's evidence rules.
//
// mode selects the per-job rules: gate.ModeCurate allows only patch and delete
// with no evidence; anything else is treated as reflect and allows only create
// and patch (delete is curator-only).
func ParseOutput(raw string, mode gate.Mode) ([]gate.Intent, error) {
	if len(raw) > MaxOutputBytes {
		return nil, bad("message is %d bytes, max %d", len(raw), MaxOutputBytes)
	}
	if !utf8.ValidString(raw) {
		return nil, bad("invalid UTF-8")
	}
	s := strings.TrimSpace(raw)
	if err := checkWrapper(raw, s); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, bad("message is not a single JSON object")
	}
	if err := walkSchema(s); err != nil {
		return nil, err
	}

	var msg struct {
		Intents *[]gate.Intent `json:"intents"`
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msg); err != nil {
		return nil, bad("decode: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, bad("trailing data after the JSON object")
	}
	if msg.Intents == nil {
		return nil, bad(`missing "intents" array`)
	}
	intents := *msg.Intents
	if len(intents) > MaxIntents {
		return nil, bad("%d intents, max %d", len(intents), MaxIntents)
	}
	for i, in := range intents {
		if err := checkIntent(in, mode); err != nil {
			return nil, bad("intent %d: %v", i, err)
		}
	}
	if intents == nil {
		intents = []gate.Intent{}
	}
	return intents, nil
}

// checkWrapper rejects any surrounding whitespace that is not ASCII space,
// tab, newline or CR (NBSP, U+3000, U+2028 and friends).
func checkWrapper(raw, trimmed string) error {
	lead := raw[:len(raw)-len(strings.TrimLeftFunc(raw, unicode.IsSpace))]
	trail := raw[len(strings.TrimRightFunc(raw, unicode.IsSpace)):]
	for _, r := range lead + trail {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			return bad("message is wrapped in non-ASCII whitespace U+%04X", r)
		}
	}
	return nil
}

func checkIntent(in gate.Intent, mode gate.Mode) error {
	switch in.Action {
	case gate.ActionCreate, gate.ActionPatch, gate.ActionDelete:
	default:
		return fmt.Errorf("unknown action %q", clip(string(in.Action)))
	}
	if mode == gate.ModeCurate {
		if in.Action == gate.ActionCreate {
			return errors.New("the curator may not create skills")
		}
		if len(in.Evidence) > 0 {
			return errors.New("curator intents must have empty evidence")
		}
		if in.Action == gate.ActionDelete && in.AbsorbedInto == "" {
			return errors.New("delete needs a non-empty absorbed_into")
		}
	} else if in.Action == gate.ActionDelete {
		return errors.New("the reflector may not delete skills")
	}
	if len(in.Evidence) > maxEvidence {
		return fmt.Errorf("%d evidence items, max %d", len(in.Evidence), maxEvidence)
	}
	for _, ev := range in.Evidence {
		if n := utf8.RuneCountInString(ev.Quote); n > maxQuoteRunes {
			return fmt.Errorf("evidence quote is %d runes, max %d", n, maxQuoteRunes)
		}
	}
	// encoding/json turns invalid escapes such as a lone surrogate into
	// U+FFFD; no legitimate field needs that character.
	texts := []string{string(in.Action), in.Name, in.Category, in.Description, in.Body, in.OldString, in.NewString, in.AbsorbedInto, in.Reason}
	for _, ev := range in.Evidence {
		texts = append(texts, ev.Kind, ev.RunID, ev.Quote)
	}
	for _, t := range texts {
		if strings.ContainsRune(t, utf8.RuneError) {
			return errors.New("string contains an invalid character")
		}
	}
	return nil
}

func clip(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "..."
	}
	return s
}

type nodeKind int

const (
	kindOther nodeKind = iota
	kindTop
	kindIntents
	kindIntent
	kindEvidenceList
	kindEvidence
)

var allowedKeys = map[nodeKind]map[string]nodeKind{
	kindTop: {"intents": kindIntents},
	kindIntent: {
		"action": kindOther, "name": kindOther, "category": kindOther, "description": kindOther,
		"body": kindOther, "old_string": kindOther, "new_string": kindOther, "absorbed_into": kindOther,
		"reason": kindOther, "evidence": kindEvidenceList,
	},
	kindEvidence: {"kind": kindOther, "run_id": kindOther, "seq": kindOther, "quote": kindOther},
}

// walkSchema walks the JSON text token by token and fails on a repeated key,
// on a key that is not EXACTLY an allowed name for its depth (encoding/json
// folds case, so it cannot be trusted to do this), or on nesting deeper than
// maxDepth.
func walkSchema(s string) error {
	dec := json.NewDecoder(strings.NewReader(s))
	if err := walkValue(dec, 1, kindTop); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad("trailing data after the JSON object")
	}
	return nil
}

func walkValue(dec *json.Decoder, depth int, kind nodeKind) error {
	tok, err := dec.Token()
	if err != nil {
		return bad("decode: %v", err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if depth > maxDepth {
		return bad("nesting deeper than %d", maxDepth)
	}
	switch d {
	case '{':
		keys := allowedKeys[kind]
		seen := map[string]struct{}{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return bad("decode: %v", err)
			}
			key, _ := kt.(string)
			child, allowed := keys[key]
			if !allowed {
				return bad("unknown key %q", clip(key))
			}
			if _, dup := seen[key]; dup {
				return bad("duplicate key %q", clip(key))
			}
			seen[key] = struct{}{}
			if err := walkValue(dec, depth+1, child); err != nil {
				return err
			}
		}
	case '[':
		elem := kindOther
		switch kind {
		case kindIntents:
			elem = kindIntent
		case kindEvidenceList:
			elem = kindEvidence
		}
		for dec.More() {
			if err := walkValue(dec, depth+1, elem); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // closing delimiter
		return bad("decode: %v", err)
	}
	return nil
}

// ScopeCheck verifies the intents only touch skills the model was shown
// (shown = names of the learned skills in its prompt) and that a reply is
// internally coherent: a skill is the name of at most one intent, every delete
// is paired with a patch of its absorbed_into umbrella, and in curate mode no
// skill is both absorbed into and deleted. Errors wrap ErrBadOutput.
func ScopeCheck(intents []gate.Intent, mode gate.Mode, shown []string) error {
	known := append([]string(nil), shown...)
	sort.Strings(known)
	isShown := func(n string) bool {
		i := sort.SearchStrings(known, n)
		return i < len(known) && known[i] == n
	}
	targets := map[string]struct{}{}
	patched := map[string]struct{}{}
	deleted := map[string]struct{}{}
	for i, in := range intents {
		if _, dup := targets[in.Name]; dup {
			return bad("intent %d: %q is targeted by more than one intent", i, clip(in.Name))
		}
		targets[in.Name] = struct{}{}
		switch in.Action {
		case gate.ActionPatch:
			if !isShown(in.Name) {
				return bad("intent %d: patch of %q, which was not shown", i, clip(in.Name))
			}
			patched[in.Name] = struct{}{}
		case gate.ActionDelete:
			if !isShown(in.Name) {
				return bad("intent %d: delete of %q, which was not shown", i, clip(in.Name))
			}
			if !isShown(in.AbsorbedInto) {
				return bad("intent %d: absorbed_into %q was not shown", i, clip(in.AbsorbedInto))
			}
			deleted[in.Name] = struct{}{}
		}
	}
	for i, in := range intents {
		if in.Action != gate.ActionDelete {
			continue
		}
		if mode == gate.ModeCurate {
			if _, ok := deleted[in.AbsorbedInto]; ok {
				return bad("intent %d: absorbed_into %q is deleted in the same reply", i, clip(in.AbsorbedInto))
			}
		}
		if _, ok := patched[in.AbsorbedInto]; !ok {
			return bad("intent %d: delete into %q needs a patch of %q in the same reply", i, clip(in.AbsorbedInto), clip(in.AbsorbedInto))
		}
	}
	return nil
}
