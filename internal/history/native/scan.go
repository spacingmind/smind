package native

import (
	"bufio"
	"context"
	"errors"
	"io"
)

// Bounds of one rebuild (ADR-0053 D4).
const (
	// MaxFileBytes caps the session file read: nothing bigger is opened.
	MaxFileBytes = 64 << 20
	// maxLineBytes caps one record; a longer line is a malformed session.
	maxLineBytes = 16 << 20
)

var (
	errTooLarge = errors.New("native: session record or output over the size bound")
	errTimeout  = errors.New("native: rebuild timed out")
)

// scanLines calls fn for every non-empty line of r, stopping at ctx's end and
// refusing a line over maxLineBytes. The slice passed to fn is only valid during
// the call.
func scanLines(ctx context.Context, r io.Reader, fn func(line []byte) error) error {
	br := bufio.NewReaderSize(r, 256<<10)
	var buf []byte
	for n := 0; ; n++ {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
				return errTimeout
			}
		}
		part, isPrefix, err := br.ReadLine()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		buf = append(buf[:0], part...)
		for isPrefix {
			part, isPrefix, err = br.ReadLine()
			if err != nil {
				return err
			}
			if len(buf)+len(part) > maxLineBytes {
				return errTooLarge
			}
			buf = append(buf, part...)
		}
		if len(buf) == 0 {
			continue
		}
		if err := fn(buf); err != nil {
			return err
		}
	}
}
