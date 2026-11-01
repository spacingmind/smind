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
		if err := writeFileAtomic(out, b); err != nil {
			os.Exit(1)
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin) // block until the conn closes stdin
}

// writeFileAtomic publishes b at path in one step (write a temp file, then
// rename it), so a test polling for path never reads it empty or half written.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
