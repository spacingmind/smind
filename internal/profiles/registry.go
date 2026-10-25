// Package profiles provides AgentProfile CRUD on top of internal/store,
// validating each write against internal/taskrunner's provider/policy/
// thinking-level vocabulary (docs/decisions/0014-agent-profiles.md). It
// exists as its own package, rather than living on store.Store directly or
// inline in internal/wsapi's handlers, because internal/store must not
// import internal/taskrunner (see store.AgentProfile's doc comment) --
// something has to sit between the two to do this validation, and a thin
// Registry (mirroring internal/accounts.Registry's shape) gives the wsapi
// handlers and any future non-wsapi caller one shared validation path
// instead of duplicating it.
package profiles

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Registry provides validated AgentProfile CRUD backed by a store.Store.
type Registry struct {
	store *store.Store

	// notifier, if set via SetNotifier, receives agent-profile lifecycle
	// notifications (create/update/delete) so the wsapi server can push
	// them as ADR-0009-shaped subscription events. Mirrors
	// internal/workspace.Manager's Notifier/SetNotifier pattern.
	notifierMu sync.Mutex
	notifier   Notifier

	skillMu sync.Mutex
	skills  SkillResolver
}

// Notifier receives agent-profile lifecycle notifications. Every method is
// called synchronously from the Registry method that made the change,
// after the underlying store write committed; implementations
// (internal/wsapi's bus adapter) must not block.
type Notifier interface {
	NotifyProfileCreated(p store.AgentProfile)
	NotifyProfileUpdated(p store.AgentProfile)
	NotifyProfileDeleted(id int64)
}

// New returns a Registry backed by s.
func New(s *store.Store) *Registry {
	return &Registry{store: s}
}

// SetNotifier registers n. Nil-safe: notifications with no notifier
// registered are no-ops, and a nil *Registry accepts the call rather than
// panicking -- matches workspace.Manager.SetNotifier's shape.
func (r *Registry) SetNotifier(n Notifier) {
	if r == nil {
		return
	}
	r.notifierMu.Lock()
	r.notifier = n
	r.notifierMu.Unlock()
}

func (r *Registry) getNotifier() Notifier {
	if r == nil {
		return nil
	}
	r.notifierMu.Lock()
	n := r.notifier
	r.notifierMu.Unlock()
	return n
}

// Create validates p and inserts it, firing NotifyProfileCreated on
// success.
func (r *Registry) Create(p store.AgentProfile) (store.AgentProfile, error) {
	if err := r.refuseOwnedName(strings.TrimSpace(p.Name)); err != nil {
		return store.AgentProfile{}, err
	}
	p.Owner, p.ReadOnly = nil, false
	if err := r.validate(&p); err != nil {
		return store.AgentProfile{}, err
	}
	created, err := r.store.CreateAgentProfile(p)
	if err != nil {
		return store.AgentProfile{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyProfileCreated(created)
	}
	return created, nil
}

// SkillResolver resolves "<plugin>:<skill>" references against the plugins
// that are active now (ADR-0042). It is set at wiring time.
type SkillResolver interface {
	PluginSkill(qualified string) (plugin string, ok bool)
	// PluginActive reports whether the named plugin is installed and active.
	PluginActive(plugin string) bool
}

// SetSkillResolver installs the plugin skill resolver (nil: plugin skill
// names are rejected).
func (r *Registry) SetSkillResolver(s SkillResolver) {
	r.skillMu.Lock()
	r.skills = s
	r.skillMu.Unlock()
}

func (r *Registry) skillResolver() SkillResolver {
	r.skillMu.Lock()
	defer r.skillMu.Unlock()
	return r.skills
}

// Get returns the profile with the given id.
func (r *Registry) Get(id int64) (store.AgentProfile, error) {
	return r.store.GetAgentProfile(id)
}

// List returns every user profile ordered by id, then the profiles enabled
// plugins contribute (owned, read-only, "<plugin>:<name>").
func (r *Registry) List() ([]store.AgentProfile, error) {
	rows, err := r.store.ListAgentProfiles()
	if err != nil {
		return nil, err
	}
	if o := r.store.Overlay(); o != nil {
		rows = append(rows, o.OwnedProfiles()...)
	}
	return rows, nil
}

// ownedProfile returns the owned profile with the given id, if its plugin is
// active.
func (r *Registry) ownedProfile(id int64) (store.AgentProfile, bool) {
	if !store.IsOwnedID(id) {
		return store.AgentProfile{}, false
	}
	if o := r.store.Overlay(); o != nil {
		return o.OwnedProfile(id)
	}
	return store.AgentProfile{}, false
}

// refuseOwned is the guard of every write by id: a profile a plugin owns is
// read-only for every caller, human or agent.
func (r *Registry) refuseOwned(id int64) error {
	if p, ok := r.ownedProfile(id); ok {
		return &store.OwnedRowError{Plugin: *p.Owner, Kind: "profile", Name: p.Name}
	}
	return nil
}

// refuseOwnedName rejects a user profile named like a profile an active
// plugin contributes, so a user row can never shadow (or be shadowed by) an
// owned one.
func (r *Registry) refuseOwnedName(name string) error {
	if !strings.Contains(name, ":") {
		return nil
	}
	if o := r.store.Overlay(); o != nil {
		for _, p := range o.OwnedProfiles() {
			if p.Name == name {
				return fmt.Errorf("name %q is taken by a profile plugin %q contributes; pick another name", name, *p.Owner)
			}
		}
	}
	return nil
}

// ValidateOwned checks the provider-level fields of a profile a plugin
// contributes (provider, permission mode, thinking level, model, sizes). It
// does not check skills or child references: those may name plugins that are
// not active.
func (r *Registry) ValidateOwned(p store.AgentProfile) error {
	return r.validateFields(&p)
}

// Update validates p and replaces the stored profile with id p.ID (a
// full-record replace -- see store.UpdateAgentProfile), firing
// NotifyProfileUpdated on success.
func (r *Registry) Update(p store.AgentProfile) (store.AgentProfile, error) {
	if err := r.refuseOwned(p.ID); err != nil {
		return store.AgentProfile{}, err
	}
	if err := r.refuseOwnedName(strings.TrimSpace(p.Name)); err != nil {
		return store.AgentProfile{}, err
	}
	p.Owner, p.ReadOnly = nil, false
	if err := r.validate(&p); err != nil {
		return store.AgentProfile{}, err
	}
	updated, err := r.store.UpdateAgentProfile(p)
	if err != nil {
		return store.AgentProfile{}, err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyProfileUpdated(updated)
	}
	return updated, nil
}

// Delete removes the profile with the given id, firing NotifyProfileDeleted
// on success.
func (r *Registry) Delete(id int64) error {
	if err := r.refuseOwned(id); err != nil {
		return err
	}
	if err := r.store.DeleteAgentProfile(id); err != nil {
		return err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyProfileDeleted(id)
	}
	return nil
}

// validate rejects a profile this package's callers should never be able
// to store: an empty name, an unknown provider id, a permission mode /
// autoAccept the provider's static catalog rejects (ADR-0019 -- an ACP
// provider accepts any mode id, since its modes are the agent's own), or a
// thinkingLevel or model string that internal/taskrunner itself would reject from
// task.prompt -- so a stored profile can never carry a combination the wire
// would reject if applied directly.
func (r *Registry) validate(p *store.AgentProfile) error {
	if err := r.validateFields(p); err != nil {
		return err
	}
	if err := r.validateFailover(p); err != nil {
		return err
	}
	return r.validateContract(p)
}

// validateFields is the provider-level part of validate.
func (r *Registry) validateFields(p *store.AgentProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	info, ok := taskrunner.ProviderInfoFor(taskrunner.Provider(p.Provider))
	if !ok {
		return fmt.Errorf("unknown provider %q", p.Provider)
	}
	if err := taskrunner.ValidatePermissionSettings(info, taskrunner.PermissionSettings{Mode: p.PermissionMode, AutoAccept: p.AutoAccept}); err != nil {
		return err
	}
	if p.ThinkingLevel != "" && !taskrunner.ThinkingLevel(p.ThinkingLevel).IsValid() {
		return fmt.Errorf("invalid thinkingLevel %q", p.ThinkingLevel)
	}
	// Against the same static catalog as the mode above: syntax always, and
	// membership only for a provider whose models are known without a
	// running agent (none today -- ADR-0025), so the string reaches run
	// start, which validates it against the live catalog.
	if err := taskrunner.ValidateModel(info, p.Model); err != nil {
		return err
	}
	if len(p.Instructions) > maxInstructionsBytes {
		return fmt.Errorf("instructions are %d bytes, over the %d byte cap", len(p.Instructions), maxInstructionsBytes)
	}
	return nil
}

// Seat-contract size caps (ADR-0041 S1), in bytes.
const (
	maxInstructionsBytes = 16 << 10
	maxContractBytes     = 32 << 10
)

// validateContract checks the ADR-0041 seat contract and normalises
// ChildPolicy: "" stays "" only for a pure legacy profile (no
// orchestrationTools, instructions or skills); otherwise an unset policy
// becomes "none". orchestrationTools=false forces "none".
func (r *Registry) validateContract(p *store.AgentProfile) error {
	if len(p.Instructions) > maxInstructionsBytes {
		return fmt.Errorf("instructions are %d bytes, over the %d byte cap", len(p.Instructions), maxInstructionsBytes)
	}
	if len(p.Skills) > 0 {
		bundle, err := skills.Bundle()
		if err != nil {
			return fmt.Errorf("load bundled skills: %w", err)
		}
		bodies := make(map[string]int, len(bundle))
		for _, sk := range bundle {
			bodies[sk.Name] = len(sk.Files["SKILL.md"])
		}
		total := len(p.Instructions)
		seen := make(map[string]bool, len(p.Skills))
		for _, name := range p.Skills {
			size, ok := bodies[name]
			if strings.Contains(name, ":") {
				plugin, _, _ := strings.Cut(name, ":")
				res := r.skillResolver()
				switch {
				case res == nil || !res.PluginActive(plugin):
					return fmt.Errorf("unknown skill %q: plugin %q is not installed and enabled", name, plugin)
				default:
					if _, found := res.PluginSkill(name); !found {
						return fmt.Errorf("unknown skill %q: plugin %q has no skill by that name", name, plugin)
					}
				}
				size, ok = 0, true // inlined at run time; the cap below counts bundled bodies only
			}
			if !ok {
				return fmt.Errorf("unknown skill %q (not a bundled skill)", name)
			}
			if seen[name] {
				return fmt.Errorf("duplicate skill %q", name)
			}
			seen[name] = true
			total += size
		}
		if total > maxContractBytes {
			return fmt.Errorf("instructions plus skills are %d bytes, over the %d byte cap", total, maxContractBytes)
		}
	}

	switch p.ChildPolicy {
	case "", "none", "leaf", "profiles":
	default:
		return fmt.Errorf("invalid childPolicy %q (want none, leaf or profiles)", p.ChildPolicy)
	}
	if p.OrchestrationTools != nil && !*p.OrchestrationTools && (p.ChildPolicy == "leaf" || p.ChildPolicy == "profiles") {
		return fmt.Errorf("a profile without orchestration tools cannot launch children")
	}
	switch p.ChildPolicy {
	case "profiles":
		if len(p.ChildProfileIDs) == 0 {
			return fmt.Errorf("childPolicy profiles requires childProfileIds")
		}
		seen := make(map[int64]bool, len(p.ChildProfileIDs))
		for _, id := range p.ChildProfileIDs {
			if p.ID != 0 && id == p.ID {
				return fmt.Errorf("a profile cannot list itself as a child")
			}
			if seen[id] {
				return fmt.Errorf("duplicate child profile %d", id)
			}
			seen[id] = true
			if store.IsOwnedID(id) {
				return fmt.Errorf("child profile %d belongs to a plugin and cannot be referenced by a stored profile", id)
			}
			if _, err := r.store.GetAgentProfile(id); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("child profile %d not found", id)
				}
				return err
			}
		}
	default:
		if len(p.ChildProfileIDs) > 0 {
			return fmt.Errorf("childProfileIds requires childPolicy profiles")
		}
	}

	legacy := p.OrchestrationTools == nil && p.Instructions == "" && len(p.Skills) == 0
	if p.ChildPolicy == "" && !legacy {
		p.ChildPolicy = "none"
	}
	return nil
}

// validateFailover checks the ADR-0041 rate-limit policy fields and
// normalises an empty OnRateLimit to "none". The fallback is looked up
// through the store so it must exist, must not be the profile itself and
// must use a different provider.
func (r *Registry) validateFailover(p *store.AgentProfile) error {
	switch p.OnRateLimit {
	case "":
		p.OnRateLimit = "none"
	case "none", "wait", "fallback":
	default:
		return fmt.Errorf("invalid onRateLimit %q (want none, wait or fallback)", p.OnRateLimit)
	}
	if p.FallbackProfileID == nil {
		if p.OnRateLimit == "fallback" {
			return fmt.Errorf("onRateLimit fallback requires fallbackProfileId")
		}
		return nil
	}
	id := *p.FallbackProfileID
	if store.IsOwnedID(id) {
		return fmt.Errorf("fallback profile %d belongs to a plugin and cannot be referenced by a stored profile", id)
	}
	if p.ID != 0 && id == p.ID {
		return fmt.Errorf("fallback profile must not be the profile itself")
	}
	fb, err := r.store.GetAgentProfile(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("fallback profile %d not found", id)
		}
		return err
	}
	if fb.Provider == p.Provider {
		return fmt.Errorf("fallback profile must use a different provider than the profile it backs up")
	}
	return nil
}
