package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newFSTestClient(t *testing.T, root string) *Client {
	t.Helper()
	return &Client{sessionCwd: map[string]string{"s1": root}}
}

func TestClient_ReadWriteTextFile_InBounds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "sub", "file.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	c := newFSTestClient(t, root)

	writeParams, _ := json.Marshal(writeTextFileParams{SessionID: "s1", Path: target, Content: "line1\nline2\nline3"})
	if _, rpcErr := c.handleWriteTextFile(context.Background(), writeParams); rpcErr != nil {
		t.Fatalf("handleWriteTextFile() error = %v", rpcErr)
	}

	data, err := os.ReadFile(target)
	if err != nil || string(data) != "line1\nline2\nline3" {
		t.Fatalf("file on disk = %q, %v, want %q", data, err, "line1\nline2\nline3")
	}

	readParams, _ := json.Marshal(readTextFileParams{SessionID: "s1", Path: target})
	res, rpcErr := c.handleReadTextFile(context.Background(), readParams)
	if rpcErr != nil {
		t.Fatalf("handleReadTextFile() error = %v", rpcErr)
	}
	if got := res.(readTextFileResult).Content; got != "line1\nline2\nline3" {
		t.Fatalf("handleReadTextFile() content = %q, want %q", got, "line1\nline2\nline3")
	}
}

func TestClient_ReadTextFile_LineAndLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "file.txt")
	if err := os.WriteFile(target, []byte("l1\nl2\nl3\nl4"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	c := newFSTestClient(t, root)

	line := uint32(2)
	limit := uint32(2)
	params, _ := json.Marshal(readTextFileParams{SessionID: "s1", Path: target, Line: &line, Limit: &limit})
	res, rpcErr := c.handleReadTextFile(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("handleReadTextFile() error = %v", rpcErr)
	}
	if got := res.(readTextFileResult).Content; got != "l2\nl3" {
		t.Fatalf("handleReadTextFile() content = %q, want %q", got, "l2\nl3")
	}
}

func TestClient_ReadTextFile_OutOfBoundsRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	c := newFSTestClient(t, root)

	tests := []struct {
		name string
		path string
	}{
		{"escape via dotdot", filepath.Join(root, "..", filepath.Base(outside), "secret.txt")},
		{"absolute path outside root", secret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(readTextFileParams{SessionID: "s1", Path: tt.path})
			_, rpcErr := c.handleReadTextFile(context.Background(), params)
			if rpcErr == nil {
				t.Fatalf("handleReadTextFile(%q) succeeded, want rejection", tt.path)
			}
			if rpcErr.Code != ErrCodeInvalidParams {
				t.Fatalf("handleReadTextFile(%q) error code = %d, want %d", tt.path, rpcErr.Code, ErrCodeInvalidParams)
			}
		})
	}
}

func TestClient_WriteTextFile_OutOfBoundsRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "new-file.txt")

	c := newFSTestClient(t, root)

	params, _ := json.Marshal(writeTextFileParams{SessionID: "s1", Path: target, Content: "evil"})
	if _, rpcErr := c.handleWriteTextFile(context.Background(), params); rpcErr == nil {
		t.Fatal("handleWriteTextFile() succeeded, want rejection")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("handleWriteTextFile() wrote outside root: stat err = %v", err)
	}
}

func TestClient_ReadTextFile_UnknownSession(t *testing.T) {
	t.Parallel()
	c := newFSTestClient(t, t.TempDir())

	params, _ := json.Marshal(readTextFileParams{SessionID: "unknown", Path: "/etc/passwd"})
	_, rpcErr := c.handleReadTextFile(context.Background(), params)
	if rpcErr == nil || rpcErr.Code != ErrCodeInvalidParams {
		t.Fatalf("handleReadTextFile() for unknown session = %v, want ErrCodeInvalidParams", rpcErr)
	}
}

func TestResolveScopedPath_SymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := resolveScopedPath(root, filepath.Join(link, "secret.txt"))
	if err == nil {
		t.Fatal("resolveScopedPath() via symlink succeeded, want rejection")
	}
}
