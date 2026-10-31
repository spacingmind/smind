package registry

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MaxLabels caps the operator labels on one host (ADR-0048 F5).
const MaxLabels = 16

var (
	labelKeyRE   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,31}$`)
	labelValueRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,31}$`)

	// labelSecretWords keep credentials out of labels: they are shown in
	// listings and prompts, so a key that sounds like a secret is refused.
	labelSecretWords = []string{"secret", "token", "passw", "apikey", "credential"}
)

// ValidateLabels checks an operator label set: at most MaxLabels, keys
// and values from a small lowercase alphabet, no secret-sounding keys.
// A nil or empty map is valid.
func ValidateLabels(m map[string]string) error {
	if len(m) > MaxLabels {
		return fmt.Errorf("labels: too many labels (max %d)", MaxLabels)
	}
	for k, v := range m {
		if !labelKeyRE.MatchString(k) {
			return fmt.Errorf("labels: key %q must match %s", k, labelKeyRE)
		}
		lk := strings.ToLower(k)
		for _, w := range labelSecretWords {
			if strings.Contains(lk, w) {
				return fmt.Errorf("labels: key %q looks like a secret", k)
			}
		}
		if !labelValueRE.MatchString(v) {
			return fmt.Errorf("labels: value for key %q must match %s", k, labelValueRE)
		}
	}
	return nil
}

// encodeLabels renders m as a JSON object (encoding/json sorts the keys),
// "" for an empty set.
func encodeLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeLabels parses stored labels; "" or malformed JSON is nil.
func decodeLabels(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}
