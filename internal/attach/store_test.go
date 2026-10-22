package attach

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	root := filepath.Join(t.TempDir(), "attachments")
	return NewStore(root), root
}

func accept(t *testing.T, s *Store, r Resolvers, reqs ...Request) Set {
	t.Helper()
	set, err := s.Accept(context.Background(), reqs, DefaultLimits(), r)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestStore_WritesSetWithModes(t *testing.T) {
	s, root := newTestStore(t)
	png := pngBytes(t, 2, 2)
	set := accept(t, s, Resolvers{
		Github: func(context.Context, string, int, string) (GithubSnapshot, error) {
			return GithubSnapshot{Title: "t", Body: "body"}, nil
		},
	},
		imgReq(png, "image/png"),
		Request{Kind: KindFile, Name: "n.txt", Data: b64([]byte("hi"))},
		Request{Kind: KindGithubPR, Number: 4},
	)
	if len(set.ID) != 32 || len(set.Items) != 3 {
		t.Fatalf("%+v", set)
	}
	dir := s.Dir(set.ID)
	if dir != filepath.Join(root, set.ID) {
		t.Fatal(dir)
	}
	wantFiles := []string{"01-x", "02-n.txt", "03-body.md"}
	for i, f := range wantFiles {
		if set.Items[i].File != f {
			t.Errorf("item %d file %q want %q", i, set.Items[i].File, f)
		}
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, st.Mode().Perm())
		}
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{dir, root} {
			st, _ := os.Stat(p)
			if st.Mode().Perm() != 0o700 {
				t.Errorf("%s mode %v", p, st.Mode().Perm())
			}
		}
	}
	got, err := s.ReadFile(set.ID, set.Items[0])
	if err != nil || string(got) != string(png) {
		t.Fatalf("read back: %v", err)
	}
	if err := s.Remove(set.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("set not removed")
	}
	if err := s.Remove(set.ID); err != nil {
		t.Fatal("removing a missing set must not fail")
	}
}

func TestStore_NoCollisionOnSameName(t *testing.T) {
	s, _ := newTestStore(t)
	set := accept(t, s, Resolvers{},
		Request{Kind: KindFile, Name: "a.txt", Data: b64([]byte("one"))},
		Request{Kind: KindFile, Name: "a.txt", Data: b64([]byte("two"))},
	)
	a, _ := s.ReadFile(set.ID, set.Items[0])
	b, _ := s.ReadFile(set.ID, set.Items[1])
	if string(a) != "one" || string(b) != "two" || set.Items[0].File == set.Items[1].File {
		t.Fatalf("%q %q %+v", a, b, set.Items)
	}
}

func rootEntries(t *testing.T, root string) []string {
	t.Helper()
	es, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestStore_FailureLeavesNoFiles(t *testing.T) {
	s, root := newTestStore(t)
	boom := errors.New("boom")
	_, err := s.Accept(context.Background(), []Request{
		imgReq(pngBytes(t, 2, 2), "image/png"),
		{Kind: KindGithubPR, Number: 1},
	}, DefaultLimits(), Resolvers{
		Github: func(context.Context, string, int, string) (GithubSnapshot, error) { return GithubSnapshot{}, boom },
	})
	wantCode(t, err, CodeGithubUnavailable)
	if es := rootEntries(t, root); len(es) != 0 {
		t.Fatalf("rejected Accept left %v", es)
	}

	// a validation failure after a valid file
	_, err = s.Accept(context.Background(), []Request{
		{Kind: KindFile, Name: "a", Data: b64([]byte("x"))},
		{Kind: KindFile, Name: "b", Data: "!!"},
	}, DefaultLimits(), Resolvers{})
	wantCode(t, err, CodeInvalidBase64)
	if es := rootEntries(t, root); len(es) != 0 {
		t.Fatalf("left %v", es)
	}

	// root that cannot be created: its parent is a regular file
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := NewStore(filepath.Join(blocker, "attachments"))
	if _, err := bad.Accept(context.Background(), []Request{{Kind: KindFile, Name: "a", Data: b64([]byte("x"))}}, DefaultLimits(), Resolvers{}); err == nil {
		t.Fatal("want error for unwritable root")
	}

	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		rs := NewStore(ro)
		if _, err := rs.Accept(context.Background(), []Request{{Kind: KindFile, Name: "a", Data: b64([]byte("x"))}}, DefaultLimits(), Resolvers{}); err == nil {
			t.Fatal("want error for read-only root")
		}
		if es := rootEntries(t, ro); len(es) != 0 {
			t.Fatalf("left %v", es)
		}
	}
}

func TestStore_ReuseCopiesToNewSet(t *testing.T) {
	s, _ := newTestStore(t)
	png := pngBytes(t, 3, 3)
	first := accept(t, s, Resolvers{}, imgReq(png, "image/png"))
	src := first.Items[0]
	r := Resolvers{Reuse: func(runID, id string) (Item, string, error) {
		if runID != "run1" || id != "1" {
			return Item{}, "", errors.New("nope")
		}
		return src, filepath.Join(s.Dir(first.ID), src.File), nil
	}}
	second := accept(t, s, r,
		Request{Kind: KindFile, Name: "z.txt", Data: b64([]byte("z"))},
		Request{Kind: KindReuse, RunID: "run1", ID: "1"},
	)
	if second.ID == first.ID {
		t.Fatal("reuse must create a new set")
	}
	it := second.Items[1]
	if it.ID != "2" || it.Kind != KindImage || it.Width != 3 || it.File != "02-x" {
		t.Fatalf("%+v", it)
	}
	got, err := s.ReadFile(second.ID, it)
	if err != nil || string(got) != string(png) {
		t.Fatal("copy differs", err)
	}
	// removing the source set leaves the copy intact
	if err := s.Remove(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadFile(second.ID, it); err != nil {
		t.Fatal(err)
	}
	_, err = s.Accept(context.Background(), []Request{{Kind: KindReuse, RunID: "other", ID: "1"}}, DefaultLimits(), r)
	wantCode(t, err, CodeNotFound)

	// only image/file may be reused
	rr := Resolvers{Reuse: func(string, string) (Item, string, error) {
		return Item{Kind: KindFileRef, Path: "a"}, "", nil
	}}
	_, err = s.Accept(context.Background(), []Request{{Kind: KindReuse, RunID: "r", ID: "1"}}, DefaultLimits(), rr)
	wantCode(t, err, CodeNotFound)
}

func TestStore_ReadFileRefusesTraversal(t *testing.T) {
	s, root := newTestStore(t)
	set := accept(t, s, Resolvers{}, Request{Kind: KindFile, Name: "a", Data: b64([]byte("x"))})
	if err := os.WriteFile(filepath.Join(root, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"../secret", "..", "a/b", `a\b`, "", ".", "x\x00y", "..secret"} {
		if _, err := s.ReadFile(set.ID, Item{File: f}); err == nil {
			t.Errorf("File %q must be refused", f)
		}
	}
	for _, id := range []string{"", "../x", strings.Repeat("g", 32), strings.Repeat("a", 31), strings.ToUpper(set.ID)} {
		if s.Dir(id) != "" {
			t.Errorf("Dir(%q) must be empty", id)
		}
		if _, err := s.ReadFile(id, set.Items[0]); err == nil {
			t.Errorf("ReadFile with set %q must fail", id)
		}
		if s.Remove(id) == nil {
			t.Errorf("Remove(%q) must fail", id)
		}
	}
}

func TestStore_EmptyRequestList(t *testing.T) {
	s, root := newTestStore(t)
	set, err := s.Accept(context.Background(), nil, DefaultLimits(), Resolvers{})
	if err != nil || len(set.Items) != 0 || len(rootEntries(t, root)) != 0 {
		t.Fatalf("%+v %v", set, err)
	}
}

func TestStore_PathRefusesTraversalAndBadSetID(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	id := strings.Repeat("ab", 16)
	if p, err := st.Path(id, Item{File: "01-a.png"}); err != nil || filepath.Base(p) != "01-a.png" || filepath.Base(filepath.Dir(p)) != id {
		t.Fatalf("Path = %q, %v", p, err)
	}
	for _, f := range []string{"", ".", "../x", "a/b", `a\b`, "a\x00b"} {
		if _, err := st.Path(id, Item{File: f}); err == nil {
			t.Fatalf("Path accepted file name %q", f)
		}
	}
	if _, err := st.Path("../etc", Item{File: "a"}); err == nil {
		t.Fatal("Path accepted a bad set id")
	}
}
