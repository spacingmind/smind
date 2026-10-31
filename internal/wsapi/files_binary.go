package wsapi

// files_binary.go -- file.stat and file.readRange (ADR-0049 §2): sniffed
// classification and chunked binary reads of a task's worktree files, plus
// the limits daemon.info advertises. Both are task-scoped like file.read,
// are never reachable by a plugin session (the "file." never-prefix), and
// log neither params nor bytes: errors name the caller's relative path and a
// fixed reason only (see internal/files/binary.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/files"
	"github.com/spacingmind/smind/internal/workspace"
)

const (
	// FileMaxReadBytes is the largest file file.readRange serves any part of.
	FileMaxReadBytes = 50 << 20
	// FileMaxChunkDirect is the raw chunk ceiling on the direct /ws transport.
	FileMaxChunkDirect = 4 << 20
	// FileMaxChunkRelay is the raw chunk ceiling over the E2EE relay: about
	// 700 KiB of base64 plus the JSON envelope stays under RelayMaxMessage
	// (itself under the relay's 1<<20 frame limit).
	FileMaxChunkRelay = 512 << 10
)

// FilesInfo is daemon.info's `files` field: this connection's file-read limits.
type FilesInfo struct {
	MaxReadBytes  int      `json:"maxReadBytes"`
	MaxChunkBytes int      `json:"maxChunkBytes"`
	PreviewKinds  []string `json:"previewKinds"`
}

// filesInfo is the `files` capability for a connection whose transport
// carries messages of up to maxMessage bytes.
func filesInfo(maxMessage int) *FilesInfo {
	return &FilesInfo{
		MaxReadBytes:  FileMaxReadBytes,
		MaxChunkBytes: fileChunkLimit(maxMessage),
		PreviewKinds:  []string{files.KindPDF},
	}
}

// fileChunkLimit picks the chunk ceiling from the transport's message
// limit: anything smaller than the direct limit is treated as the relay.
func fileChunkLimit(maxMessage int) int {
	if maxMessage >= DirectMaxMessage {
		return FileMaxChunkDirect
	}
	return FileMaxChunkRelay
}

type fileStatResult struct {
	Size        int64    `json:"size"`
	Mtime       DateTime `json:"mtime"`
	Kind        string   `json:"kind"`
	Mime        string   `json:"mime"`
	Ext         string   `json:"ext"`
	Mismatch    bool     `json:"mismatch"`
	Previewable bool     `json:"previewable"`
}

type fileReadRangeResult struct {
	Offset int64    `json:"offset"`
	Length int      `json:"length"`
	EOF    bool     `json:"eof"`
	Data   []byte   `json:"data"` // base64 on the wire
	Mtime  DateTime `json:"mtime"`
}

// handleFileStat serves file.stat: {taskId, path} classified from content.
func handleFileStat(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.stat: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.stat: %w", err)
		}
		st, err := files.Stat(root, p.Path)
		if err != nil {
			return nil, fmt.Errorf("file.stat: %w", err)
		}
		return fileStatResult{
			Size: st.Size, Mtime: DateTime(st.Mtime), Kind: st.Kind, Mime: st.Mime,
			Ext: st.Ext, Mismatch: st.Mismatch, Previewable: st.Previewable,
		}, nil
	}
}

// handleFileReadRange serves file.readRange: {taskId, path, offset, length,
// ifMtime?} → up to the connection's chunk ceiling of bytes.
func handleFileReadRange(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID  int64    `json:"taskId"`
			Path    string   `json:"path"`
			Offset  int64    `json:"offset"`
			Length  int64    `json:"length"`
			IfMtime DateTime `json:"ifMtime"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.readRange: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.readRange: %w", err)
		}
		var ifMtime *time.Time
		if t := time.Time(p.IfMtime); !t.IsZero() {
			ifMtime = &t
		}
		lim := files.Limits{
			MaxReadBytes:  FileMaxReadBytes,
			MaxChunkBytes: int64(fileChunkLimit(connMaxMessage(rc))),
		}
		rng, err := files.ReadRange(root, p.Path, p.Offset, p.Length, lim, ifMtime)
		if err != nil {
			return nil, fmt.Errorf("file.readRange: %w", err)
		}
		if rng.Data == nil {
			rng.Data = []byte{} // "" on the wire, not null
		}
		return fileReadRangeResult{
			Offset: rng.Offset, Length: len(rng.Data), EOF: rng.EOF,
			Data: rng.Data, Mtime: DateTime(rng.Mtime),
		}, nil
	}
}
