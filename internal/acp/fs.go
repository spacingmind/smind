package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type readTextFileParams struct {
	SessionID string  `json:"sessionId"`
	Path      string  `json:"path"`
	Line      *uint32 `json:"line,omitempty"`
	Limit     *uint32 `json:"limit,omitempty"`
}

type readTextFileResult struct {
	Content string `json:"content"`
}

type writeTextFileParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

type writeTextFileResult struct{}

// handleReadTextFile implements the agent-initiated fs/read_text_file
// request, honoring the optional 1-based line/limit window.
func (c *Client) handleReadTextFile(_ context.Context, raw json.RawMessage) (any, *RPCError) {
	var p readTextFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrCodeInvalidParams, Message: "fs/read_text_file: " + err.Error()}
	}

	resolved, rpcErr := c.resolveRequestPath(p.SessionID, p.Path)
	if rpcErr != nil {
		return nil, rpcErr
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeResourceNotFound, Message: "fs/read_text_file: " + err.Error()}
	}

	content := string(data)
	if p.Line != nil || p.Limit != nil {
		content = selectLines(content, p.Line, p.Limit)
	}
	return readTextFileResult{Content: content}, nil
}

// handleWriteTextFile implements the agent-initiated fs/write_text_file
// request.
func (c *Client) handleWriteTextFile(_ context.Context, raw json.RawMessage) (any, *RPCError) {
	var p writeTextFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrCodeInvalidParams, Message: "fs/write_text_file: " + err.Error()}
	}

	resolved, rpcErr := c.resolveRequestPath(p.SessionID, p.Path)
	if rpcErr != nil {
		return nil, rpcErr
	}

	if err := os.WriteFile(resolved, []byte(p.Content), 0o644); err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "fs/write_text_file: " + err.Error()}
	}
	return writeTextFileResult{}, nil
}

func (c *Client) resolveRequestPath(sessionID, path string) (string, *RPCError) {
	root, ok := c.sessionRoot(sessionID)
	if !ok {
		return "", &RPCError{Code: ErrCodeInvalidParams, Message: fmt.Sprintf("unknown session %q", sessionID)}
	}
	resolved, err := resolveScopedPath(root, path)
	if err != nil {
		return "", &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
	}
	return resolved, nil
}

// resolveScopedPath resolves path to an absolute, symlink-evaluated form
// and verifies it falls within root (also symlink-evaluated). This is the
// security boundary that keeps the agent subprocess confined to the task's
// worktree: without it, a malicious or buggy agent could use "../" or a
// symlink planted inside the worktree to read or write arbitrary files on
// the host. EvalSymlinks requires its target to exist, which fails for a
// fs/write_text_file call creating a brand-new file, so writes resolve
// their parent directory instead and rejoin the file's base name.
func resolveScopedPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q must be absolute", path)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", root, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", root, err)
	}

	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve path %q: %w", path, err)
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
		if err != nil {
			return "", fmt.Errorf("resolve parent of %q: %w", path, err)
		}
		resolved = filepath.Join(parent, filepath.Base(clean))
	}

	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes session root %q", path, root)
	}
	return resolved, nil
}

// selectLines returns the 1-based, limit-bounded window of content that
// ReadTextFileRequest's line/limit params describe. Splitting on "\n" and
// rejoining doesn't perfectly preserve a file's exact trailing-newline
// byte layout; that's an accepted simplification given ACP callers use
// this for displaying/editing text, not byte-exact reproduction.
func selectLines(content string, line, limit *uint32) string {
	lines := strings.Split(content, "\n")

	start := 0
	if line != nil && *line > 0 {
		start = int(*line) - 1
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if limit != nil && start+int(*limit) < end {
		end = start + int(*limit)
	}

	return strings.Join(lines[start:end], "\n")
}
