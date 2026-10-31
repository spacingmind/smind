package plugins

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins/host"
)

func TestView_StaticFileInvalid_FailsInstall(t *testing.T) {
	man := manifestWith("demo", "1.0.0", `"contributes":{"panels":[{"id":"p","title":"P","where":"task","view":{"file":"p.json"}}]}`)
	dir := writePluginDir(t, man, map[string]string{"p.json": `{"ui":1,"root":{"type":"bogus"}}`})
	m, err := ParseManifest([]byte(man), "0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	err = m.loadContributions(dir)
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want a ManifestError", err)
	}
	if len(me.Errs) != 1 || me.Errs[0].Field != "contributes.panels[0].view.file" || !strings.Contains(me.Errs[0].Problem, "$.root") {
		t.Fatalf("errors = %+v", me.Errs)
	}
}

// viewHelperFlag turns the test binary into a plugin whose rpc.view answers a
// view document: hosttest has no mode that returns one. argv: flag, mode,
// arg. Mode "echo" answers a text node holding the params it received, mode
// "raw" answers arg verbatim, mode "big" a text node of arg bytes.
const viewHelperFlag = "--view-helper"

func init() {
	if len(os.Args) < 4 || os.Args[1] != viewHelperFlag {
		return
	}
	mode, arg := os.Args[2], os.Args[3]
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
			if mode == "echo" {
				result = map[string]any{"ui": 1, "root": map[string]any{"type": "text", "text": string(m.Params)}}
			} else if mode == "big" {
				n, _ := strconv.Atoi(arg)
				result = map[string]any{"ui": 1, "root": map[string]any{"type": "text", "text": strings.Repeat("x", n)}}
			} else {
				result = json.RawMessage(arg)
			}
		}
		if len(m.ID) == 0 {
			continue
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(out), out)
	}
}

// viewPlugin installs and enables a plugin with a file panel "notes" (ui 1)
// and an rpc panel "live" backed by the view helper in mode.
func viewPlugin(t *testing.T, e *env, id, mode, arg string) {
	t.Helper()
	cmd, _ := json.Marshal([]string{os.Args[0], viewHelperFlag, mode, arg})
	man := manifestWith(id, "1.0.0", `"process":{"command":`+string(cmd)+`},"contributes":{"rpc":[{"name":"view"}],`+
		`"panels":[{"id":"notes","title":"Notes","where":"sidebar","view":{"file":"notes.json"}},`+
		`{"id":"live","title":"Live","where":"task","view":{"rpc":"view"}}]}`)
	installEnable(t, e, writePluginDir(t, man, map[string]string{"notes.json": `{"ui":1,"root":{"type":"text","text":"Notes"}}`}), false)
	waitProc(t, e.m, id, host.StateRunning)
}

func TestView_File_And_RPC(t *testing.T) {
	e := newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "echo", "-")
	got, err := e.m.View(bg, "demo", "notes", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"root":{"text":"Notes","type":"text"},"ui":1}`; string(got) != want {
		t.Fatalf("file view = %s, want %s", got, want)
	}
	got, err = e.m.View(bg, "demo", "live", 1, json.RawMessage(`{"task":"t1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Root struct{ Text string }
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	var params struct {
		PanelID string
		Context map[string]string
	}
	if err := json.Unmarshal([]byte(doc.Root.Text), &params); err != nil || params.PanelID != "live" || params.Context["task"] != "t1" {
		t.Fatalf("the process received %q (%v)", doc.Root.Text, err)
	}
	if _, err := e.m.View(bg, "demo", "live", 1, json.RawMessage(`[1]`)); CodeOf(err) != CodeBadCall {
		t.Fatalf("array context: %v", err)
	}
}

func TestView_UnknownPanel_Typed(t *testing.T) {
	e := newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "echo", "-")
	if _, err := e.m.View(bg, "demo", "ghost", 1, nil); CodeOf(err) != CodeUnknownPanel {
		t.Fatalf("err = %v, want %s", err, CodeUnknownPanel)
	}
}

func TestView_UiLevelTooLow_Typed(t *testing.T) {
	e := newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "echo", "-")
	_, err := e.m.View(bg, "demo", "notes", 0, nil)
	if CodeOf(err) != CodeUILevelTooLow || !strings.Contains(err.Error(), "supports ui level 0; the panel needs 1") {
		t.Fatalf("err = %v, want %s", err, CodeUILevelTooLow)
	}
}

func TestView_InvalidDocument_TypedNoTruncation(t *testing.T) {
	bad := `{"ui":1,"root":{"type":"bogus"}}`
	e := newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "raw", bad)
	got, err := e.m.View(bg, "demo", "live", 1, nil)
	if CodeOf(err) != CodeViewInvalid || !strings.Contains(err.Error(), "$.root.type") || got != nil {
		t.Fatalf("got %s, err = %v", got, err)
	}

	big := `{"ui":1,"root":{"type":"text","text":"` + strings.Repeat("x", 100<<10) + `"}}`
	e = newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "big", strconv.Itoa(100<<10))
	got, err = e.m.View(bg, "demo", "live", 1, nil)
	if CodeOf(err) != CodeViewInvalid || got != nil {
		t.Fatalf("over-cap string: got %d bytes, err = %v", len(got), err)
	}
	if len(err.Error()) > 400 || strings.HasPrefix(big, err.Error()) {
		t.Fatalf("the error must be a short description, not the document: %d bytes", len(err.Error()))
	}
}

func TestView_PluginDown_Typed(t *testing.T) {
	e := newHostEnv(t, nil)
	viewPlugin(t, e, "demo", "echo", "-")
	if _, err := e.m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.View(bg, "demo", "notes", 1, nil); CodeOf(err) != CodePluginUnavailable {
		t.Fatalf("err = %v, want %s", err, CodePluginUnavailable)
	}
	if _, err := e.m.View(bg, "nobody", "notes", 1, nil); CodeOf(err) != CodePluginUnavailable {
		t.Fatalf("unknown plugin: %v", err)
	}
}
