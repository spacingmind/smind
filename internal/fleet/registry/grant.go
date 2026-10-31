// Package registry is the fleet's host registry (ADR-0048 F1a): the grant
// model that says what a paired host may do, and the host/link records the
// daemon keeps. A pairing is two directed links; a grant travels with the
// inbound link and is written by the human on the receiving side.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/spacingmind/smind/internal/relay/devices"
)

// Operations a grant may name. Every op except read mutates something, so
// read is mandatory in any grant: an op without it could not read back what
// it changed.
const (
	OpRead   = "read"
	OpCreate = "create"
	OpSend   = "send"
	OpStop   = "stop"
)

// Limits a grant may not exceed. Cost is capped in dollars, not cents, so
// the JSON stays human-editable.
const (
	maxWorkspacesPerGrant  = 256
	maxProvidersPerGrant   = 64
	maxLiveAgentsPerGrant  = 64
	maxWallSecondsPerGrant = 604800 // 7 days
	maxCostUSDPerGrant     = 10000.0
)

// WorkspaceScope is which workspaces an op may touch: all of them, or a
// closed list of ids. All and IDs are mutually exclusive -- a list next to
// "all" would be either redundant or a lie.
type WorkspaceScope struct {
	All bool    `json:"all,omitempty"`
	IDs []int64 `json:"ids,omitempty"`
}

// Grant is what the far side of a link may do here. It is stored as JSON on
// the inbound link and is the only thing Authorize consults.
type Grant struct {
	Ops             []string       `json:"ops"`
	WorkspacesRead  WorkspaceScope `json:"workspacesRead"`
	WorkspacesWrite []int64        `json:"workspacesWrite"`
	Providers       []string       `json:"providers"`
	MaxLiveAgents   int            `json:"maxLiveAgents"`
	MaxWallSeconds  int            `json:"maxWallSeconds"`
	MaxCostUSD      float64        `json:"maxCostUsd"`
	// PermissionMode is "" (inherit the workspace's mode) or "default"
	// (force default permissions). Peer grants can never raise permissions.
	PermissionMode string `json:"permissionMode,omitempty"`
}

// DefaultForwardGrant is what a responder offers before a human narrows it
// (ADR-0048 S5): every op, read-everything, and the ADR's budget defaults.
// It has no write workspaces, so it is not valid for approval as-is.
func DefaultForwardGrant() Grant {
	return Grant{
		Ops:             []string{OpRead, OpCreate, OpSend, OpStop},
		WorkspacesRead:  WorkspaceScope{All: true},
		WorkspacesWrite: []int64{},
		Providers:       []string{},
		MaxLiveAgents:   2,
		MaxWallSeconds:  7200,
		MaxCostUSD:      5.00,
	}
}

// DefaultReverseGrant is what the initiator writes on its inbound link: the
// peer may read, nothing else, until a human widens it.
func DefaultReverseGrant() Grant {
	return Grant{
		Ops:             []string{OpRead},
		WorkspacesRead:  WorkspaceScope{All: true},
		WorkspacesWrite: []int64{},
		Providers:       []string{},
		MaxLiveAgents:   0,
		MaxWallSeconds:  0,
		MaxCostUSD:      0,
		PermissionMode:  "",
	}
}

// Validate checks the grant's shape. Every error names the field it rejects.
// Validate does not require a write workspace; ValidateForApproval does.
func (g Grant) Validate() error {
	switch {
	case len(g.Ops) == 0:
		return errors.New("grant.ops: at least one op is required")
	case !slices.Contains(g.Ops, OpRead):
		return fmt.Errorf("grant.ops: %q is required (every other op needs it)", OpRead)
	}
	seenOps := make(map[string]bool, len(g.Ops))
	for _, op := range g.Ops {
		switch {
		case op != OpRead && op != OpCreate && op != OpSend && op != OpStop:
			return fmt.Errorf("grant.ops: unknown op %q", op)
		case seenOps[op]:
			return fmt.Errorf("grant.ops: duplicate op %q", op)
		}
		seenOps[op] = true
	}

	if g.WorkspacesRead.All && len(g.WorkspacesRead.IDs) > 0 {
		return errors.New("grant.workspacesRead: all and ids are mutually exclusive")
	}
	seenRead := make(map[int64]bool, len(g.WorkspacesRead.IDs))
	for _, id := range g.WorkspacesRead.IDs {
		switch {
		case id <= 0:
			return fmt.Errorf("grant.workspacesRead: id must be > 0, got %d", id)
		case seenRead[id]:
			return fmt.Errorf("grant.workspacesRead: duplicate id %d", id)
		}
		seenRead[id] = true
	}

	if len(g.WorkspacesWrite) > maxWorkspacesPerGrant {
		return fmt.Errorf("grant.workspacesWrite: at most %d, got %d", maxWorkspacesPerGrant, len(g.WorkspacesWrite))
	}
	seenWrite := make(map[int64]bool, len(g.WorkspacesWrite))
	for _, id := range g.WorkspacesWrite {
		switch {
		case id <= 0:
			return fmt.Errorf("grant.workspacesWrite: id must be > 0, got %d", id)
		case seenWrite[id]:
			return fmt.Errorf("grant.workspacesWrite: duplicate id %d", id)
		}
		seenWrite[id] = true
	}

	if len(g.Providers) > maxProvidersPerGrant {
		return fmt.Errorf("grant.providers: at most %d, got %d", maxProvidersPerGrant, len(g.Providers))
	}
	seenProvider := make(map[string]bool, len(g.Providers))
	for _, p := range g.Providers {
		switch {
		case !providerRe.MatchString(p):
			return fmt.Errorf("grant.providers: %q must match %s", p, providerRe)
		case seenProvider[p]:
			return fmt.Errorf("grant.providers: duplicate %q", p)
		}
		seenProvider[p] = true
	}

	if g.MaxLiveAgents < 0 || g.MaxLiveAgents > maxLiveAgentsPerGrant {
		return fmt.Errorf("grant.maxLiveAgents: must be between 0 and %d, got %d", maxLiveAgentsPerGrant, g.MaxLiveAgents)
	}
	if g.MaxWallSeconds < 0 || g.MaxWallSeconds > maxWallSecondsPerGrant {
		return fmt.Errorf("grant.maxWallSeconds: must be between 0 and %d, got %d", maxWallSecondsPerGrant, g.MaxWallSeconds)
	}
	if math.IsNaN(g.MaxCostUSD) || math.IsInf(g.MaxCostUSD, 0) {
		return fmt.Errorf("grant.maxCostUsd: must be finite, got %g", g.MaxCostUSD)
	}
	if g.MaxCostUSD < 0 || g.MaxCostUSD > maxCostUSDPerGrant {
		return fmt.Errorf("grant.maxCostUsd: must be between 0 and %g, got %g", maxCostUSDPerGrant, g.MaxCostUSD)
	}
	if g.PermissionMode != "" && g.PermissionMode != "default" {
		return fmt.Errorf("grant.permissionMode: must be empty or %q, got %q", "default", g.PermissionMode)
	}
	return nil
}

// ValidateForApproval is Validate plus what a human must have decided before
// a grant may take effect: mutating ops need at least one workspace to act
// on, and create needs room to run in.
func (g Grant) ValidateForApproval() error {
	if err := g.Validate(); err != nil {
		return err
	}
	mutating := slices.Contains(g.Ops, OpCreate) || slices.Contains(g.Ops, OpSend) || slices.Contains(g.Ops, OpStop)
	if mutating && len(g.WorkspacesWrite) < 1 {
		return errors.New("grant.workspacesWrite: select at least one workspace to allow create, send or stop")
	}
	if slices.Contains(g.Ops, OpCreate) {
		if g.MaxLiveAgents < 1 {
			return errors.New("grant.maxLiveAgents: must be >= 1 when create is granted")
		}
		if g.MaxWallSeconds < 1 {
			return errors.New("grant.maxWallSeconds: must be >= 1 when create is granted")
		}
	}
	return nil
}

// Allows reports whether the grant names op.
func (g Grant) Allows(op string) bool { return slices.Contains(g.Ops, op) }

// CanRead reports whether the grant may read workspaceID. A scope of all
// covers every id; otherwise the id must be listed.
func (g Grant) CanRead(workspaceID int64) bool {
	return g.WorkspacesRead.All || slices.Contains(g.WorkspacesRead.IDs, workspaceID)
}

// CanWrite reports whether the grant may mutate workspaceID. There is no
// write-all: every writable workspace is listed by a human.
func (g Grant) CanWrite(workspaceID int64) bool {
	return slices.Contains(g.WorkspacesWrite, workspaceID)
}

// providerRe is the provider id shape: a lowercase slug, never empty, never
// starting with a separator.
var providerRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ParseGrant decodes raw as a grant, refusing unknown fields (a peer sending
// extra keys is a protocol change, not a hint) and any invalid shape. An
// empty string is an error, not a zero grant.
func ParseGrant(raw string) (Grant, error) {
	if raw == "" {
		return Grant{}, errors.New("grant: empty JSON")
	}
	var g Grant
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return Grant{}, fmt.Errorf("grant: parse: %w", err)
	}
	if err := g.Validate(); err != nil {
		return Grant{}, err
	}
	return g, nil
}

// JSON renders the grant for storage. Nil slices are normalised to empty
// first so they marshal as [] (never null), and marshal of this struct
// cannot fail -- every field is a string, int, float or slice of those --
// so the ignored error is unreachable.
func (g Grant) JSON() string {
	if g.Ops == nil {
		g.Ops = []string{}
	}
	if g.WorkspacesRead.IDs == nil {
		g.WorkspacesRead.IDs = []int64{}
	}
	if g.WorkspacesWrite == nil {
		g.WorkspacesWrite = []int64{}
	}
	if g.Providers == nil {
		g.Providers = []string{}
	}
	b, _ := json.Marshal(g)
	return string(b)
}

// aliasRe is the host alias shape: what a user types to address a peer, so
// it is short, lowercase, and hyphen-separated.
var aliasRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidateAlias rejects an alias that is not 1-32 characters of lowercase
// letters, digits and hyphens, starting with a letter.
func ValidateAlias(alias string) error {
	if alias == "" {
		return errors.New("alias: must not be empty")
	}
	if len(alias) > 32 {
		return fmt.Errorf("alias: at most 32 characters, got %d", len(alias))
	}
	if !aliasRe.MatchString(alias) {
		return fmt.Errorf("alias: %q must match %s", alias, aliasRe)
	}
	return nil
}

// ValidateLabel delegates to the relay's device label rules, so a host label
// and a device label accept the same text and reject the same spoofing
// characters.
func ValidateLabel(label string) error {
	if err := devices.ValidateLabel(label); err != nil {
		return fmt.Errorf("label: %w", err)
	}
	return nil
}
