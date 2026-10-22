package attach

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spacingmind/smind/internal/securefile"
)

var setIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Store keeps attachment sets under root, one directory per set.
type Store struct{ root string }

// NewStore returns a store rooted at root ($SMIND_HOME/attachments). The
// directory is created lazily, mode 0700.
func NewStore(root string) *Store { return &Store{root: root} }

// Dir returns the directory of a set, or "" when setID is not a valid id.
func (s *Store) Dir(setID string) string {
	if !setIDPattern.MatchString(setID) {
		return ""
	}
	return filepath.Join(s.root, setID)
}

// Accept validates every request and, only if all pass, writes the set to
// disk. A rejected Accept leaves nothing behind.
func (s *Store) Accept(ctx context.Context, reqs []Request, lim Limits, r Resolvers) (Set, error) {
	ps, err := validate(ctx, reqs, lim, r)
	if err != nil {
		return Set{}, err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Set{}, fmt.Errorf("attach: set id: %w", err)
	}
	set := Set{ID: hex.EncodeToString(raw[:]), Items: make([]Item, len(ps))}
	if len(ps) == 0 {
		return Set{}, nil
	}
	dir := s.Dir(set.ID)
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return Set{}, fmt.Errorf("attach: create root: %w", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Set{}, fmt.Errorf("attach: create set: %w", err)
	}
	for i, p := range ps {
		it := p.item
		nn := fmt.Sprintf("%02d", i+1)
		var data []byte
		switch {
		case p.blob != nil:
			it.File = nn + "-" + SafeName(it.Name, lim.MaxNameBytes)
			data = p.blob
		case p.body != nil:
			it.File = nn + "-body.md"
			data = p.body
		}
		if it.File != "" {
			if err := writeNew(filepath.Join(dir, it.File), data); err != nil {
				_ = os.RemoveAll(dir)
				return Set{}, fmt.Errorf("attach: write %s: %w", it.File, err)
			}
		}
		set.Items[i] = it
	}
	return set, nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return securefile.Restrict(path)
}

// Path returns the absolute path of an item's stored file. Set ids that are
// not 32 hex chars and file names with separators, ".." or NUL are refused,
// so a corrupted row can never point outside the set.
func (s *Store) Path(setID string, it Item) (string, error) {
	dir := s.Dir(setID)
	if dir == "" {
		return "", errors.New("attach: invalid set id")
	}
	if it.File == "" || it.File == "." || strings.Contains(it.File, "..") || strings.ContainsAny(it.File, `/\`+"\x00") {
		return "", errors.New("attach: invalid file name")
	}
	return filepath.Join(dir, it.File), nil
}

// ReadFile reads the stored file of an item (see Path for what is refused).
func (s *Store) ReadFile(setID string, it Item) ([]byte, error) {
	p, err := s.Path(setID, it)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Remove deletes a whole set; a missing set is not an error.
func (s *Store) Remove(setID string) error {
	dir := s.Dir(setID)
	if dir == "" {
		return errors.New("attach: invalid set id")
	}
	return os.RemoveAll(dir)
}
