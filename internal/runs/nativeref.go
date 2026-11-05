package runs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/providers"
)

// nativeRef is runs.native_ref (ADR-0053): where the provider keeps the
// session this run was a turn of. It names a place and a time window; it holds
// no session content. The daemon writes it when the run ends, from what the
// runner launched the CLI with, never from user input.
type nativeRef = native.Ref

type turnWindow = native.TurnWindow

// claudeProjectDir is the directory name Claude Code gives a working directory
// under <config dir>/projects: every character that is not an ASCII letter or
// digit becomes "-" (so "/.smind/" is "--smind-"). Verified against the
// transcripts on a real machine; see TestClaudeProjectDirMatchesRealLayout.
func claudeProjectDir(cwd string) string { return native.ClaudeProjectDir(cwd) }

// claudeTranscript is <configDir>/projects/<cwd dir>/<sessionID>.jsonl.
func claudeTranscript(configDir, cwd, sessionID string) string {
	return filepath.Join(configDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
}

// codexRollout finds the rollout file of a thread: sessions/YYYY/MM/DD/
// rollout-<ts>-<threadID>.jsonl, or archived_sessions/ after an archive.
func codexRollout(home, threadID string) string {
	if home == "" || threadID == "" || strings.ContainsAny(threadID, `*?[]/\`) {
		return ""
	}
	for _, pattern := range []string{
		filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+threadID+".jsonl"),
		filepath.Join(home, "archived_sessions", "rollout-*-"+threadID+".jsonl"),
	} {
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

// transcriptVersion reads the CLI version a session file records, looking at
// its first lines only: "version" on a claude record, session_meta.cli_version
// on a codex rollout.
func transcriptVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for i := 0; i < 50 && sc.Scan(); i++ {
		var rec struct {
			Version string `json:"version"`
			Payload struct {
				CLIVersion string `json:"cli_version"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Version != "" {
			return rec.Version
		}
		if rec.Payload.CLIVersion != "" {
			return rec.Payload.CLIVersion
		}
	}
	return ""
}

// captureNativeRef stores r's native_ref. Best effort: a run with no provider
// session (nothing to point at) gets none.
func (reg *Registry) captureNativeRef(r *run, finishedAt time.Time) {
	if r.runner == nil {
		return
	}
	ns, ok := r.runner.NativeSession(r.chatID, r.provider)
	if !ok {
		return
	}
	ref := nativeRef{
		Provider: string(r.provider), Harness: ns.Harness, SessionID: ns.SessionID,
		ConfigDir: ns.ConfigDir, CodexHome: ns.CodexHome,
		TurnWindow: turnWindow{StartedAt: r.startedAt, FinishedAt: finishedAt}, PromptSha256: sha256Hex(r.prompt),
		CapturedAt: time.Now().UTC(),
	}
	if t, err := reg.st.GetTask(r.taskID); err == nil && t.WorktreePath != nil {
		ref.Cwd = *t.WorktreePath
	}
	switch providers.Harness(ns.Harness) {
	case providers.HarnessClaudeCode:
		if ns.ConfigDir != "" && ref.Cwd != "" {
			if p := claudeTranscript(ns.ConfigDir, ref.Cwd, ns.SessionID); fileExists(p) {
				ref.TranscriptPath = p
			}
		}
	case providers.HarnessCodexAppServer:
		ref.TranscriptPath = codexRollout(ns.CodexHome, ns.SessionID)
	}
	if ref.TranscriptPath != "" {
		ref.ProviderVersion = transcriptVersion(ref.TranscriptPath)
	}
	b, err := json.Marshal(ref)
	if err != nil {
		return
	}
	_ = reg.st.SetRunNativeRef(r.id, string(b))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
