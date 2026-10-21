package arm

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"
)

// tarOf returns a one-file tar stream whose entry has the given raw name.
func tarOf(t *testing.T, name, body string) io.Reader {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	return &b
}
