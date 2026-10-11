// Command arghelper is a minimal test target for the Windows shim tests:
// it writes its command-line arguments as JSON to the path in
// SMIND_ARGHELPER_OUT, then blocks until stdin closes, so a test can
// verify argv round-trips through a .cmd wrapper spawned by newConn.
package main

import (
	"encoding/json"
	"io"
	"os"
)

func main() {
	b, _ := json.Marshal(os.Args[1:])
	if out := os.Getenv("SMIND_ARGHELPER_OUT"); out != "" {
		if err := os.WriteFile(out, b, 0o666); err != nil {
			os.Exit(1)
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin) // block until the conn closes stdin
}
