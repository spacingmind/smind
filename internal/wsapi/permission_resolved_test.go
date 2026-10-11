package wsapi

import (
	"encoding/json"
	"testing"
)

func TestPermissionResolvedIsAKnownTopic(t *testing.T) {
	t.Parallel()
	if TopicPermissionResolved != "permission.resolved" {
		t.Fatalf("TopicPermissionResolved = %q", TopicPermissionResolved)
	}
	if !knownTopics[TopicPermissionResolved] {
		t.Fatal("permission.resolved is not in knownTopics")
	}
	b, err := json.Marshal(permissionResolvedPayload{RunID: "r", TaskID: 1, ChatID: 2, RequestID: "q", Reason: "runEnded"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"runId":"r","taskId":1,"chatId":2,"requestId":"q","reason":"runEnded"}`; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}
