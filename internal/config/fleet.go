package config

import "fmt"

// FleetConfig is the fleet section (ADR-0048). It is off by default and only
// a human edit of config.yaml turns it on: no RPC, agent or relay session
// can change it. With fleet.enabled false the daemon's behaviour is
// byte-for-byte today's -- nothing dials out and no fleet state is kept.
// Validate runs even when the section is off, so a typo in a value nobody
// has switched on yet still surfaces at load time.
type FleetConfig struct {
	// Enabled turns the fleet layer on.
	Enabled bool `yaml:"enabled"`
	// MaxHosts caps how many hosts one daemon may know about.
	MaxHosts int `yaml:"maxHosts"`
	// MaxConnectionsPerPeer caps simultaneous links to one peer host.
	MaxConnectionsPerPeer int `yaml:"maxConnectionsPerPeer"`
	// HandshakeSeconds bounds how long a peer handshake may take.
	HandshakeSeconds int `yaml:"handshakeSeconds"`
	// MaxFrameKiB caps one fleet frame's payload size.
	MaxFrameKiB int `yaml:"maxFrameKiB"`
	// MaxOutstandingPerLink caps in-flight frames per link before backpressure.
	MaxOutstandingPerLink int `yaml:"maxOutstandingPerLink"`
	// ObservationBackoffMinSeconds is the floor of the observer's backoff.
	ObservationBackoffMinSeconds int `yaml:"observationBackoffMinSeconds"`
	// ObservationBackoffMaxSeconds is the ceiling of the observer's backoff;
	// must be at least the min.
	ObservationBackoffMaxSeconds int `yaml:"observationBackoffMaxSeconds"`
	// IdempotencyHours bounds how long a duplicate request is deduped.
	IdempotencyHours int `yaml:"idempotencyHours"`
	// ReplayWindowMinutes bounds how old a peer message's timestamp may be.
	ReplayWindowMinutes int `yaml:"replayWindowMinutes"`
	// AuditRetentionDays is how long fleet audit records are kept.
	AuditRetentionDays int `yaml:"auditRetentionDays"`
}

// Defaults for FleetConfig.
const (
	DefaultFleetMaxHosts                     = 16
	DefaultFleetMaxConnectionsPerPeer        = 1
	DefaultFleetHandshakeSeconds             = 10
	DefaultFleetMaxFrameKiB                  = 256
	DefaultFleetMaxOutstandingPerLink        = 16
	DefaultFleetObservationBackoffMinSeconds = 1
	DefaultFleetObservationBackoffMaxSeconds = 60
	DefaultFleetIdempotencyHours             = 24
	DefaultFleetReplayWindowMinutes          = 10
	DefaultFleetAuditRetentionDays           = 30
)

// DefaultFleet returns the fleet section's built-in defaults, off.
func DefaultFleet() FleetConfig {
	return FleetConfig{
		MaxHosts:                     DefaultFleetMaxHosts,
		MaxConnectionsPerPeer:        DefaultFleetMaxConnectionsPerPeer,
		HandshakeSeconds:             DefaultFleetHandshakeSeconds,
		MaxFrameKiB:                  DefaultFleetMaxFrameKiB,
		MaxOutstandingPerLink:        DefaultFleetMaxOutstandingPerLink,
		ObservationBackoffMinSeconds: DefaultFleetObservationBackoffMinSeconds,
		ObservationBackoffMaxSeconds: DefaultFleetObservationBackoffMaxSeconds,
		IdempotencyHours:             DefaultFleetIdempotencyHours,
		ReplayWindowMinutes:          DefaultFleetReplayWindowMinutes,
		AuditRetentionDays:           DefaultFleetAuditRetentionDays,
	}
}

// Validate rejects out-of-range values, naming the yaml path and the bad
// value in every message.
func (c FleetConfig) Validate() error {
	if c.MaxHosts < 1 || c.MaxHosts > 256 {
		return fmt.Errorf("fleet.maxHosts must be between 1 and 256, got %d", c.MaxHosts)
	}
	if c.MaxConnectionsPerPeer < 1 || c.MaxConnectionsPerPeer > 8 {
		return fmt.Errorf("fleet.maxConnectionsPerPeer must be between 1 and 8, got %d", c.MaxConnectionsPerPeer)
	}
	if c.HandshakeSeconds < 1 || c.HandshakeSeconds > 120 {
		return fmt.Errorf("fleet.handshakeSeconds must be between 1 and 120, got %d", c.HandshakeSeconds)
	}
	if c.MaxFrameKiB < 16 || c.MaxFrameKiB > 4096 {
		return fmt.Errorf("fleet.maxFrameKiB must be between 16 and 4096, got %d", c.MaxFrameKiB)
	}
	if c.MaxOutstandingPerLink < 1 || c.MaxOutstandingPerLink > 256 {
		return fmt.Errorf("fleet.maxOutstandingPerLink must be between 1 and 256, got %d", c.MaxOutstandingPerLink)
	}
	if c.ObservationBackoffMinSeconds < 1 || c.ObservationBackoffMinSeconds > 3600 {
		return fmt.Errorf("fleet.observationBackoffMinSeconds must be between 1 and 3600, got %d", c.ObservationBackoffMinSeconds)
	}
	if c.ObservationBackoffMaxSeconds < 1 || c.ObservationBackoffMaxSeconds > 3600 {
		return fmt.Errorf("fleet.observationBackoffMaxSeconds must be between 1 and 3600, got %d", c.ObservationBackoffMaxSeconds)
	}
	if c.ObservationBackoffMaxSeconds < c.ObservationBackoffMinSeconds {
		return fmt.Errorf("fleet.observationBackoffMaxSeconds must be >= observationBackoffMinSeconds (%d), got %d", c.ObservationBackoffMinSeconds, c.ObservationBackoffMaxSeconds)
	}
	if c.IdempotencyHours < 1 || c.IdempotencyHours > 168 {
		return fmt.Errorf("fleet.idempotencyHours must be between 1 and 168, got %d", c.IdempotencyHours)
	}
	if c.ReplayWindowMinutes < 1 || c.ReplayWindowMinutes > 60 {
		return fmt.Errorf("fleet.replayWindowMinutes must be between 1 and 60, got %d", c.ReplayWindowMinutes)
	}
	if c.AuditRetentionDays < 1 || c.AuditRetentionDays > 365 {
		return fmt.Errorf("fleet.auditRetentionDays must be between 1 and 365, got %d", c.AuditRetentionDays)
	}
	return nil
}
