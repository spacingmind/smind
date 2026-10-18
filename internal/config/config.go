// Package config loads smind daemon configuration.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root daemon configuration.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Orchestration OrchestrationConfig `yaml:"orchestration"`
	Worktrees     WorktreesConfig     `yaml:"worktrees"`
	Agents        AgentsConfig        `yaml:"agents"`
	AccountUsage  AccountUsageConfig  `yaml:"accountUsage"`
	Relay         RelayConfig         `yaml:"relay"`
	Learning      LearningConfig      `yaml:"learning"`
	Log           LogConfig           `yaml:"log"`
}

// LogConfig tunes the daemon's own rotating log file, $SMIND_HOME/daemon.log
// (ADR-0031 decision 7). The daemon also keeps logging to stderr.
type LogConfig struct {
	// MaxSizeMB is the size at which daemon.log is rotated. Default 10;
	// Load rejects values < 1.
	MaxSizeMB int `yaml:"maxSizeMB"`
	// MaxFiles is how many log files are kept in total, the active one
	// included (daemon.log, daemon.log.1, ...). Default 5; Load rejects
	// values < 1.
	MaxFiles int `yaml:"maxFiles"`
}

// Defaults for LogConfig.
const (
	DefaultLogMaxSizeMB = 10
	DefaultLogMaxFiles  = 5
)

// RelayConfig tunes this daemon's relay client side (ADR-0031 D9).
type RelayConfig struct {
	// LegacyDevices admits relay peers that do not send a device_hello
	// (desktop/mobile builds older than multi-device) as one unlisted legacy
	// device on the fixed legacy route. Default FALSE (decided 2026-10-15,
	// ADR-0031 D9): only devices that prove a device identity connect.
	//
	// WARNING — setting this to true switches device security off for that
	// route: anyone holding the relay host secret and this daemon's public key
	// (a leaked pairing link, a stolen paired device, the relay operator) can
	// connect and is served WITHOUT approval, and `smind relay devices revoke`,
	// the pending state and the pairing window do not apply to it. Enable it
	// only as a stop-gap until your desktop/mobile apps are updated; the daemon
	// logs a warning at start and for every legacy connection.
	//
	// With a daemon password set (server.passwordHash / SMIND_PASSWORD) legacy
	// service is refused regardless: a client without device identity cannot
	// carry the password (ADR-0031 P13c).
	LegacyDevices bool `yaml:"legacyDevices"`
	// MaxDevices caps how many paired devices may be connected at once.
	// Default 16; Load rejects values < 1.
	MaxDevices int `yaml:"maxDevices"`
}

// DefaultRelayMaxDevices is relay.maxDevices when unset.
const DefaultRelayMaxDevices = 16

// AccountUsageConfig is the user-facing surface of ADR-0028's usage
// windows. Only these five knobs are configurable; the call-budget
// constants (30 s floor, 6/hour, 2 in flight, 5 min Retry-After default,
// 6 h cap, 1->30 min backoff) are fixed.
type AccountUsageConfig struct {
	// Enabled is the kill switch for ALL outbound usage calls. Default true.
	Enabled bool `yaml:"enabled"`
	// TTLSeconds is how long a usage snapshot stays fresh; default 300,
	// minimum 300 (may only raise).
	TTLSeconds int `yaml:"ttlSeconds"`
	// Routing toggles exhaustion-aware routing on the windows. Default true.
	Routing bool `yaml:"routing"`
	// ExhaustThresholdPct is the usage percent at/above which an account
	// counts as exhausted; default 100, valid 1..100.
	ExhaustThresholdPct int `yaml:"exhaustThresholdPct"`
	// Passive switches the passive observer/cooldown (ADR-0028 §7) on.
	// Default true; enabled:false also disables it.
	Passive bool `yaml:"passive"`
}

// MinAccountUsageTTLSeconds is the lowest accepted accountUsage.ttlSeconds.
const MinAccountUsageTTLSeconds = 300

// WorktreesConfig holds daemon-wide knobs for task worktree setup/teardown
// scripts and per-task ports (ADR-0024).
type WorktreesConfig struct {
	Ports PortsConfig `yaml:"ports"`
	// SetupTimeoutSeconds bounds a setup script (default 600).
	SetupTimeoutSeconds int `yaml:"setupTimeoutSeconds"`
	// TeardownTimeoutSeconds bounds a teardown script (default 120).
	TeardownTimeoutSeconds int `yaml:"teardownTimeoutSeconds"`
}

// PortsConfig configures per-task port allocation.
type PortsConfig struct {
	// Range is "lo-hi" within 1024-65535; empty disables port allocation.
	Range string `yaml:"range"`
}

// PortRange parses Ports.Range. ok is false when no range is configured.
func (p PortsConfig) PortRange() (lo, hi int, ok bool, err error) {
	if p.Range == "" {
		return 0, 0, false, nil
	}
	if _, err := fmt.Sscanf(p.Range, "%d-%d", &lo, &hi); err != nil {
		return 0, 0, false, fmt.Errorf("worktrees.ports.range %q: want \"lo-hi\"", p.Range)
	}
	if lo < 1024 || hi > 65535 || lo > hi {
		return 0, 0, false, fmt.Errorf("worktrees.ports.range %q: need 1024 <= lo <= hi <= 65535", p.Range)
	}
	return lo, hi, true, nil
}

// AgentsConfig holds per-agent tuning.
type AgentsConfig struct {
	GLM GLMAgentConfig `yaml:"glm"`
}

// GLMAgentConfig tunes the GLM ACP agent.
type GLMAgentConfig struct {
	// MaxTurns, when set, is passed to the agent as --max-turns <n> so one
	// long prompt cannot exhaust its output budget. Unset (nil) keeps the
	// agent's own default; Load rejects values outside [1, 10000].
	MaxTurns *int `yaml:"maxTurns"`
}

// MaxGLMTurns is the upper bound accepted for agents.glm.maxTurns.
const MaxGLMTurns = 10000

// ServerConfig controls the HTTP server (ADR-0031 D8).
type ServerConfig struct {
	// Port defaults to 4648. It is the port of the default listen address;
	// Listen, when set, wins (the two may not disagree).
	Port int `yaml:"port"`
	// Listen is "host:port" to bind. Empty means 127.0.0.1:<Port>. A
	// non-loopback host (0.0.0.0, a LAN or Tailscale IP, a name) is only
	// accepted together with a password: the daemon refuses to start
	// otherwise. Env override: SMIND_LISTEN.
	Listen string `yaml:"listen"`
	// Hostnames are extra Host header names accepted besides localhost,
	// *.localhost and literal IPs. A leading "." matches the domain and its
	// subdomains (".example.com"); otherwise the match is exact. Env
	// override: SMIND_HOSTNAMES (comma separated).
	Hostnames []string `yaml:"hostnames"`
	// PasswordHash is a bcrypt hash written by `smind set-password`; never a
	// clear password. Non-empty means remote clients must log in.
	PasswordHash string `yaml:"passwordHash"`
	// PasswordPlain comes only from SMIND_PASSWORD (for containers) and is
	// hashed in memory at daemon start. Never read from or written to
	// config.yaml.
	PasswordPlain string `yaml:"-"`
}

// DefaultPort is the daemon's default HTTP port.
const DefaultPort = 4648

// Env names that override the server section (ADR-0031 D8, for containers).
const (
	EnvListen    = "SMIND_LISTEN"
	EnvHostnames = "SMIND_HOSTNAMES"
	EnvPassword  = "SMIND_PASSWORD"
)

// ListenHostPort is the effective bind address split into host and port:
// Listen when set, else 127.0.0.1 and Port (default 4648). An error means
// the configuration is invalid (bad address, or port and listen disagree).
func (s ServerConfig) ListenHostPort() (host string, port int, err error) {
	port = s.Port
	if port == 0 {
		port = DefaultPort
	}
	if s.Listen == "" {
		if port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("server.port %d is not a valid port", port)
		}
		return "127.0.0.1", port, nil
	}
	h, p, err := net.SplitHostPort(s.Listen)
	if err != nil {
		return "", 0, fmt.Errorf("server.listen %q: want host:port (for example 127.0.0.1:4648 or 0.0.0.0:4648): %w", s.Listen, err)
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("server.listen %q: %q is not a valid port", s.Listen, p)
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); h != "" && ip == nil {
		if err := ValidateHostname(h); err != nil || strings.HasPrefix(h, ".") {
			return "", 0, fmt.Errorf("server.listen %q: %q is neither an IP address nor a hostname", s.Listen, h)
		}
	}
	if s.Port != 0 && s.Port != n && s.Port != DefaultPort {
		return "", 0, fmt.Errorf("server.port (%d) and server.listen (%s) name different ports; set one of them", s.Port, s.Listen)
	}
	return h, n, nil
}

// ClientPort is the port local CLI clients (stop, update, every RPC command)
// dial on 127.0.0.1: the daemon always listens on loopback too (see
// LoopbackCompanion), whatever the configured address. Unusable
// configuration falls back to the default port so CLI commands still run.
func (s ServerConfig) ClientPort() int {
	if _, port, err := s.ListenHostPort(); err == nil {
		return port
	}
	return DefaultPort
}

// ListenIsLoopback reports whether the configured bind address is loopback
// only (an IP in 127.0.0.0/8 or ::1, or the name "localhost"). An empty host
// (":4648"), 0.0.0.0, :: and every other name or IP are non-loopback.
func (s ServerConfig) ListenIsLoopback() bool {
	host, _, err := s.ListenHostPort()
	if err != nil {
		return false
	}
	return IsLoopbackHost(host)
}

// IsLoopbackHost reports whether host is "localhost" or a loopback IP.
func IsLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// LoopbackCompanion returns the extra loopback address the daemon must also
// bind so local clients (CLI, agent CLIs using the proxy, the desktop app)
// keep working: they always dial 127.0.0.1:<port>. "" when the configured
// address already covers it (127.0.0.1, "localhost", the 0.0.0.0 / :: / empty
// host wildcards); otherwise 127.0.0.1:<port> (a specific LAN or Tailscale
// IP or name, or another loopback address such as [::1] or 127.0.0.2).
func (s ServerConfig) LoopbackCompanion() string {
	host, port, err := s.ListenHostPort()
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); host == "" || strings.EqualFold(host, "localhost") ||
		(ip != nil && (ip.IsUnspecified() || ip.Equal(net.IPv4(127, 0, 0, 1)))) {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// Bind is one listener to open.
type Bind struct{ Network, Addr string }

// Binds lists every listener the daemon opens: the configured address plus the
// loopback companion. IPv4 literals bind IPv4 only ("tcp4"): Go's "tcp" on
// 0.0.0.0 is dual-stack and would also expose the host's global IPv6
// addresses, which is not what "0.0.0.0" says. Say "[::]:port" (or ":port")
// to listen on both families. "localhost" binds 127.0.0.1.
func (s ServerConfig) Binds() ([]Bind, error) {
	host, port, err := s.ListenHostPort()
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	binds := []Bind{{Network: listenNetwork(host), Addr: net.JoinHostPort(host, strconv.Itoa(port))}}
	if c := s.LoopbackCompanion(); c != "" {
		binds = append(binds, Bind{Network: "tcp4", Addr: c})
	}
	return binds, nil
}

func listenNetwork(host string) string {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	switch {
	case ip == nil, ip.IsUnspecified() && ip.To4() == nil: // names, "", "::" : both families
		return "tcp"
	case ip.To4() != nil:
		return "tcp4"
	default:
		return "tcp6"
	}
}

// HasPassword reports whether remote login is configured, by hash or by
// SMIND_PASSWORD.
func (s ServerConfig) HasPassword() bool {
	return s.PasswordHash != "" || s.PasswordPlain != ""
}

// validateServer checks the server section; used by Load and by the config
// editor before it writes.
func validateServer(s ServerConfig) error {
	if _, _, err := s.ListenHostPort(); err != nil {
		return err
	}
	for _, h := range s.Hostnames {
		if err := ValidateHostname(h); err != nil {
			return fmt.Errorf("server.hostnames: %w", err)
		}
	}
	return nil
}

// ValidateHostname accepts a Host allowlist entry: letters, digits, dots and
// hyphens, with one optional leading "." (domain and subdomains). No ports,
// schemes, slashes, wildcards or spaces: an entry that can never match is a
// typo worth failing loudly.
func ValidateHostname(h string) error {
	if h == "" {
		return fmt.Errorf("empty hostname")
	}
	name := strings.TrimPrefix(h, ".")
	if name == "" || len(name) > 253 {
		return fmt.Errorf("%q is not a hostname", h)
	}
	if strings.HasPrefix(h, ".") && !strings.Contains(strings.TrimSuffix(name, "."), ".") {
		// ".com" would switch the DNS-rebinding defence off for a whole TLD.
		return fmt.Errorf("%q matches every name under %q; list a specific domain such as .example.com", h, name)
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%q is not a hostname", h)
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("%q is not a hostname (letters, digits, dots and hyphens only; no port, scheme or wildcard)", h)
			}
		}
	}
	return nil
}

// OrchestrationConfig guards agent-driven task orchestration (a task
// spawning child tasks of its own, e.g. via the MCP task_new tool).
type OrchestrationConfig struct {
	// MaxDepth caps how many ancestors a task may have: a root task is at
	// depth 0, so the default (2) allows a root task's children and
	// grandchildren but rejects a great-grandchild. Enforced by
	// internal/workspace.Manager.CreateTask.
	MaxDepth int `yaml:"maxDepth"`
	// MaxConcurrentRunsPerTree caps how many runs may be StatusRunning at
	// once across one task tree (a root task and all its descendants).
	// Default 4; Load rejects values < 1.
	MaxConcurrentRunsPerTree int `yaml:"maxConcurrentRunsPerTree"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Server:        ServerConfig{Port: 4648},
		Orchestration: OrchestrationConfig{MaxDepth: 2, MaxConcurrentRunsPerTree: 4},
		Worktrees: WorktreesConfig{
			SetupTimeoutSeconds:    600,
			TeardownTimeoutSeconds: 120,
		},
		AccountUsage: AccountUsageConfig{
			Enabled:             true,
			TTLSeconds:          MinAccountUsageTTLSeconds,
			Routing:             true,
			ExhaustThresholdPct: 100,
			Passive:             true,
		},
		Relay:    RelayConfig{LegacyDevices: false, MaxDevices: DefaultRelayMaxDevices},
		Learning: DefaultLearning(),
		Log:      LogConfig{MaxSizeMB: DefaultLogMaxSizeMB, MaxFiles: DefaultLogMaxFiles},
	}
}

// Load reads the config file, falling back to defaults for missing keys.
// A missing file is not an error.
func Load() (Config, error) {
	return LoadFile(Path())
}

// LoadFile reads the config at path like Load does. `smind service` uses it
// to read the config of the user the service runs as, which is not always the
// user running the command.
func LoadFile(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		// The environment still applies: a container's SMIND_LISTEN and
		// SMIND_PASSWORD must not vanish because the file cannot be read.
		applyEnv(&cfg)
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			// Same for a typo in the file.
			applyEnv(&cfg)
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if err := validateServer(cfg.Server); err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if _, _, _, err := cfg.Worktrees.Ports.PortRange(); err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	if n := cfg.Orchestration.MaxConcurrentRunsPerTree; n < 1 {
		return cfg, fmt.Errorf("config %s: orchestration.maxConcurrentRunsPerTree must be >= 1, got %d", path, n)
	}
	if ttl := cfg.AccountUsage.TTLSeconds; ttl < MinAccountUsageTTLSeconds {
		return cfg, fmt.Errorf("config %s: accountUsage.ttlSeconds must be >= %d, got %d", path, MinAccountUsageTTLSeconds, ttl)
	}
	if p := cfg.AccountUsage.ExhaustThresholdPct; p < 1 || p > 100 {
		return cfg, fmt.Errorf("config %s: accountUsage.exhaustThresholdPct must be between 1 and 100, got %d", path, p)
	}
	if n := cfg.Relay.MaxDevices; n < 1 {
		return cfg, fmt.Errorf("config %s: relay.maxDevices must be >= 1, got %d", path, n)
	}
	if n := cfg.Log.MaxSizeMB; n < 1 {
		return cfg, fmt.Errorf("config %s: log.maxSizeMB must be >= 1, got %d", path, n)
	}
	if n := cfg.Log.MaxFiles; n < 1 {
		return cfg, fmt.Errorf("config %s: log.maxFiles must be >= 1, got %d", path, n)
	}
	if mt := cfg.Agents.GLM.MaxTurns; mt != nil && (*mt < 1 || *mt > MaxGLMTurns) {
		return cfg, fmt.Errorf("config %s: agents.glm.maxTurns must be between 1 and %d, got %d", path, MaxGLMTurns, *mt)
	}
	if err := cfg.Learning.Validate(); err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// applyEnv applies every env override over the file. It runs on every exit
// path of LoadFile (missing, unreadable or malformed file included): a
// container's kill switch must not vanish because there is no config.yaml.
func applyEnv(cfg *Config) {
	applyServerEnv(&cfg.Server)
	if v := strings.TrimSpace(os.Getenv(EnvLearning)); v != "" {
		switch strings.ToLower(v) {
		case "0", "false", "off":
			cfg.Learning.Enabled = false
		}
	}
}

// applyServerEnv applies SMIND_LISTEN, SMIND_HOSTNAMES and SMIND_PASSWORD over
// the file (ADR-0031 D8: containers configure the daemon through env).
func applyServerEnv(s *ServerConfig) {
	if v := strings.TrimSpace(os.Getenv(EnvListen)); v != "" {
		s.Listen = v
	}
	if v := os.Getenv(EnvHostnames); strings.TrimSpace(v) != "" {
		s.Hostnames = nil
		for _, h := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
			s.Hostnames = append(s.Hostnames, strings.ToLower(h))
		}
	}
	if v := os.Getenv(EnvPassword); v != "" {
		s.PasswordPlain = v
	}
}
