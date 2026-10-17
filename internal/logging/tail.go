package logging

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"
)

// TailFile writes the last n lines of the file at path to w and returns the
// offset it read up to, to hand to Follow. A missing file is an error the
// caller can test with errors.Is(err, fs.ErrNotExist). n <= 0 writes nothing
// but still returns the current end of the file.
func TailFile(path string, n int, w io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	end := fi.Size()
	if n <= 0 || end == 0 {
		return end, nil
	}

	const chunk = 64 * 1024
	var buf []byte
	pos := end
	// Scan backwards until the buffer holds more than n newlines (a trailing
	// newline ends the last line, so n lines need n+1 newlines unless the file
	// starts mid-buffer) or the start of the file is reached.
	for pos > 0 {
		size := int64(chunk)
		if pos < size {
			size = pos
		}
		pos -= size
		part := make([]byte, size)
		if _, err := f.ReadAt(part, pos); err != nil && err != io.EOF {
			return 0, err
		}
		buf = append(part, buf...)
		if bytes.Count(buf, []byte{'\n'}) > n {
			break
		}
	}
	// Drop the (possibly partial) first line unless the whole file is in buf.
	if pos > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	lines := bytes.SplitAfter(buf, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, l := range lines {
		if _, err := w.Write(l); err != nil {
			return 0, err
		}
	}
	return end, nil
}

// followPoll is how often Follow checks the file for new bytes or rotation.
var followPoll = 250 * time.Millisecond

// Follow copies bytes appended to the file at path to w, starting at offset,
// until ctx is done. It survives rotation: when path now names a different
// file it first drains what is left of the old one, then continues with the
// new file from its start. A missing file is waited for.
func Follow(ctx context.Context, path string, w io.Writer, offset int64) error {
	var f *os.File
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	open := func(off int64) {
		nf, err := os.Open(path)
		if err != nil {
			return
		}
		if _, err := nf.Seek(off, io.SeekStart); err != nil {
			_ = nf.Close()
			return
		}
		f = nf
	}
	open(offset)
	ticker := time.NewTicker(followPoll)
	defer ticker.Stop()
	wait := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			return true
		}
	}
	for {
		if f == nil {
			open(0)
			if f == nil {
				if !wait() {
					return nil
				}
				continue
			}
		}
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		pos, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		cur, err1 := f.Stat()
		now, err2 := os.Stat(path)
		switch {
		case err2 != nil:
			// path is gone (mid-rotation): keep the old handle, retry.
		case err1 == nil && !os.SameFile(cur, now):
			// Rotated: drain whatever landed in the old file between the copy
			// above and the rotation, then start the new one.
			if _, err := io.Copy(w, f); err != nil {
				return err
			}
			_ = f.Close()
			f = nil
			open(0)
			continue
		case now.Size() < pos:
			// Truncated in place: start over from the top.
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			continue
		}
		if !wait() {
			return nil
		}
	}
}
