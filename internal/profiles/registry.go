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
	"fmt"
	"strings"
	"sync"

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
	if err := validate(p); err != nil {
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

// Get returns the profile with the given id.
func (r *Registry) Get(id int64) (store.AgentProfile, error) {
	return r.store.GetAgentProfile(id)
}

// List returns every profile, ordered by id.
func (r *Registry) List() ([]store.AgentProfile, error) {
	return r.store.ListAgentProfiles()
}

// Update validates p and replaces the stored profile with id p.ID (a
// full-record replace -- see store.UpdateAgentProfile), firing
// NotifyProfileUpdated on success.
func (r *Registry) Update(p store.AgentProfile) (store.AgentProfile, error) {
	if err := validate(p); err != nil {
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
	if err := r.store.DeleteAgentProfile(id); err != nil {
		return err
	}
	if n := r.getNotifier(); n != nil {
		n.NotifyProfileDeleted(id)
	}
	return nil
}

// validate rejects a profile this package's callers should never be able
// to store: an empty name, an unknown provider id, or an approvalPolicy/
// thinkingLevel string that internal/taskrunner itself would reject from
// task.prompt -- so a stored profile can never carry a combination the wire
// would reject if applied directly.
func validate(p store.AgentProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !isSupportedProvider(p.Provider) {
		return fmt.Errorf("unknown provider %q", p.Provider)
	}
	if p.ApprovalPolicy != "" && !taskrunner.ApprovalPolicy(p.ApprovalPolicy).IsValid() {
		return fmt.Errorf("invalid approvalPolicy %q", p.ApprovalPolicy)
	}
	if p.ThinkingLevel != "" && !taskrunner.ThinkingLevel(p.ThinkingLevel).IsValid() {
		return fmt.Errorf("invalid thinkingLevel %q", p.ThinkingLevel)
	}
	return nil
}

func isSupportedProvider(id string) bool {
	for _, p := range taskrunner.SupportedProviders() {
		if string(p.ID) == id {
			return true
		}
	}
	return false
}
