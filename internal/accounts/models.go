package accounts

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// ValidateModels checks every glob in models parses as a path.Match pattern.
func ValidateModels(models []string) error {
	for _, g := range models {
		if strings.TrimSpace(g) == "" {
			return fmt.Errorf("models: empty glob")
		}
		if _, err := path.Match(g, ""); err != nil {
			return fmt.Errorf("models: bad glob %q: %w", g, err)
		}
	}
	return nil
}

// EncodeModels validates models and returns its stored JSON form; an empty
// list encodes as "" (stored as NULL, i.e. "no list").
func EncodeModels(models []string) (string, error) {
	if len(models) == 0 {
		return "", nil
	}
	if err := ValidateModels(models); err != nil {
		return "", err
	}
	data, err := json.Marshal(models)
	if err != nil {
		return "", fmt.Errorf("marshal models: %w", err)
	}
	return string(data), nil
}

// MatchesModel reports whether any of the account's model globs matches
// model. An account with no list never matches (see ADR-0020 §5: such
// accounts are only a fallback).
func (a Account) MatchesModel(model string) bool {
	if model == "" {
		return false
	}
	for _, g := range a.Models {
		if ok, err := path.Match(g, model); err == nil && ok {
			return true
		}
	}
	return false
}
