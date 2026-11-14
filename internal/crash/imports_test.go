package crash

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCrashImportsNoNetwork: crash-imports-no-network — the recorder stays
// free of network and process code and of every other smind package but
// fsutil, so a panic can never turn into a dial or a spawn.
func TestCrashImportsNoNetwork(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	const smindPrefix = "github.com/spacingmind/smind/"
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			switch path {
			case "net", "net/http", "os/exec":
				t.Errorf("%s imports %q", name, path)
			}
			if strings.HasPrefix(path, smindPrefix) && path != smindPrefix+"internal/fsutil" {
				t.Errorf("%s imports smind package %q", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test sources found to check")
	}
}
