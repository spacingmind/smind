package runs

import (
	"encoding/json"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// persistedEvent is the JSON shape written to run_events.event_data: exactly
// Event, minus Raw. Raw is documented on taskrunner.Event as backend-native
// debug-only data (an acp.SessionUpdate, a claudecode.Message, ...) -- not
// meaningful to persist, and not guaranteed to survive a JSON round-trip for
// every provider.
type persistedEvent struct {
	Type                taskrunner.EventType          `json:"type"`
	Text                string                        `json:"text,omitempty"`
	StopReason          string                        `json:"stopReason,omitempty"`
	PermissionRequestID string                        `json:"permissionRequestId,omitempty"`
	PermissionSummary   string                        `json:"permissionSummary,omitempty"`
	PermissionOptions   []taskrunner.PermissionOption `json:"permissionOptions,omitempty"`
	PermissionOptionID  string                        `json:"permissionOptionId,omitempty"`
}

func encodeEvent(e Event) (string, error) {
	b, err := json.Marshal(persistedEvent{
		Type:                e.Type,
		Text:                e.Text,
		StopReason:          e.StopReason,
		PermissionRequestID: e.PermissionRequestID,
		PermissionSummary:   e.PermissionSummary,
		PermissionOptions:   e.PermissionOptions,
		PermissionOptionID:  e.PermissionOptionID,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeEvent(data string) (Event, error) {
	var p persistedEvent
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return Event{}, err
	}
	return Event{
		Type:                p.Type,
		Text:                p.Text,
		StopReason:          p.StopReason,
		PermissionRequestID: p.PermissionRequestID,
		PermissionSummary:   p.PermissionSummary,
		PermissionOptions:   p.PermissionOptions,
		PermissionOptionID:  p.PermissionOptionID,
	}, nil
}
