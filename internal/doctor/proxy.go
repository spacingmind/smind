package doctor

import (
	"strings"

	"github.com/spacingmind/smind/internal/netenv"
)

// OutboundProxy reports the proxy variables the transport reads, host only.
// A non-http *_PROXY is a warning: provider calls through it are refused.
func OutboundProxy(env Env) []Finding {
	vars := netenv.ProxyEnv(env.Getenv)
	if len(vars) == 0 {
		return nil
	}
	parts := make([]string, len(vars))
	warn := false
	for i, v := range vars {
		parts[i] = v.Name + "=" + v.Value
		if v.Name != "NO_PROXY" && v.Name != "no_proxy" && !strings.HasPrefix(v.Value, "http://") {
			warn = true
		}
	}
	f := Finding{
		ID: "network.proxy", Level: LevelInfo, Provider: "network",
		Message: "outbound proxy from the environment: " + strings.Join(parts, "; ") +
			"; used for provider and usage calls, never for loopback, the browser or fleet peers",
	}
	if warn {
		f.Level = LevelWarn
		f.Message += "; provider calls through a non-http proxy are refused"
		f.Fix = "use an http:// proxy URL, or unset the variable"
	}
	return []Finding{f}
}
