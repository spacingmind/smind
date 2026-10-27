package qaframes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// SeedCall is one RPC of a seed file.
type SeedCall struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// SeedDaemon runs the seed file's RPCs, in order, against the isolated daemon
// at base (http://host:port) using the token in home. Any RPC error stops it.
func SeedDaemon(ctx context.Context, base, home, seedFile string) error {
	raw, err := os.ReadFile(seedFile)
	if err != nil {
		return err
	}
	var calls []SeedCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return fmt.Errorf("%s: %w", seedFile, err)
	}
	tok, err := os.ReadFile(filepath.Join(home, "token"))
	if err != nil {
		return err
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	ws := "ws://" + u.Host + "/ws?token=" + url.QueryEscape(strings.TrimSpace(string(tok)))
	c, _, err := websocket.DefaultDialer.DialContext(ctx, ws, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	for i, call := range calls {
		id := fmt.Sprint(i + 1)
		if err := c.WriteJSON(map[string]any{"id": id, "method": call.Method, "params": call.Params}); err != nil {
			return err
		}
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
		for {
			var m struct {
				ID    string          `json:"id"`
				Error json.RawMessage `json:"error"`
			}
			if err := c.ReadJSON(&m); err != nil {
				return fmt.Errorf("%s: %w", call.Method, err)
			}
			if m.ID != id {
				continue // an event, not our reply
			}
			if len(m.Error) > 0 && string(m.Error) != "null" {
				return fmt.Errorf("%s: %s", call.Method, m.Error)
			}
			break
		}
	}
	return nil
}
