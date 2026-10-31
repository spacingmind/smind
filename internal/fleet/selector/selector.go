// Package selector picks a fleet host by operator labels. It is pure: no I/O,
// no time, no retries.
package selector

import (
	"fmt"
	"sort"
	"strings"
)

// Selector picks a host by operator labels. Labels must be non-empty.
type Selector struct {
	Labels map[string]string
	Any    bool
}

// Candidate is one host as the caller sees it.
type Candidate struct {
	Alias, HostID string
	Labels        map[string]string
	Enabled       bool
	Connected     bool
	Live          int
}

const (
	CodeNoMatch   = "selector_no_match"
	CodeAmbiguous = "selector_ambiguous"
	CodeInvalid   = "selector_invalid"
)

// Error is a model-readable selector failure; Code is one of the Code*
// constants, Candidates the aliases listed in the message.
type Error struct {
	Code       string
	Candidates []string
	msg        string
}

func (e *Error) Error() string { return e.msg }

// String renders the labels as sorted "k:v" pairs joined by commas.
func (s Selector) String() string {
	parts := make([]string, 0, len(s.Labels))
	for k, v := range s.Labels {
		parts = append(parts, k+":"+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// Select resolves sel against cands without mutating either.
func Select(sel Selector, cands []Candidate) (Candidate, error) {
	if len(sel.Labels) == 0 {
		return Candidate{}, &Error{Code: CodeInvalid, msg: CodeInvalid + ": labels must name at least one label"}
	}
	var eligible []Candidate
	unavailable := []string{}
	for _, c := range cands {
		if !labelled(sel, c) {
			continue
		}
		if c.Enabled && c.Connected {
			eligible = append(eligible, c)
		} else {
			unavailable = append(unavailable, c.Alias)
		}
	}
	sort.Strings(unavailable)
	switch {
	case len(eligible) == 0:
		msg := fmt.Sprintf("%s: no enabled, connected host has labels %s", CodeNoMatch, sel)
		if len(unavailable) > 0 {
			msg += "; labelled but unavailable (disabled or disconnected): " + strings.Join(unavailable, ", ")
		} else {
			msg += "; no host has these labels"
		}
		return Candidate{}, &Error{Code: CodeNoMatch, Candidates: unavailable, msg: msg}
	case len(eligible) == 1:
		return eligible[0], nil
	case !sel.Any:
		aliases := make([]string, len(eligible))
		for i, c := range eligible {
			aliases[i] = c.Alias
		}
		sort.Strings(aliases)
		msg := fmt.Sprintf("%s: %d hosts match %s: %s; add labels to narrow it or pass any:true to take the least busy",
			CodeAmbiguous, len(aliases), sel, strings.Join(aliases, ", "))
		return Candidate{}, &Error{Code: CodeAmbiguous, Candidates: aliases, msg: msg}
	}
	best := eligible[0]
	for _, c := range eligible[1:] {
		if c.Live < best.Live || (c.Live == best.Live && c.Alias < best.Alias) {
			best = c
		}
	}
	return best, nil
}

func labelled(sel Selector, c Candidate) bool {
	for k, v := range sel.Labels {
		if got, ok := c.Labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}
