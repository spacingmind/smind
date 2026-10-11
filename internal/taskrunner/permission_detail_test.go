package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func numberedLines(n int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString("\n")
	}
	return sb.String()
}

func writeCall(t *testing.T, kind, path, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"toolCallId": "1", "title": "Write file: " + path, "kind": kind, "status": "pending",
		"locations": []map[string]string{{"path": path}},
		"rawInput":  map[string]string{"path": path, "content": content},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionDetail_WriteReplacesExistingFile(t *testing.T) {
	wt := t.TempDir()
	mustWrite(t, filepath.Join(wt, "a/b.txt"), "one\ntwo\nthree\n")
	d := BuildPermissionDetail(wt, writeCall(t, "edit", "a/b.txt", "one\nTWO\n"))
	if d == nil || d.Kind != "write" || !d.ReplacesExisting {
		t.Fatalf("detail = %+v", d)
	}
	if *d.ExistingLines != 3 || *d.ExistingBytes != 14 || d.NewLines != 2 {
		t.Fatalf("stats = %+v", d)
	}
	if !strings.Contains(d.Diff, "-two") || !strings.Contains(d.Diff, "+TWO") || !strings.Contains(d.Diff, "-three") {
		t.Fatalf("diff = %q", d.Diff)
	}
	if d.Preview != "one\nTWO\n" {
		t.Fatalf("preview = %q", d.Preview)
	}
}

func TestPermissionDetail_WriteNewFile(t *testing.T) {
	wt := t.TempDir()
	d := BuildPermissionDetail(wt, writeCall(t, "edit", "new.txt", "a\nb\n"))
	if d == nil || d.ReplacesExisting || d.ExistingBytes != nil || d.Diff != "" || d.Redacted {
		t.Fatalf("detail = %+v", d)
	}
	if h := d.Headline(); !strings.HasPrefix(h, "new file, 2 lines") {
		t.Fatalf("headline = %q", h)
	}
}

func TestPermissionDetail_LargeShrinkFlag(t *testing.T) {
	wt := t.TempDir()
	mustWrite(t, filepath.Join(wt, "f.txt"), numberedLines(30))
	if d := BuildPermissionDetail(wt, writeCall(t, "edit", "f.txt", numberedLines(14))); !d.LargeShrink {
		t.Fatalf("expected largeShrink: %+v", d)
	}
	if d := BuildPermissionDetail(wt, writeCall(t, "edit", "f.txt", numberedLines(15))); d.LargeShrink {
		t.Fatalf("15 of 30 is not under half: %+v", d)
	}
	mustWrite(t, filepath.Join(wt, "small.txt"), numberedLines(19))
	if d := BuildPermissionDetail(wt, writeCall(t, "edit", "small.txt", "x\n")); d.LargeShrink {
		t.Fatalf("existing < 20 lines must not flag: %+v", d)
	}
}

func TestPermissionDetail_CapsPreviewAndDiff(t *testing.T) {
	wt := t.TempDir()
	mustWrite(t, filepath.Join(wt, "big.txt"), numberedLines(500))
	newContent := strings.Repeat("é", 10000) + "\n" + numberedLines(400)
	d := BuildPermissionDetail(wt, writeCall(t, "edit", "big.txt", newContent))
	if !d.Truncated {
		t.Fatal("expected truncated")
	}
	if len(d.Preview) > 4096 || strings.Count(d.Preview, "\n") > 60 {
		t.Fatalf("preview too big: %d", len(d.Preview))
	}
	if len(d.Diff) > 8192 || strings.Count(d.Diff, "\n") > 200 {
		t.Fatalf("diff too big: %d", len(d.Diff))
	}
	if !strings.HasPrefix(d.Preview, "é") || strings.ContainsRune(d.Preview, '�') {
		t.Fatal("preview cut mid-rune")
	}
	cmd, _ := json.Marshal(map[string]any{"kind": "execute", "rawInput": map[string]string{"command": strings.Repeat("a", 5000)}})
	c := BuildPermissionDetail(wt, cmd)
	if c == nil || len(c.Command) != 2048 || !c.Truncated {
		t.Fatalf("command = %+v", c)
	}
}

func TestPermissionDetail_SensitivePathRedactsExisting(t *testing.T) {
	wt := t.TempDir()
	mustWrite(t, filepath.Join(wt, ".env"), "SECRET=hunter2\n")
	d := BuildPermissionDetail(wt, writeCall(t, "edit", ".env", "A=1\n"))
	if !d.Redacted || d.Diff != "" || d.Preview != "" || d.ExistingBytes != nil || d.ExistingLines != nil || d.NewLines != 1 {
		t.Fatalf("detail = %+v", d)
	}
	for _, name := range []string{"server.pem", "id_rsa", "My_Credentials.json", "app.key", "client-SECRET.txt"} {
		mustWrite(t, filepath.Join(wt, name), "hunter2\n")
		d := BuildPermissionDetail(wt, writeCall(t, "edit", name, "x\n"))
		if !d.Redacted || d.Diff != "" || d.ExistingBytes != nil {
			t.Fatalf("%s: %+v", name, d)
		}
	}
}

func TestPermissionDetail_PathOutsideWorktreeRedactsExisting(t *testing.T) {
	wt, other := t.TempDir(), t.TempDir()
	outside := filepath.Join(other, "o.txt")
	mustWrite(t, outside, "hunter2\n")
	for _, p := range []string{outside, "../" + filepath.Base(other) + "/o.txt"} {
		d := BuildPermissionDetail(wt, writeCall(t, "edit", p, "x\n"))
		if !d.Redacted || d.Diff != "" || d.ExistingBytes != nil || d.ReplacesExisting && d.Diff != "" {
			t.Fatalf("%s: %+v", p, d)
		}
	}
}

func TestPermissionDetail_SymlinkEscapeRedacts(t *testing.T) {
	wt, other := t.TempDir(), t.TempDir()
	outside := filepath.Join(other, "o.txt")
	mustWrite(t, outside, "hunter2\n")
	if err := os.Symlink(outside, filepath.Join(wt, "link.txt")); err != nil {
		t.Skip(err)
	}
	d := BuildPermissionDetail(wt, writeCall(t, "edit", "link.txt", "x\n"))
	if !d.Redacted || d.Diff != "" || d.ExistingBytes != nil {
		t.Fatalf("detail = %+v", d)
	}
}

func TestPermissionDetail_NonWriteToolHasNoDetail(t *testing.T) {
	for _, kind := range []string{"read", "search", "fetch", "think", "other", ""} {
		if d := BuildPermissionDetail(t.TempDir(), writeCall(t, kind, "a.txt", "x")); d != nil {
			t.Fatalf("kind %q: %+v", kind, d)
		}
	}
	if BuildPermissionDetail(t.TempDir(), json.RawMessage(`not json`)) != nil {
		t.Fatal("unparsable must be nil")
	}
}

func TestPermissionDetail_EditToolShowsOldNewDiff(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"title": "Edit file: a.go", "kind": "edit",
		"rawInput": map[string]string{"path": "a.go", "old_text": "foo\nbar\n", "new_text": "foo\nbaz\n"},
	})
	d := BuildPermissionDetail(t.TempDir(), raw)
	if d == nil || d.Kind != "edit" || d.ReplacesExisting {
		t.Fatalf("detail = %+v", d)
	}
	if !strings.Contains(d.Diff, "-bar") || !strings.Contains(d.Diff, "+baz") {
		t.Fatalf("diff = %q", d.Diff)
	}
}

func TestPermissionDetail_HeadlineWording(t *testing.T) {
	wt := t.TempDir()
	mustWrite(t, filepath.Join(wt, "schema.sql"), numberedLines(240))
	d := BuildPermissionDetail(wt, writeCall(t, "edit", "schema.sql", numberedLines(7)))
	h := d.Headline()
	if !strings.Contains(h, "240 -> 7 lines") || !strings.Contains(h, "OVERWRITES") {
		t.Fatalf("headline = %q", h)
	}
	if (*PermissionDetail)(nil).Headline() != "" {
		t.Fatal("nil headline")
	}
}
