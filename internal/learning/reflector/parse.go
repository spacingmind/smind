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
	"strings"
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
// entire message (surrounding whitespace aside) and returns its intents. Any
// violation returns an error wrapping ErrBadOutput and a nil slice: nothing
// partial is ever returned, and one bad intent fails the whole message. It
// does not run the gate and relaxes none of the gate's evidence rules.
func ParseOutput(raw string) ([]gate.Intent, error) {
	if len(raw) > MaxOutputBytes {
		return nil, bad("message is %d bytes, max %d", len(raw), MaxOutputBytes)
	}
	if !utf8.ValidString(raw) {
		return nil, bad("invalid UTF-8")
	}
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, bad("message is not a single JSON object")
	}
	if err := rejectDuplicateKeys(s); err != nil {
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
		if err := checkIntent(in); err != nil {
			return nil, bad("intent %d: %v", i, err)
		}
	}
	if intents == nil {
		intents = []gate.Intent{}
	}
	return intents, nil
}

func checkIntent(in gate.Intent) error {
	switch in.Action {
	case gate.ActionCreate, gate.ActionPatch, gate.ActionDelete:
	default:
		return fmt.Errorf("unknown action %q", clip(string(in.Action)))
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

// rejectDuplicateKeys walks the JSON text token by token and fails on a
// repeated key in any object (encoding/json would silently keep the last one)
// or on nesting deeper than maxDepth.
func rejectDuplicateKeys(s string) error {
	dec := json.NewDecoder(strings.NewReader(s))
	if err := walkValue(dec, 1); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad("trailing data after the JSON object")
	}
	return nil
}

func walkValue(dec *json.Decoder, depth int) error {
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
		seen := map[string]struct{}{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return bad("decode: %v", err)
			}
			key, _ := kt.(string)
			if _, dup := seen[key]; dup {
				return bad("duplicate key %q", clip(key))
			}
			seen[key] = struct{}{}
			if err := walkValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkValue(dec, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // closing delimiter
		return bad("decode: %v", err)
	}
	return nil
}
