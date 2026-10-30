package acp

// KimiCommand returns the command to spawn Moonshot AI's Kimi Code CLI in ACP
// mode (see https://github.com/MoonshotAI/kimi-code), for use with New.
// Unlike GLMCommand, this is not self-installing via npx: it requires the
// vendor's install script (`curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash`)
// and a one-time interactive `kimi` -> `/login` done out of band first (per
// the CLI's own README) -- `kimi acp` itself expects an already-authenticated
// install, it does not perform its own login flow. The older Python
// `kimi-cli` (MoonshotAI/kimi-cli) is archived and not what this targets.
func KimiCommand() []string {
	return []string{"kimi", "acp"}
}
