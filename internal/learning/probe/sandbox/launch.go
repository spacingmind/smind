package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// tools is the arm's complete built-in tool surface; the CLI's init report
// must list exactly these (see Verify and CheckInit).
var tools = []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "NotebookEdit", "Skill"}

// AllowedTools returns a copy of the arm's tool allowlist.
func AllowedTools() []string { return append([]string(nil), tools...) }

// Config is what Prepare needs to build one arm's launch.
type Config struct {
	ArmDir        string // the arm's cwd and only writable dir
	ProjectionDir string // may be "" (canary, arm A with no skills): then no --add-dir and no allow/deny entry for it
	// PrivateDir is a 0700 dir OUTSIDE the arm, caller-owned, for the launcher
	// and pidfile: the arm's Bash could rewrite them otherwise.
	PrivateDir string
	RealCLI    string                      // absolute path to claude (the caller resolves it)
	LookupEnv  func(string) (string, bool) // nil = os.LookupEnv
}

// Launch is a validated, ready-to-use arm launch. It holds no env value
// except through the launcher file; its String form is value-free.
type Launch struct {
	ArmDir, ProjectionDir string
	CLIPath, PidFile      string
	SettingsJSON          string
	Tools                 []string
	Policy                *Policy

	// prepared is set only by Prepare: a Launch built any other way (a
	// literal, a copy of a zero value) cannot produce Options and is
	// rejected by every consumer.
	prepared bool
}

// Prepared reports whether l came from Prepare.
func (l *Launch) Prepared() bool { return l != nil && l.prepared }

// String omits the settings and paths' contents beyond the identifiers
// needed to debug: never an env value.
func (l *Launch) String() string {
	return fmt.Sprintf("sandbox.Launch{arm=%s cli=%s}", l.ArmDir, l.CLIPath)
}

// Prepare validates cfg, writes the launcher and builds the settings. It
// fails closed: any problem is an error, never a weaker launch.
func Prepare(cfg Config) (*Launch, error) {
	if !Supported() {
		return nil, ErrUnsupported
	}
	for name, p := range map[string]string{"arm dir": cfg.ArmDir, "private dir": cfg.PrivateDir, "real CLI": cfg.RealCLI} {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("sandbox: %s must be absolute", name)
		}
	}
	arm, err := canonPath(cfg.ArmDir)
	if err != nil {
		return nil, fmt.Errorf("arm dir: %w", err)
	}
	priv, err := canonPath(cfg.PrivateDir)
	if err != nil {
		return nil, fmt.Errorf("private dir: %w", err)
	}
	if nested(arm, priv) || nested(priv, arm) {
		return nil, errors.New("sandbox: the arm dir and the private dir must not be nested in each other")
	}
	proj := ""
	if cfg.ProjectionDir != "" {
		if proj, err = canonPath(cfg.ProjectionDir); err != nil {
			return nil, fmt.Errorf("projection dir: %w", err)
		}
		if nested(priv, proj) || nested(proj, priv) {
			return nil, errors.New("sandbox: the projection dir and the private dir must not be nested in each other")
		}
	}
	settings, err := Settings(arm, proj)
	if err != nil {
		return nil, err
	}
	env := AllowlistEnv(cfg.LookupEnv)
	pidFile := filepath.Join(priv, "claude.pid")
	path, err := WriteLauncher(priv, cfg.RealCLI, env, pidFile)
	if err != nil {
		return nil, err
	}
	policy := &Policy{ArmDir: arm}
	if proj != "" {
		policy.ReadOnlyDirs = []string{proj}
	}
	return &Launch{ArmDir: arm, ProjectionDir: proj, CLIPath: path, PidFile: pidFile, SettingsJSON: settings,
		Tools: AllowedTools(), Policy: policy, prepared: true}, nil
}

// nested reports whether child is parent or lies inside it.
func nested(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Options returns the claudecode options of the one CLI launch shape. Callers
// add model, max turns and budget themselves. There is deliberately no
// permission prompt tool and no bypass flag: P0 measured the policy is never
// consulted headless, so the settings sandbox is the boundary and the policy
// is belt and braces. A Launch that did not come from Prepare yields nil.
// Slash commands stay enabled: --disable-slash-commands would also remove
// the Skill tool (measured), and a probe needs the arm to load a candidate
// skill. WithEnv is called once: the SDK replaces the list on every call.
func (l *Launch) Options() []claudecode.Option {
	if !l.Prepared() {
		return nil
	}
	none := "none"
	opts := []claudecode.Option{
		claudecode.WithSettingSources(),
		claudecode.WithStrictMCPConfig(),
		claudecode.WithExtraArgs(map[string]*string{"permission-prompts": &none}),
		claudecode.WithPermissionMode("acceptEdits"),
		claudecode.WithPermissionPolicy(l.Policy),
		claudecode.WithTools(l.Tools...),
		claudecode.WithDisallowedTools("WebFetch", "WebSearch"),
		claudecode.WithSettings(l.SettingsJSON),
		claudecode.WithCLIPath(l.CLIPath),
		claudecode.WithEnv("ENABLE_CLAUDEAI_MCP_SERVERS=false"),
	}
	if l.ProjectionDir != "" {
		// The projection is an add-dir (the built-in Read needs it to read a
		// skill file) and a plugin root: measured with --setting-sources= a
		// skill under --add-dir is not discovered, a --plugin-dir plugin is.
		opts = append(opts, claudecode.WithAddDirs(l.ProjectionDir), claudecode.WithPluginDirs(l.ProjectionDir))
	}
	return opts
}
