package steward

import (
	"strings"
	"testing"
)

func TestDigest_MatrixMissingNeverNull(t *testing.T) {
	in := base()
	in.Numstat = "1\t1\tweb/packages/ui/src/x.css\n"
	in.WriteScope = []string{"internal/steward/", "web/"}
	in.Frames = framesAt(in.Task.HeadSHA) // all four matrix cells covered

	data, _, err := Build(in).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"matrixMissing":[]`) {
		t.Errorf("digest JSON lacks \"matrixMissing\":[] for full coverage: %s", data)
	}
}
