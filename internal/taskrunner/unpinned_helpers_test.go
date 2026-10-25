package taskrunner

// testClaudeModel is the explicit model the claude-native tests launch with: a
// claude launch with no model is refused (NoModelError), so a test of anything
// else about the launch names one.
const testClaudeModel = "claude-haiku-5-5"

// testModelFor is testClaudeModel for the claude provider and "" (the
// provider's own default) for every other, for helpers that loop over providers.
func testModelFor(p Provider) string {
	if p == ProviderClaudeNative {
		return testClaudeModel
	}
	return ""
}
