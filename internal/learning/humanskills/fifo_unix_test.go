//go:build unix

package humanskills

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHumanSkills_FIFOSkillSkipped(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "fifo-skill")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(bad, "SKILL.md"), 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	good := filepath.Join(root, "good")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "SKILL.md"), []byte("---\nname: good\ndescription: Use when good.\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	type result struct {
		skills []Skill
		err    error
	}
	done := make(chan result, 1)
	go func() {
		s, err := LoadDirs(root)
		done <- result{s, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || len(r.skills) != 1 || r.skills[0].Name != "good" {
			t.Errorf("skills %+v err %v, want only good", r.skills, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loader hung on a FIFO SKILL.md")
	}
}
