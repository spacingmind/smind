package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/acp"
)

// Errors of ReplayACPSession.
var (
	// ErrACPNoLoadSession: the agent did not advertise loadSession, so session/load
	// was not called (the protocol forbids it).
	ErrACPNoLoadSession = errors.New("taskrunner: the agent does not advertise loadSession")
	// ErrACPSessionUnknown: the agent answered session/load with an error.
	ErrACPSessionUnknown = errors.New("taskrunner: the agent could not load the session")
	// ErrACPReplayTooLarge: the replay passed the byte bound.
	ErrACPReplayTooLarge = errors.New("taskrunner: the replayed history is over the size bound")
)

// ReplayACPSession starts provider's ACP agent the way a run does (scrubbed
// provider env, plugin confinement, no MCP servers, permission requests denied),
// calls session/load for sessionID with cwd, and returns every event the agent
// replays: the whole session, all turns, in order (ADR-0053 D4). It sends no
// prompt and sets no mode or option. The process is closed before it returns,
// whether the load finished, failed, outran maxBytes or ctx ended.
func (r *Runner) ReplayACPSession(ctx context.Context, provider Provider, sessionID, cwd string, maxBytes int64) ([]Event, error) {
	command, ok := r.acpCommandFor(provider)
	if !ok {
		return nil, fmt.Errorf("taskrunner: no ACP command configured for provider %q", provider)
	}
	popts := []acp.Option{acp.WithPermissionPolicy(acp.AutoDenyPolicy{})}
	spec, haveSpec := r.providerRegistry().Get(string(provider))
	if haveSpec && spec.NoMCPServers {
		popts = append(popts, acp.WithoutMcpServers())
	}
	if haveSpec && spec.Plugin != "" {
		pcmd, pextra, err := r.pluginLaunch(spec, cwd)
		if err != nil {
			return nil, fmt.Errorf("taskrunner: %w", err)
		}
		command = pcmd
		popts = append(popts, pextra...)
		popts = append(popts, acp.WithEnv("NO_BROWSER=true"))
	} else {
		env, err := r.launchEnvEntries(provider)
		if err != nil {
			return nil, fmt.Errorf("taskrunner: %w", err)
		}
		env = append([]string{"NO_BROWSER=true"}, env...)
		popts = append(popts, acp.WithEnv(env...))
	}
	secrets := r.providerSecretValues(provider)
	client, err := r.newACPClient(command, popts...)
	if err != nil {
		return nil, scrubError(err, secrets)
	}
	defer client.Close()
	if err := client.Initialize(ctx); err != nil {
		return nil, scrubError(err, secrets)
	}
	if !client.SupportsLoadSession() {
		return nil, ErrACPNoLoadSession
	}

	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan acp.SessionUpdate)
	type collected struct {
		events []Event
		over   bool
	}
	got := make(chan collected, 1)
	go func() {
		var c collected
		var bytes int64
		for u := range updates { // drained to the end so the client never blocks
			if c.over {
				continue
			}
			e, ok := acpEvent(u)
			if !ok {
				continue
			}
			bytes += int64(len(e.Text) + len(e.ToolTitle) + len(e.ToolInput) + len(e.ToolResult) + len(e.RawPayload) + 64)
			if bytes > maxBytes {
				c.over = true
				c.events = nil
				cancel() // stop the load: the client closes `updates` on return
				continue
			}
			c.events = append(c.events, e)
		}
		got <- c
	}()
	_, loadErr := client.LoadSessionReplay(loadCtx, sessionID, cwd, updates)
	c := <-got
	switch {
	case c.over:
		return nil, ErrACPReplayTooLarge
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case loadErr != nil:
		return nil, fmt.Errorf("%w: %s", ErrACPSessionUnknown, strings.TrimSpace(scrubError(loadErr, secrets).Error()))
	}
	return c.events, nil
}
