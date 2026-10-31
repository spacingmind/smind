package config

// StewardConfig is the steward section (ADR-0047). Everything under it is a
// human edit of config.yaml only -- no RPC, agent or relay session can
// change it -- and the daemon reads the effective value on every decision
// (like plugins.enabled), so turning the engine on or off never needs a
// restart to take effect.
type StewardConfig struct {
	Policy StewardPolicyConfig `yaml:"policy"`
}

// StewardPolicyConfig is the permission policy engine's switch (ADR-0047
// §2). Off by default: with no steward block the permission path is
// byte-for-byte today's. The switch being on says nothing about the policy
// file -- an invalid or unreadable file then means floor + ask, never
// allow-all -- and only a human edit turns it on.
type StewardPolicyConfig struct {
	Enabled bool `yaml:"enabled"`
}
