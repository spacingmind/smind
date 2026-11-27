package wire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// Test_config_set_not_served_on_the_fleet_link pins ADR-0062: no config.* name
// exists in the fleet wire namespace, and every method name the package
// declares is a fleet.* read method.
func Test_config_set_not_served_on_the_fleet_link(t *testing.T) {
	for _, m := range ReadMethods {
		if strings.HasPrefix(m, "config.") {
			t.Errorf("ReadMethods names %s", m)
		}
		if !strings.HasPrefix(m, "fleet.") {
			t.Errorf("ReadMethods holds a non-fleet name %s", m)
		}
	}
	// Every string constant named Method* in wire.go is checked too, so a
	// method added without joining ReadMethods cannot carry a config.* name.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "wire.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if !strings.HasPrefix(name.Name, "Method") || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v, _ := strconv.Unquote(lit.Value)
			seen++
			if strings.HasPrefix(v, "config.") {
				t.Errorf("%s = %q is a config.* name on the fleet link", name.Name, v)
			}
			if !strings.HasPrefix(v, "fleet.") {
				t.Errorf("%s = %q is outside the fleet. namespace", name.Name, v)
			}
		}
		return true
	})
	if seen != len(ReadMethods) {
		t.Errorf("found %d Method* constants, ReadMethods has %d", seen, len(ReadMethods))
	}
}
