package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// AuditEntry is one line of config-audit.jsonl (ADR-0062): every config.set
// attempt, allowed or refused, with who asked and how it ended.
type AuditEntry struct {
	Time    string `json:"time"`
	Key     string `json:"key"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Outcome string `json:"outcome"`
	Channel string `json:"channel"`
	Caller  string `json:"caller"`
}

const auditFileName = "config-audit.jsonl"

// auditMaxBytes is the size at which the audit file rotates to .1.
var auditMaxBytes int64 = 1 << 20

var auditMu sync.Mutex

var auditSecretRE = regexp.MustCompile(`(?i)password|token|secret|key-?material|keymaterial`)

// AppendAudit appends one JSON line to config-audit.jsonl in the daemon home,
// owner-only (0600). Values of secret-looking keys are written as
// "<redacted>"; no allowlisted key is secret, so that is defence in depth.
func AppendAudit(e AuditEntry) error {
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	if auditSecretRE.MatchString(e.Key) {
		e.Old, e.New = "<redacted>", "<redacted>"
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	auditMu.Lock()
	defer auditMu.Unlock()
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config audit: %w", err)
	}
	path := filepath.Join(dir, auditFileName)
	if st, err := os.Stat(path); err == nil && st.Size() >= auditMaxBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return fmt.Errorf("config audit: rotate: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("config audit: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("config audit: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("config audit: %w", err)
	}
	return nil
}
