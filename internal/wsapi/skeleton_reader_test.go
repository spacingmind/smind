package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestSkeletonBigPayloadReadsTruncatedNotWrong(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	// Distinct head and tail so a wrong cut shows up.
	result := strings.Repeat("H", 4096) + strings.Repeat("m", 12000) + strings.Repeat("T", 3904)
	if len(result) != 20000 {
		t.Fatalf("fixture is %d bytes", len(result))
	}
	if _, err := c.db.CreateRun(store.Run{ID: "big", TaskID: task.ID, Provider: "glm", Status: "done", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"type": int(taskrunner.EventTypeToolCall), "toolName": "Read", "toolStatus": "success", "toolResult": result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.AppendRunEvent("big", 0, string(data)); err != nil {
		t.Fatal(err)
	}
	if err := runs.Skeletonize(c.db, "big"); err != nil {
		t.Fatal(err)
	}

	events, err := c.db.ListRunEvents("big")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%v, err=%v", events, err)
	}
	var got struct {
		ToolResult string `json:"toolResult"`
	}
	if err := json.Unmarshal([]byte(events[0].EventData), &got); err != nil {
		t.Fatal(err)
	}
	// The excerpt tier cuts the payload's raw JSON text (quotes included), so
	// head and tail are bytes of that encoding.
	enc, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.ToolResult, "bytes omitted") ||
		!strings.HasPrefix(got.ToolResult, string(enc[:4096])) || !strings.HasSuffix(got.ToolResult, string(enc[len(enc)-4096:])) {
		t.Fatalf("result not head+marker+tail (len %d): %.80q", len(got.ToolResult), got.ToolResult)
	}
	if !events[0].Truncated {
		t.Fatal("Truncated flag not set on a cut payload")
	}

	raw, err := c.db.ListRunEventsRaw("big")
	if err != nil || len(raw) != 1 {
		t.Fatalf("raw=%v, err=%v", raw, err)
	}
	if n := len(raw[0].EventData); n >= 600 {
		t.Fatalf("raw row is %d bytes, want a short summary", n)
	}
}
