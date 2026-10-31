package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const summaryRunes = 120

// bodyKeys are the request fields that carry free text a person or an agent
// wrote (a prompt, a message). The audit keeps a hash of the request and a short
// redacted prefix of it, and never any of these values: Summary replaces each with
// its length before it takes the prefix, so a prompt cannot reach the table by
// being the first thing in the JSON.
var bodyKeys = map[string]bool{"prompt": true, "text": true, "message": true, "body": true, "content": true, "input": true}

// maskBodies returns v with every string under a bodyKeys key replaced by
// "<N chars>", at any depth.
func maskBodies(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if str, ok := val.(string); ok && bodyKeys[k] {
				t[k] = "<" + strconv.Itoa(utf8.RuneCountInString(str)) + " chars>"
				continue
			}
			t[k] = maskBodies(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = maskBodies(t[i])
		}
		return t
	}
	return v
}

// Summary describes a request body for the audit row without keeping it: the
// hash of the body minus "prov", and a short redacted prefix of the body with
// its free-text fields (bodyKeys) reduced to their length.
func Summary(body json.RawMessage, redact func(string) string) string {
	text := string(body)
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) == nil && m != nil {
		delete(m, "prov")
		if b, err := json.Marshal(m); err == nil {
			text = string(b)
		}
	}
	// The hash covers the real body (prov aside); only what is shown is masked.
	shown := text
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v map[string]any
	if dec.Decode(&v) == nil && v != nil {
		var out strings.Builder
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		if enc.Encode(maskBodies(v)) == nil {
			shown = strings.TrimSuffix(out.String(), "\n")
		}
	}
	sum := sha256.Sum256([]byte(text))
	head := []rune(shown)
	if len(head) > summaryRunes {
		head = head[:summaryRunes]
	}
	return "sha256=" + hex.EncodeToString(sum[:]) + " " + redact(string(head))
}

// Sweeper deletes audit rows older than a cutoff.
type Sweeper interface {
	SweepFleetAudit(before time.Time) (int64, error)
}

// SweepAudit deletes rows older than now-retention.
func SweepAudit(sw Sweeper, now time.Time, retention time.Duration) (int64, error) {
	return sw.SweepFleetAudit(now.Add(-retention))
}

// StartAuditSweeper sweeps once now and then every period until stop is
// called; stop is idempotent and waits for the goroutine.
func StartAuditSweeper(sw Sweeper, retention, period time.Duration) (stop func()) {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, _ = SweepAudit(sw, time.Now(), retention)
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_, _ = SweepAudit(sw, time.Now(), retention)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}
