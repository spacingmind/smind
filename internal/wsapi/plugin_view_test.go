package wsapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins"
)

// viewHelperFlag turns the test binary into a plugin whose rpc.view answers a
// text node holding the params it received (hosttest has no mode that returns
// a view document).
const viewHelperFlag = "--view-helper"

func init() {
	if len(os.Args) < 2 || os.Args[1] != viewHelperFlag {
		return
	}
	rd := bufio.NewReader(os.Stdin)
	for {
		var n int
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, "content-length") {
				n, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(rd, body); err != nil {
			os.Exit(0)
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &m)
		var result any = map[string]any{}
		switch m.Method {
		case "plugin.shutdown":
			os.Exit(0)
		case "rpc.view":
			result = map[string]any{"ui": 1, "root": map[string]any{"type": "text", "text": string(m.Params)}}
		}
		if len(m.ID) == 0 {
			continue
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(out), out)
	}
}

// installViewPlugin installs and enables a plugin with a file panel "notes"
// (ui 1) and an rpc panel "live" (ui 1), backed by the view helper.
func (e *procEnv) installViewPlugin(t *testing.T, id string) {
	t.Helper()
	cmd, _ := json.Marshal([]string{os.Args[0], viewHelperFlag})
	man := `{"id":"` + id + `","name":"P","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},"process":{"command":` + string(cmd) + `},` +
		`"contributes":{"rpc":[{"name":"view"}],"panels":[` +
		`{"id":"notes","title":"Notes","where":"sidebar","view":{"file":"notes.json"}},` +
		`{"id":"live","title":"Live","where":"task","view":{"rpc":"view"}}]}}`
	dir := t.TempDir()
	for name, body := range map[string]string{"smind-plugin.json": man, "notes.json": `{"ui":1,"root":{"type":"text","text":"Notes"}}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": dir})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": id, "confirm": res.(plugins.Info).Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	e.waitRunning(t, id)
}

func (e *procEnv) pview(rc *requestContext, id, panel string, ui int, ctx any) (map[string]any, error) {
	res, err := e.call("plugin.view", rc, map[string]any{"pluginId": id, "panelId": panel, "ui": ui, "context": ctx})
	if err != nil {
		return nil, err
	}
	return res.(map[string]any), nil
}

func TestPluginView_File_And_RPC(t *testing.T) {
	e := newProcEnv(t)
	e.installViewPlugin(t, "alpha")
	res, err := e.pview(localRC(), "alpha", "notes", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["pluginId"] != "alpha" || res["panelId"] != "notes" || string(res["view"].(json.RawMessage)) != `{"root":{"text":"Notes","type":"text"},"ui":1}` {
		t.Fatalf("file view: %v", res)
	}
	res, err = e.pview(localRC(), "alpha", "live", 1, map[string]any{"task": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if v := string(res["view"].(json.RawMessage)); !strings.Contains(v, `panelId`) || !strings.Contains(v, `t1`) {
		t.Fatalf("rpc view did not carry panelId and context: %s", v)
	}
	if _, err := e.call("plugin.view", localRC(), map[string]any{"pluginId": "alpha"}); err == nil {
		t.Fatal("a missing panelId must be refused")
	}
}

func TestPluginView_UiLevelTooLow_Typed(t *testing.T) {
	e := newProcEnv(t)
	e.installViewPlugin(t, "alpha")
	_, err := e.pview(localRC(), "alpha", "notes", 0, nil)
	wantCallCode(t, err, "ui_level_too_low")
	_, err = e.pview(localRC(), "alpha", "ghost", 1, nil)
	wantCallCode(t, err, plugins.CodeUnknownPanel)
}

func TestPluginView_RunScopedRefused(t *testing.T) {
	e := newProcEnv(t)
	e.installViewPlugin(t, "alpha")
	if _, err := e.pview(agentRC(), "alpha", "notes", 1, nil); err == nil || !strings.Contains(err.Error(), "not available to agents") {
		t.Fatalf("err = %v, want a refusal for agents", err)
	}
	if _, err := e.api.hs["plugin.view"](context.Background(), nil, json.RawMessage(`{"pluginId":"alpha","panelId":"notes","ui":1}`)); err == nil {
		t.Fatal("a nil request context must fail closed")
	}
}

func TestPluginView_RelaySessionAllowed(t *testing.T) {
	e := newProcEnv(t)
	e.installViewPlugin(t, "alpha")
	if _, err := e.pview(relayRC(), "alpha", "live", 1, nil); err != nil {
		t.Fatalf("a relay session must read views: %v", err)
	}
}
