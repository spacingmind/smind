//go:build windows

package realpath

import (
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"
)

// volumeNameDOS (with FILE_NAME_NORMALIZED, also 0) asks
// GetFinalPathNameByHandle for a drive-letter path; x/sys/windows doesn't
// export it.
const volumeNameDOS = 0x0

func resolve(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", &fs.PathError{Op: "realpath", Path: path, Err: err}
	}
	// No data access needed -- attributes only, so files the caller can't
	// read still resolve. BACKUP_SEMANTICS opens directories too.
	h, err := windows.CreateFile(p,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		// ERROR_FILE_NOT_FOUND / ERROR_PATH_NOT_FOUND match fs.ErrNotExist.
		return "", &fs.PathError{Op: "realpath", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameDOS)
		if err != nil {
			return "", &fs.PathError{Op: "realpath", Path: path, Err: err}
		}
		if n < uint32(len(buf)) {
			return stripVerbatim(windows.UTF16ToString(buf[:n])), nil
		}
		buf = make([]uint16, n) // n is the required size, including the NUL
	}
}

// stripVerbatim turns GetFinalPathNameByHandle's `\\?\C:\x` into `C:\x`
// and `\\?\UNC\server\share` into `\\server\share`, the forms the rest of
// the daemon (and filepath.Rel/HasPrefix checks) compare against.
func stripVerbatim(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	return strings.TrimPrefix(p, `\\?\`)
}
