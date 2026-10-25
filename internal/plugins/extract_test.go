package plugins

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extractBytes(t *testing.T, data []byte, lim Limits) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	ar := writeFile(t, filepath.Join(root, "in.bin"), data)
	dest := filepath.Join(root, "out", "tree")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ExtractArchive(ar, dest, lim)
	return root, dest, err
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %q, got none", code)
	}
	if got := CodeOf(err); got != code {
		t.Fatalf("want code %q, got %q (%v)", code, got, err)
	}
}

func TestExtract_PathTraversalRejected(t *testing.T) {
	names := []string{"../x", "a/../../x", "/abs/x", `C:\x`, "C:/x", `..\x`, `a\..\x`, "a\x00b", "a/./../../x", "ok/../../x"}
	for _, n := range names {
		for _, format := range []string{"tar", "tgz", "zip"} {
			if strings.ContainsRune(n, 0) && format != "zip" {
				continue // tar cannot encode a NUL name; zip can
			}
			t.Run(format+"/"+strings.ReplaceAll(n, "\x00", "NUL"), func(t *testing.T) {
				ents := []ent{{name: "fine.txt", body: "ok"}, {name: n, body: "pwn"}}
				var data []byte
				switch format {
				case "tar":
					data = buildTar(t, false, ents)
				case "tgz":
					data = buildTar(t, true, ents)
				default:
					data = buildZip(t, ents)
				}
				root, dest, err := extractBytes(t, data, DefaultLimits)
				wantCode(t, err, CodeUnsafeEntry)
				// Nothing escaped the destination, wherever the name pointed.
				for _, p := range []string{filepath.Join(dest, "..", "x"), filepath.Join(root, "x"), filepath.Join(dest, "..", "..", "x")} {
					if _, serr := os.Stat(p); serr == nil {
						t.Fatalf("%s was written", p)
					}
				}
			})
		}
	}
}

func TestExtract_SymlinkAndHardlinkRejected(t *testing.T) {
	cases := map[string][]byte{
		"tar symlink":  buildTar(t, false, []ent{{name: "l", typ: tar.TypeSymlink, target: "/etc/passwd"}}),
		"tgz symlink":  buildTar(t, true, []ent{{name: "l", typ: tar.TypeSymlink, target: "../../x"}}),
		"tar hardlink": buildTar(t, false, []ent{{name: "a", body: "x"}, {name: "h", typ: tar.TypeLink, target: "a"}}),
		"zip symlink":  buildZip(t, []ent{{name: "l", typ: tar.TypeSymlink, target: "/etc/passwd"}}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := extractBytes(t, data, DefaultLimits)
			wantCode(t, err, CodeLinkRejected)
		})
	}
	t.Run("tar fifo and device", func(t *testing.T) {
		for _, typ := range []byte{tar.TypeFifo, tar.TypeChar, tar.TypeBlock} {
			_, _, err := extractBytes(t, buildTar(t, false, []ent{{name: "dev", typ: typ}}), DefaultLimits)
			wantCode(t, err, CodeUnsafeEntry)
		}
	})
}

func TestExtract_SymlinkEscapeViaDirSourceRejected(t *testing.T) {
	secret := t.TempDir()
	writeFile(t, filepath.Join(secret, "token"), []byte("s3cret"))

	src := writePluginDir(t, goodManifest, nil)
	if err := os.Symlink(secret, filepath.Join(src, "evil")); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	dest := filepath.Join(t.TempDir(), "tree")
	os.MkdirAll(dest, 0o755)
	_, err := CopyDir(src, dest, DefaultLimits)
	wantCode(t, err, CodeLinkRejected)
	if _, serr := os.Stat(filepath.Join(dest, "evil", "token")); serr == nil {
		t.Fatal("the symlink was followed")
	}

	// A file symlink is refused too.
	src2 := writePluginDir(t, goodManifest, nil)
	os.Symlink(filepath.Join(secret, "token"), filepath.Join(src2, "t.txt"))
	dest2 := filepath.Join(t.TempDir(), "tree")
	os.MkdirAll(dest2, 0o755)
	_, err = CopyDir(src2, dest2, DefaultLimits)
	wantCode(t, err, CodeLinkRejected)

	// The source root itself may be a symlink the Human typed: it resolves
	// once, and a clean target copies fine while an unsafe one still fails.
	clean := writePluginDir(t, goodManifest, map[string]string{"a.txt": "a"})
	rootLink := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(clean, rootLink); err != nil {
		t.Fatal(err)
	}
	dest3 := filepath.Join(t.TempDir(), "tree")
	os.MkdirAll(dest3, 0o755)
	if _, err := CopyDir(rootLink, dest3, DefaultLimits); err != nil {
		t.Fatalf("clean target behind a root symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest3, "a.txt")); err != nil {
		t.Fatal("clean files were not copied")
	}
	evilRoot := filepath.Join(t.TempDir(), "evilroot")
	os.Symlink(src, evilRoot) // src has the evil link inside
	dest4 := filepath.Join(t.TempDir(), "tree")
	os.MkdirAll(dest4, 0o755)
	_, err = CopyDir(evilRoot, dest4, DefaultLimits)
	wantCode(t, err, CodeLinkRejected)
}

func TestExtract_SizeAndEntryCapsEnforced(t *testing.T) {
	lim := Limits{MaxEntries: 20, MaxTotalBytes: 1 << 20, MaxFileBytes: 512 << 10}

	t.Run("tgz bomb: small archive, huge expansion", func(t *testing.T) {
		data := buildTar(t, true, []ent{{name: "big", body: strings.Repeat("\x00", 8<<20)}})
		if len(data) > 64<<10 {
			t.Fatalf("test archive is not small: %d", len(data))
		}
		_, _, err := extractBytes(t, data, lim)
		wantCode(t, err, CodeTooLarge)
	})
	t.Run("zip whose header lies about the size", func(t *testing.T) {
		data := lyingZip(t, "big", 8<<20)
		_, dest, err := extractBytes(t, data, lim)
		if err == nil || (CodeOf(err) != CodeBadArchive && CodeOf(err) != CodeTooLarge) {
			t.Fatalf("lying header must be rejected with a typed error, got %v", err)
		}
		// It was cut off at the cap, not written out in full.
		if st, serr := os.Stat(filepath.Join(dest, "big")); serr == nil && st.Size() > lim.MaxFileBytes+1 {
			t.Fatalf("wrote %d bytes past the cap", st.Size())
		}
	})
	t.Run("total cap across many honest entries", func(t *testing.T) {
		var ents []ent
		for i := 0; i < 8; i++ {
			ents = append(ents, ent{name: "f" + string(rune('a'+i)), body: strings.Repeat("A", 300<<10)})
		}
		_, _, err := extractBytes(t, buildTar(t, true, ents), lim)
		wantCode(t, err, CodeTooLarge)
	})
	t.Run("entry count", func(t *testing.T) {
		var ents []ent
		for i := 0; i < 30; i++ {
			ents = append(ents, ent{name: "d/" + strings.Repeat("x", i+1), body: "1"})
		}
		for _, data := range [][]byte{buildTar(t, false, ents), buildZip(t, ents)} {
			_, _, err := extractBytes(t, data, lim)
			wantCode(t, err, CodeTooManyEntries)
		}
	})
	t.Run("a normal archive passes", func(t *testing.T) {
		_, dest, err := extractBytes(t, buildTar(t, true, []ent{{name: "a/b.txt", body: "hi", exec: true}}), lim)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "a", "b.txt")); string(b) != "hi" {
			t.Fatalf("content %q", b)
		}
	})
}

func TestExtract_DuplicateAndCaseCollisionRejected(t *testing.T) {
	cases := map[string][]ent{
		"same name twice":        {{name: "a.txt", body: "1"}, {name: "a.txt", body: "2"}},
		"case collision":         {{name: "dir/Readme", body: "1"}, {name: "dir/readme", body: "2"}},
		"dir case collision":     {{name: "Dir/a", body: "1"}, {name: "dir/b", body: "2"}},
		"file then dir under it": {{name: "a", body: "1"}, {name: "a/b", body: "2"}},
	}
	for name, ents := range cases {
		for _, fmtName := range []string{"tar", "zip"} {
			t.Run(name+"/"+fmtName, func(t *testing.T) {
				var data []byte
				if fmtName == "tar" {
					data = buildTar(t, false, ents)
				} else {
					data = buildZip(t, ents)
				}
				_, _, err := extractBytes(t, data, DefaultLimits)
				wantCode(t, err, CodeDuplicateEntry)
			})
		}
	}
}

func TestCleanEntryName(t *testing.T) {
	ok := map[string]string{"a/b": "a/b", "./a/b": "a/b", "a//b/": "a/b", "a/./b": "a/b"}
	for in, want := range ok {
		got, err := CleanEntryName(in)
		if err != nil || got != want {
			t.Errorf("CleanEntryName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/..", "con", "a/NUL.txt", "a:b", "x.", "x /y"} {
		if _, err := CleanEntryName(bad); err == nil {
			t.Errorf("CleanEntryName(%q) should fail", bad)
		}
	}
}

func TestTreeHash_StableAndContentSensitive(t *testing.T) {
	src := writePluginDir(t, goodManifest, map[string]string{"a.txt": "a"})
	d1, d2 := filepath.Join(t.TempDir(), "t"), filepath.Join(t.TempDir(), "t")
	os.MkdirAll(d1, 0o755)
	os.MkdirAll(d2, 0o755)
	h1, err := CopyDir(src, d1, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := CopyDir(src, d2, DefaultLimits)
	again, err := treeHashOf(d1)
	if err != nil || h1 != h2 || h1 != again {
		t.Fatalf("hash unstable: %s %s %s %v", h1, h2, again, err)
	}
	os.WriteFile(filepath.Join(d1, "a.txt"), []byte("changed"), 0o644)
	if h, _ := treeHashOf(d1); h == h1 {
		t.Fatal("hash ignores content")
	}
}

// infiniteReader yields zeros forever: what a stream looks like when its
// header (or stat size) promised something small.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) { return len(p), nil }

func TestExtract_StreamCountedNotHeader(t *testing.T) {
	lim := Limits{MaxEntries: 10, MaxTotalBytes: 1 << 20, MaxFileBytes: 64 << 10}
	dest := t.TempDir()
	w := newTreeWriter(dest, lim)
	err := w.writeFile("big.bin", infiniteReader{}, false)
	wantCode(t, err, CodeTooLarge)
	if st, serr := os.Stat(filepath.Join(dest, "big.bin")); serr != nil || st.Size() > lim.MaxFileBytes+1 {
		t.Fatalf("file not cut at the cap: %v %v", st, serr)
	}
	// And the total cap across entries, each under the per-file cap.
	w2 := newTreeWriter(t.TempDir(), Limits{MaxEntries: 100, MaxTotalBytes: 100 << 10, MaxFileBytes: 64 << 10})
	var last error
	for i := 0; i < 5 && last == nil; i++ {
		last = w2.writeFile("f"+string(rune('a'+i)), io.LimitReader(infiniteReader{}, 40<<10), false)
	}
	wantCode(t, last, CodeTooLarge)
}
