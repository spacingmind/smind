package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const summaryRunes = 120

// Summary describes a request body for the audit row without keeping it: the
// hash of the body minus "prov" and a short redacted prefix.
func Summary(body json.RawMessage, redact func(string) string) string {
	text := string(body)
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) == nil && m != nil {
		delete(m, "prov")
		if b, err := json.Marshal(m); err == nil {
			text = string(b)
		}
	}
	sum := sha256.Sum256([]byte(text))
	head := []rune(text)
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
