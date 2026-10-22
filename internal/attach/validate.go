package attach

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"  // register decoders for image.DecodeConfig
	_ "image/jpeg" // register decoders for image.DecodeConfig
	_ "image/png"  // register decoders for image.DecodeConfig
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const truncatedSuffix = "\n[… truncated]"

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// GithubSnapshot is what a Github resolver returns.
type GithubSnapshot struct {
	Number                                          int
	Repo, Title, URL, State, BaseRef, HeadRef, Body string
}

// Resolvers are the three non-byte kinds; each may be nil.
type Resolvers struct {
	// FileRef maps a client path to a workspace-relative one.
	FileRef func(path string) (rel string, isDir bool, err error)
	// Github fetches PR/issue context. Called during validation.
	Github func(ctx context.Context, kind string, number int, repo string) (GithubSnapshot, error)
	// Reuse returns the source item and the ABSOLUTE source file path.
	Reuse func(runID, id string) (Item, string, error)
}

// prepared is one validated attachment, ready to be written.
type prepared struct {
	item Item
	blob []byte // image/file/reuse bytes
	body []byte // github body, written as NN-body.md
}

var allowedImages = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

func decodeBase64(index int, data string, maxDecoded int) ([]byte, *Error) {
	if data == "" {
		return nil, newErr(CodeInvalid, index, "data is required")
	}
	if strings.HasPrefix(data, "data:") {
		return nil, newErr(CodeInvalidBase64, index, "data must be raw base64, not a data: URL")
	}
	if strings.ContainsAny(data, " \t\r\n") {
		return nil, newErr(CodeInvalidBase64, index, "data must not contain whitespace")
	}
	if base64.StdEncoding.DecodedLen(len(data)) > maxDecoded+3 {
		return nil, newErr(CodeTooLarge, index, "attachment exceeds %d bytes", maxDecoded)
	}
	b, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil {
		return nil, newErr(CodeInvalidBase64, index, "data is not valid base64")
	}
	return b, nil
}

func sniffImage(b []byte) string {
	t := http.DetectContentType(b)
	if t == "application/octet-stream" && len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp"
	}
	return t
}

func le16(b []byte) int { return int(b[0]) | int(b[1])<<8 }
func le24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func webpConfig(b []byte) (w, h int, ok bool) {
	if len(b) < 16 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(b[12:16]) {
	case "VP8 ":
		if len(b) < 30 {
			return 0, 0, false
		}
		return le16(b[26:28]) & 0x3fff, le16(b[28:30]) & 0x3fff, true
	case "VP8L":
		if len(b) < 25 {
			return 0, 0, false
		}
		v := le32(b[21:25])
		return 1 + int(v&0x3fff), 1 + int((v>>14)&0x3fff), true
	case "VP8X":
		if len(b) < 30 {
			return 0, 0, false
		}
		return 1 + le24(b[24:27]), 1 + le24(b[27:30]), true
	}
	return 0, 0, false
}

// validateImageBytes sniffs, type-checks and measures image bytes. declared
// is the client's MimeType; reuse passes the stored type instead.
func validateImageBytes(index int, b []byte, declared string, lim Limits) (mime string, w, h int, err *Error) {
	mime = sniffImage(b)
	if !allowedImages[mime] {
		return "", 0, 0, newErr(CodeUnsupportedImage, index, "unsupported image type %q (allowed: png, jpeg, gif, webp)", mime)
	}
	if declared != mime {
		return "", 0, 0, newErr(CodeMimeMismatch, index, "declared type %q does not match content %q", declared, mime)
	}
	if mime == "image/webp" {
		var ok bool
		if w, h, ok = webpConfig(b); !ok {
			return "", 0, 0, newErr(CodeInvalid, index, "cannot read image dimensions")
		}
	} else {
		cfg, _, derr := image.DecodeConfig(bytes.NewReader(b))
		if derr != nil {
			return "", 0, 0, newErr(CodeInvalid, index, "cannot read image dimensions")
		}
		w, h = cfg.Width, cfg.Height
	}
	if w <= 0 || h <= 0 {
		return "", 0, 0, newErr(CodeInvalid, index, "image has no dimensions")
	}
	if w > lim.MaxImageDim || h > lim.MaxImageDim {
		return "", 0, 0, newErr(CodeTooLarge, index, "image is %dx%d, larger than %d px", w, h, lim.MaxImageDim)
	}
	return mime, w, h, nil
}

func fileMime(b []byte, declared string) string {
	sniffed := http.DetectContentType(b)
	base, _, _ := strings.Cut(sniffed, ";")
	if (base == "text/plain" || base == "application/octet-stream") && declared != "" && !strings.Contains(declared, ";") {
		return declared
	}
	return sniffed
}

func clipBody(body string, max int) string {
	if max <= 0 || len(body) <= max {
		return body
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut] + truncatedSuffix
}

// validate checks every request in order without writing anything. Only the
// resolvers may touch the outside world.
func validate(ctx context.Context, reqs []Request, lim Limits, r Resolvers) ([]prepared, error) {
	if len(reqs) > lim.MaxCount {
		return nil, newErr(CodeTooMany, -1, "at most %d attachments per prompt", lim.MaxCount)
	}
	out := make([]prepared, 0, len(reqs))
	var total int64
	var refs, gh int
	addBytes := func(i int, n int) *Error {
		total += int64(n)
		if total > int64(lim.MaxTotalBytes) {
			return newErr(CodeTooLarge, i, "attachments exceed %d bytes in total", lim.MaxTotalBytes)
		}
		return nil
	}
	for i, req := range reqs {
		id := strconv.Itoa(i + 1)
		p := prepared{item: Item{ID: id, Kind: req.Kind}}
		switch req.Kind {
		case KindImage:
			b, e := decodeBase64(i, req.Data, lim.MaxImageBytes)
			if e != nil {
				return nil, e
			}
			if len(b) > lim.MaxImageBytes {
				return nil, newErr(CodeTooLarge, i, "image exceeds %d bytes", lim.MaxImageBytes)
			}
			mime, w, h, e := validateImageBytes(i, b, req.MimeType, lim)
			if e != nil {
				return nil, e
			}
			if e := addBytes(i, len(b)); e != nil {
				return nil, e
			}
			p.item.Name = SafeName(req.Name, lim.MaxNameBytes)
			p.item.MimeType, p.item.Size, p.item.Width, p.item.Height = mime, int64(len(b)), w, h
			p.blob = b
		case KindFile:
			b, e := decodeBase64(i, req.Data, lim.MaxFileBytes)
			if e != nil {
				return nil, e
			}
			if len(b) > lim.MaxFileBytes {
				return nil, newErr(CodeTooLarge, i, "file exceeds %d bytes", lim.MaxFileBytes)
			}
			if e := addBytes(i, len(b)); e != nil {
				return nil, e
			}
			p.item.Name = SafeName(req.Name, lim.MaxNameBytes)
			p.item.MimeType, p.item.Size = fileMime(b, req.MimeType), int64(len(b))
			p.blob = b
		case KindFileRef:
			if refs++; refs > lim.MaxFileRefs {
				return nil, newErr(CodeTooMany, i, "at most %d file references", lim.MaxFileRefs)
			}
			if req.Path == "" || len(req.Path) > lim.MaxPathLen {
				return nil, newErr(CodeInvalid, i, "path is empty or longer than %d bytes", lim.MaxPathLen)
			}
			if r.FileRef == nil {
				return nil, newErr(CodeInvalid, i, "file references are not supported here")
			}
			rel, isDir, err := r.FileRef(req.Path)
			if err != nil {
				var ae *Error
				if errors.As(err, &ae) {
					cp := *ae
					cp.Index = i
					return nil, &cp
				}
				return nil, newErr(CodeOutsideWorkspace, i, "%v", err)
			}
			p.item.Path, p.item.IsDir = rel, isDir
		case KindGithubPR, KindGithubIssue:
			if req.Number <= 0 {
				return nil, newErr(CodeInvalid, i, "number must be positive")
			}
			if req.Repo != "" && !repoPattern.MatchString(req.Repo) {
				return nil, newErr(CodeInvalid, i, "repo must look like owner/name")
			}
			if gh++; gh > lim.MaxGithub {
				return nil, newErr(CodeTooMany, i, "at most %d GitHub attachments", lim.MaxGithub)
			}
			if r.Github == nil {
				return nil, newErr(CodeGithubUnavailable, i, "GitHub is not available")
			}
			snap, err := r.Github(ctx, req.Kind, req.Number, req.Repo)
			if err != nil {
				var ae *Error
				if errors.As(err, &ae) {
					cp := *ae
					cp.Index = i
					return nil, &cp
				}
				return nil, newErr(CodeGithubUnavailable, i, "%v", err)
			}
			repo := snap.Repo
			if repo == "" {
				repo = req.Repo
			}
			num := snap.Number
			if num == 0 {
				num = req.Number
			}
			p.item.Number, p.item.Repo, p.item.Title = num, repo, snap.Title
			p.item.URL, p.item.State, p.item.BaseRef, p.item.HeadRef = snap.URL, snap.State, snap.BaseRef, snap.HeadRef
			if snap.Body != "" {
				p.body = []byte(clipBody(snap.Body, lim.MaxBodyBytes))
			}
		case KindReuse:
			if r.Reuse == nil {
				return nil, newErr(CodeInvalid, i, "reusing attachments is not supported here")
			}
			src, path, err := r.Reuse(req.RunID, req.ID)
			if err != nil || (src.Kind != KindImage && src.Kind != KindFile) {
				return nil, newErr(CodeNotFound, i, "attachment to reuse not found")
			}
			limit := lim.MaxFileBytes
			if src.Kind == KindImage {
				limit = lim.MaxImageBytes
			}
			fi, serr := os.Stat(path)
			if serr != nil || !fi.Mode().IsRegular() {
				return nil, newErr(CodeNotFound, i, "attachment to reuse not found")
			}
			if fi.Size() > int64(limit) {
				return nil, newErr(CodeTooLarge, i, "attachment exceeds %d bytes", limit)
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil, newErr(CodeNotFound, i, "attachment to reuse not found")
			}
			if e := addBytes(i, len(b)); e != nil {
				return nil, e
			}
			p.item = src
			p.item.ID, p.item.File, p.item.Kind = id, "", src.Kind
			p.item.Size = int64(len(b))
			p.blob = b
		default:
			return nil, newErr(CodeInvalid, i, "unknown attachment kind %q", req.Kind)
		}
		out = append(out, p)
	}
	return out, nil
}
