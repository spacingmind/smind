package window

import "encoding/json"

// ResultText flattens a provider-native tool result into plain text, exactly
// as the window renderer does.
func ResultText(raw json.RawMessage) string { return resultText(raw) }

// TouchesSecretPath reports whether a tool call with this input and title
// mentions a secret-shaped path, exactly as the window renderer decides to
// withhold its result.
func TouchesSecretPath(input json.RawMessage, title string) bool {
	return (&toolCall{input: input, title: title}).touchesSecretPath()
}
