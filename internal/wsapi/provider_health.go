package wsapi

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/providers"
)

// providerVersionTimeout bounds the single `--version` run the health check
// makes: enough for a CLI to print its version, short enough that a hanging
// binary can't hang provider.test.
const providerVersionTimeout = 3 * time.Second

// versionToken matches the numeric version at the start of one whitespace
// token: all of "1.2.3", or the "2025.09.18" inside "2025.09.18-7ae6800".
var versionToken = regexp.MustCompile(`^(\d+(?:\.\d+)*)`)

// testProviderSpec runs provider.test's health check for a spec: the PATH
// lookup testProviderCLI does, plus what spec.Health says beyond it -- the
// install hint when the binary is missing, and for a found binary its
// version (when Health.VersionArg makes it print one, gated by
// Health.MinVersion) and the login/unverified notes. No model call, no
// shell: the only thing it ever executes is the found binary with
// VersionArg, and never when VersionArg is empty.
func testProviderSpec(spec providers.Spec) providerTestResult {
	res := testProviderCLI(spec.HealthCommand)
	if spec.Plugin != "" {
		return testPluginProviderSpec(spec, res)
	}
	if !res.OK {
		if spec.Health.Install != "" {
			res.Detail += "; install: " + spec.Health.Install
		}
		return res
	}
	if spec.Health.VersionArg != "" {
		if v := runVersionCommand(res.path, spec.Health.VersionArg); v != "" {
			if spec.Health.MinVersion != "" && compareVersions(v, spec.Health.MinVersion) < 0 {
				res.OK = false
				res.Detail = fmt.Sprintf("found %s, version %s is too old: needs >= %s (update: %s)", res.path, v, spec.Health.MinVersion, spec.Health.Install)
			} else {
				res.Detail = fmt.Sprintf("found %s (%s)", res.path, v)
			}
		}
		// A failing or version-less version command stays best effort: the
		// binary was found, which is all that was promised.
	}
	if spec.Health.Login != "" {
		res.Detail += "; login: " + spec.Health.Login
	}
	if spec.Health.Unverified {
		res.Detail += "; not verified against a real login"
	}
	return res
}

// testPluginProviderSpec is the health check of a plugin provider: it NEVER
// executes the plugin's command. A version probe would run plugin-controlled
// code with the daemon's whole environment; the scrubbed, confined launch that
// may do that arrives with PD2 (docs/plans/active/plugins-wave-d.md, AD18).
// Until then it reports only whether argv[0] resolves (a stat), the install
// and login hints, and that the version check is not available yet.
func testPluginProviderSpec(spec providers.Spec, res providerTestResult) providerTestResult {
	if !res.OK {
		if spec.Health.Install != "" {
			res.Detail += "; install: " + spec.Health.Install
		}
		return res
	}
	if spec.Health.VersionArg != "" {
		res.Detail += "; version check is not available for plugin providers yet"
	}
	if spec.Health.Login != "" {
		res.Detail += "; login: " + spec.Health.Login
	}
	return res
}

// runVersionCommand executes path with arg (no shell) under
// providerVersionTimeout and returns the version it printed, "" when the
// command failed, timed out or printed nothing parseable.
func runVersionCommand(path, arg string) string {
	ctx, cancel := context.WithTimeout(context.Background(), providerVersionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, arg).CombinedOutput()
	if err != nil {
		return ""
	}
	v, ok := parseVersion(string(out))
	if !ok {
		return ""
	}
	return v
}

// parseVersion returns the version in out: the first whitespace token that
// starts with one, e.g. "1.0.0" in "tool 1.0.0 (2026.10.30)" or "2025.09.18"
// in "2025.09.18-7ae6800". Leading zeros are kept ("09" stays "09");
// compareVersions is what compares them numerically.
func parseVersion(out string) (string, bool) {
	for _, tok := range strings.Fields(out) {
		if m := versionToken.FindStringSubmatch(tok); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// compareVersions compares dotted versions segment by segment, numerically,
// a missing segment counting as 0: -1, 0 or 1 as a is lower, equal or higher
// than b ("2025.09.18" < "2026.01.01", "1.2" == "1.2.0", "1.10" > "1.9").
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := versionSegment(as, i), versionSegment(bs, i)
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionSegment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}
