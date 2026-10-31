package plugins

import (
	"errors"
	"strings"
	"testing"
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
