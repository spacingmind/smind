// Command treeagent is a stand-in "agent" for the Windows process-tree
// kill tests: it spawns a long-running grandchild (ping), reports both
// pids as JSON to the path in SMIND_TREEAGENT_OUT, and stays alive until
// that grandchild dies -- so a test can assert the whole tree went down
// together.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
)

func main() {
	child := exec.Command("ping", "-n", "300", "127.0.0.1")
	if err := child.Start(); err != nil {
		os.Exit(1)
	}
	if out := os.Getenv("SMIND_TREEAGENT_OUT"); out != "" {
		b, _ := json.Marshal(map[string]int{"pid": os.Getpid(), "child": child.Process.Pid})
		if err := writeFileAtomic(out, b); err != nil {
			os.Exit(1)
		}
	}
	_ = child.Wait()
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
