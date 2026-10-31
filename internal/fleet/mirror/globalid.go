// Package mirror holds the cross-host task identity and the owner-only
// refresher of remote-task mirrors (ADR-0048 F4).
package mirror

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spacingmind/smind/internal/store"
)

// ErrUnknownHost is returned when a global id names an alias no registry
// row carries.
var ErrUnknownHost = errors.New("mirror: unknown host alias")

var (
	aliasRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	idRE    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// GlobalID is the user-facing id of a remote task: alias:id.
func GlobalID(alias, remoteTaskID string) string {
	return alias + ":" + remoteTaskID
}

// ParseGlobalID splits a global id on its first colon. A bare local id
// (no colon) is an error: a local id never names a remote task.
func ParseGlobalID(s string) (alias, id string, err error) {
	alias, id, ok := strings.Cut(s, ":")
	if !ok {
		return "", "", fmt.Errorf("mirror: global id %q: want alias:id", s)
	}
	if !aliasRE.MatchString(alias) {
		return "", "", fmt.Errorf("mirror: global id %q: bad alias", s)
	}
	if !idRE.MatchString(id) {
		return "", "", fmt.Errorf("mirror: global id %q: bad task id", s)
	}
	return alias, id, nil
}

// HostLister lists the registry rows.
type HostLister interface {
	ListFleetHosts() ([]store.FleetHost, error)
}

// Owner resolves a global id to the owner's host id and remote task id.
// The alias must match exactly one registry row.
func Owner(l HostLister, globalID string) (hostID, remoteTaskID string, err error) {
	alias, id, err := ParseGlobalID(globalID)
	if err != nil {
		return "", "", err
	}
	hosts, err := l.ListFleetHosts()
	if err != nil {
		return "", "", fmt.Errorf("mirror: owner of %q: %w", globalID, err)
	}
	found := ""
	for _, h := range hosts {
		if h.Alias != alias {
			continue
		}
		if found != "" {
			return "", "", fmt.Errorf("mirror: alias %q is not unique", alias)
		}
		found = h.HostID
	}
	if found == "" {
		return "", "", fmt.Errorf("%w: %s", ErrUnknownHost, alias)
	}
	return found, id, nil
}
