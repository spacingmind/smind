package wsapi

// attachments.go -- prompt attachments on the wire (ADR-0038): request
// parsing shared by task.prompt and run.start, the per-request resolvers,
// run.attachment, and the daemon.info limits. Nothing here logs params or
// attachment data; errors name the attachment's position only.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/files"
	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/workspace"
)

const (
	// DirectMaxMessage is the largest message the /ws connection reads.
	DirectMaxMessage = 16 << 20
	// RelayMaxMessage is the largest payload a relay data connection
	// carries; run.attachment refuses anything that would not fit.
	RelayMaxMessage = 1048551
)

// SetAttachmentStore turns prompt attachments on (nil = off, the default).
// Like SetManagedBy it must be called at startup, before the API serves any
// connection.
func (a *API) SetAttachmentStore(st *attach.Store) { a.Runs.SetAttachmentStore(st) }

// AttachmentsInfo is daemon.info's `attachments` field.
type AttachmentsInfo struct {
	MaxCount        int      `json:"maxCount"`
	MaxImageBytes   int      `json:"maxImageBytes"`
	MaxFileBytes    int      `json:"maxFileBytes"`
	MaxTotalBytes   int      `json:"maxTotalBytes"`
	MaxMessageBytes int      `json:"maxMessageBytes"`
	Images          []string `json:"images"`
	// Kinds are the attachment kinds this daemon accepts, so a client shows
	// only affordances that work (absent on an older daemon = image, file,
	// file_ref).
	Kinds []string `json:"kinds"`
}

func attachmentsInfo(maxMessage int) *AttachmentsInfo {
	lim := attach.DefaultLimits()
	return &AttachmentsInfo{
		MaxCount: lim.MaxCount, MaxImageBytes: lim.MaxImageBytes, MaxFileBytes: lim.MaxFileBytes,
		MaxTotalBytes: lim.MaxTotalBytes, MaxMessageBytes: maxMessage,
		Images: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
		Kinds:  []string{attach.KindImage, attach.KindFile, attach.KindFileRef, attach.KindPR, attach.KindIssue},
	}
}

// connMaxMessage is the message size limit of the connection rc arrived on.
func connMaxMessage(rc *requestContext) int {
	if rc == nil || rc.conn == nil || rc.conn.maxMessage <= 0 {
		return RelayMaxMessage
	}
	return rc.conn.maxMessage
}

// acceptAttachments validates reqs for a prompt to taskID and writes the
// accepted set to the store. No requests yield the zero Set and no error;
// a daemon without a store refuses any. A rejection leaves nothing on disk.
func acceptAttachments(ctx context.Context, wm *workspace.Manager, reg *runs.Registry, gh *githubHolder, taskID int64, reqs []attach.Request) (attach.Set, error) {
	if len(reqs) == 0 {
		return attach.Set{}, nil
	}
	st := reg.AttachmentStore()
	if st == nil {
		return attach.Set{}, &attach.Error{Code: attach.CodeInvalid, Message: "attachments are not enabled on this daemon", Index: -1}
	}
	res := attach.Resolvers{
		FileRef: func(path string) (string, bool, error) { return resolveFileRef(wm, taskID, path) },
		Forge: func(ctx context.Context, forgeName, kind string, number int, repo string) (attach.ForgeSnapshot, error) {
			return fetchForgeSnapshot(ctx, wm, gh, taskID, forgeName, kind, number, repo)
		},
		Reuse: func(runID, id string) (attach.Item, string, error) {
			return reg.ReuseAttachment(st, taskID, runID, id)
		},
	}
	return st.Accept(ctx, reqs, attach.DefaultLimits(), res)
}

// fetchForgeSnapshot is the attach.Resolvers.Forge of a prompt to taskID: one
// read-only fetch of the PR or issue, in the task's worktree, through the
// daemon host's own forge login (ADR-0038 §4). It runs at accept time, so the
// agent sees what the user saw even if the PR changes, a queued prompt is
// delivered later, or a replay would fail. A *forge.Error keeps its code
// (forge_cli_missing, forge_unauthenticated, forge_no_remote, forge_not_found,
// forge_rate_limited, forge_timeout, forge_error) and its fixed message; it
// never carries argv or stderr.
func fetchForgeSnapshot(ctx context.Context, wm *workspace.Manager, gh *githubHolder, taskID int64, forgeName, kind string, number int, repo string) (attach.ForgeSnapshot, error) {
	if forgeName != "github" {
		return attach.ForgeSnapshot{}, &attach.Error{Code: attach.CodeInvalid, Message: fmt.Sprintf("unsupported forge %q (only \"github\")", forgeName), Index: -1}
	}
	if gh == nil || gh.c == nil {
		return attach.ForgeSnapshot{}, &attach.Error{Code: attach.CodeForgeError, Message: "no forge available", Index: -1}
	}
	dir, err := taskWorktreePath(wm, taskID)
	if err != nil {
		return attach.ForgeSnapshot{}, &attach.Error{Code: attach.CodeNotFound, Message: "task has no workspace", Index: -1}
	}
	snap, err := gh.c.Get(ctx, dir, kind, number, repo)
	if err != nil {
		var fe *forge.Error
		if errors.As(err, &fe) {
			return attach.ForgeSnapshot{}, &attach.Error{Code: fe.Code, Message: fe.Message, Index: -1}
		}
		return attach.ForgeSnapshot{}, &attach.Error{Code: attach.CodeForgeError, Message: "the forge returned an error", Index: -1}
	}
	return attach.ForgeSnapshot{
		Number: snap.Number, Repo: snap.Repo, Title: snap.Title, URL: snap.URL, State: snap.State,
		BaseRef: snap.BaseRef, HeadRef: snap.HeadRef, Body: snap.Body,
	}, nil
}

// resolveFileRef maps a client path to a workspace-relative one. It checks
// containment (symlinks resolved) and existence only: the file is never
// opened.
func resolveFileRef(wm *workspace.Manager, taskID int64, path string) (string, bool, error) {
	task, err := wm.GetTask(taskID)
	if err != nil || task.WorktreePath == nil {
		return "", false, &attach.Error{Code: attach.CodeNotFound, Message: "task has no workspace"}
	}
	root := *task.WorktreePath
	abs, err := files.Resolve(root, path)
	if err != nil {
		return "", false, &attach.Error{Code: attach.CodeOutsideWorkspace, Message: "path is outside the workspace"}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, &attach.Error{Code: attach.CodeNotFound, Message: "task has no workspace"}
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", false, &attach.Error{Code: attach.CodeNotFound, Message: "path does not exist"}
	}
	rel, err := filepath.Rel(realRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, &attach.Error{Code: attach.CodeOutsideWorkspace, Message: "path is outside the workspace"}
	}
	return filepath.ToSlash(rel), fi.IsDir(), nil
}

// runAttachmentResult is run.attachment's result.
type runAttachmentResult struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Data     string `json:"data"`
}

// handleRunAttachment returns the bytes of one stored image/file attachment
// of a run, refusing what would not fit this connection's message limit.
func handleRunAttachment(reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			RunID string `json:"runId"`
			ID    string `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errors.New("run.attachment: invalid params")
		}
		notFound := &attach.Error{Code: attach.CodeNotFound, Message: "run.attachment: attachment not found", Index: -1}
		st := reg.AttachmentStore()
		if st == nil {
			return nil, notFound
		}
		set, it, ok := reg.StoredAttachment(p.RunID, p.ID)
		if !ok || (it.Kind != attach.KindImage && it.Kind != attach.KindFile) {
			return nil, notFound
		}
		// base64 expands 3 bytes to 4; the slack covers the JSON envelope
		// and the escaped name and mime type.
		if encoded := (it.Size+2)/3*4 + int64(len(it.Name)+len(it.MimeType))*6 + 256; encoded > int64(connMaxMessage(rc)) {
			return nil, &attach.Error{Code: attach.CodeTooLarge, Message: "run.attachment: attachment is too large for this connection", Index: -1}
		}
		data, err := st.ReadFile(set.ID, it)
		if err != nil {
			return nil, notFound
		}
		return runAttachmentResult{Name: it.Name, MimeType: it.MimeType, Size: int64(len(data)), Data: base64.StdEncoding.EncodeToString(data)}, nil
	}
}

// startOptions are the per-run start options of a prompt: the model, plus the
// accepted attachment set when there is one.
func startOptions(model string, set attach.Set, reg *runs.Registry) []runs.StartOption {
	opts := []runs.StartOption{runs.WithModel(model)}
	if len(set.Items) > 0 {
		opts = append(opts, runs.WithAttachments(set, reg.AttachmentStore()))
	}
	return opts
}
