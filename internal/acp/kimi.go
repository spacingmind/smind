package acp

// KimiCommand returns the command to spawn Moonshot AI's Kimi CLI in ACP
// mode (see https://github.com/MoonshotAI/kimi-cli), for use with New.
// Unlike GLMCommand, this is not self-installing via npx: it requires
// `pip install kimi-cli` and a one-time interactive `kimi` -> `/login` done
// out of band first (per the CLI's own README) -- `kimi acp` itself expects
// an already-authenticated install, it does not perform its own login flow.
func KimiCommand() []string {
	return []string{"kimi", "acp"}
}
