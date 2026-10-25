package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SourceKind says where a plugin came from.
type SourceKind string

const (
	SourceDir     SourceKind = "dir"     // copied from a directory
	SourceLink    SourceKind = "link"    // referenced in place, never copied
	SourceGit     SourceKind = "git"     // a Git commit, pinned by sha
	SourceArchive SourceKind = "archive" // an archive, pinned by sha256
)

var (
	shaRe    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const (
	gitTimeout      = 2 * time.Minute
	downloadTimeout = 2 * time.Minute
)

// Source is a parsed install source. Local paths (Dir, a local archive) come
// only from the Human typing them: nothing scans a workspace for plugins.
type Source struct {
	Kind SourceKind
	// Dir: the directory; Archive: a local path or an https URL; Git: the URL.
	Spec string
	// Git: the 40-hex commit; Archive: the 64-hex sha256.
	Pin string
}

// String is the canonical, pinned form recorded in the plugins table.
func (s Source) String() string {
	switch s.Kind {
	case SourceGit:
		return "git+" + s.Spec + "@" + s.Pin
	case SourceArchive:
		return s.Spec + "#sha256=" + s.Pin
	}
	return s.Spec
}

// ParseSource classifies an install argument. link marks `smind plugin link`,
// which only accepts a directory.
func ParseSource(arg string, link bool) (Source, error) {
	if strings.TrimSpace(arg) == "" || strings.ContainsRune(arg, 0) || strings.ContainsAny(arg, "\r\n") {
		return Source{}, newErr(CodeBadSource, "source is empty or contains control characters")
	}
	if strings.HasPrefix(arg, "git+") {
		if link {
			return Source{}, newErr(CodeBadSource, "link takes a directory, not a Git URL")
		}
		return parseGitSource(strings.TrimPrefix(arg, "git+"))
	}
	if st, err := os.Stat(arg); err == nil && st.IsDir() {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return Source{}, newErr(CodeBadSource, "cannot resolve %q: %v", arg, err)
		}
		if link {
			return Source{Kind: SourceLink, Spec: abs}, nil
		}
		return Source{Kind: SourceDir, Spec: abs}, nil
	}
	if link {
		return Source{}, newErr(CodeBadSource, "%q is not a directory (link takes an existing directory)", arg)
	}
	spec, pin, ok := strings.Cut(arg, "#sha256=")
	if !ok {
		if st, err := os.Stat(arg); err == nil && !st.IsDir() || strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "http://") {
			return Source{}, newErr(CodeUnpinnedArchive, "an archive source must be pinned: append #sha256=<64 hex digits> to %q", arg)
		}
		return Source{}, newErr(CodeBadSource, "%q is not a directory, a git+<url>@<sha> or an <archive>#sha256=<hash>", arg)
	}
	pin = strings.ToLower(pin)
	if !sha256Re.MatchString(pin) {
		return Source{}, newErr(CodeUnpinnedArchive, "the sha256 pin must be 64 hex digits, got %q", pin)
	}
	if strings.HasPrefix(spec, "http://") {
		return Source{}, newErr(CodeBadSource, "archive URLs must be https")
	}
	if strings.HasPrefix(spec, "https://") {
		u, err := url.Parse(spec)
		if err != nil || u.Host == "" || u.User != nil {
			return Source{}, newErr(CodeBadSource, "archive URL is not usable (credentials in a URL are refused)")
		}
	} else if abs, err := filepath.Abs(spec); err == nil {
		spec = abs
	}
	return Source{Kind: SourceArchive, Spec: spec, Pin: pin}, nil
}

func parseGitSource(rest string) (Source, error) {
	i := strings.LastIndex(rest, "@")
	if i < 0 || !shaRe.MatchString(rest[i+1:]) {
		return Source{}, newErr(CodeUnpinnedRef, "a Git source must be pinned to a full 40-hex commit sha: git+<url>@<sha>; a branch, tag or short sha is not accepted")
	}
	spec, pin := rest[:i], rest[i+1:]
	if strings.HasPrefix(spec, "-") {
		return Source{}, newErr(CodeBadSource, "Git URL may not start with '-'")
	}
	u, err := url.Parse(spec)
	if err != nil || u.Scheme == "" {
		return Source{}, newErr(CodeBadSource, "Git URL %q must look like https://host/repo.git or file:///path/repo.git", spec)
	}
	switch u.Scheme {
	case "https", "file":
	default:
		return Source{}, newErr(CodeBadSource, "Git URL scheme %q is not allowed (use https or file: ssh needs credentials, and installs never prompt for or read them)", u.Scheme)
	}
	if u.User != nil {
		if _, hasPw := u.User.Password(); hasPw {
			return Source{}, newErr(CodeBadSource, "credentials in a Git URL are refused (they would land in argv and the database)")
		}
	}
	return Source{Kind: SourceGit, Spec: spec, Pin: pin}, nil
}

// gitEnv is the scrubbed environment git runs in: no inherited credentials or
// config, no prompts, only the two transports a plugin source may use.
func gitEnv(home string) []string {
	nul := os.DevNull
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + nul,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GCM_INTERACTIVE=never",
		"GIT_ALLOW_PROTOCOL=file:https",
		"LC_ALL=C",
	}
	if sr := os.Getenv("SystemRoot"); sr != "" { // git on Windows needs it
		env = append(env, "SystemRoot="+sr)
	}
	return env
}

// fetchGit checks out exactly commit pin of url into dest (an empty
// directory) and verifies HEAD equals the pin. Hooks, submodules and
// credential prompts are all off; the work is bounded by a timeout.
func fetchGit(ctx context.Context, src Source, workDir, dest string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return &Error{Code: CodeGit, Msg: "git is not installed or not on PATH", Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	home := filepath.Join(workDir, "githome")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	hooks := filepath.Join(workDir, "nohooks")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		return err
	}
	base := []string{
		"-c", "core.hooksPath=" + hooks,
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "protocol.file.allow=always",
		"-c", "submodule.recurse=false",
		"-c", "core.fsmonitor=false",
		"-c", "advice.detachedHead=false",
		"-C", dest,
	}
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append(append([]string{}, base...), args...)...)
		cmd.Env = gitEnv(home)
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return "", &Error{Code: CodeGit, Msg: fmt.Sprintf("git %s timed out after %s", args[0], gitTimeout), Err: ctx.Err()}
		}
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return "", &Error{Code: CodeGit, Msg: fmt.Sprintf("git %s failed: %s", args[0], msg), Err: err}
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := run("init", "-q"); err != nil {
		return err
	}
	// "--" and a url that cannot start with '-' keep the URL from becoming an option.
	if _, err := run("fetch", "-q", "--depth", "1", "--no-tags", "--no-recurse-submodules", "--", src.Spec, src.Pin); err != nil {
		return err
	}
	if _, err := run("checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	head, err := run("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if !strings.EqualFold(head, src.Pin) {
		return newErr(CodeGit, "checked-out commit %s does not equal the pin %s", head, src.Pin)
	}
	return nil
}

// fetchArchive copies the archive named by src into dst, hashing the bytes as
// they are copied, and fails with hash_mismatch if they differ from the pin.
// The caller extracts from dst, i.e. from the very bytes that were hashed, so
// the file cannot change between the check and the extraction.
func fetchArchive(ctx context.Context, src Source, dst string) error {
	var r io.Reader
	if strings.HasPrefix(src.Spec, "https://") {
		ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.Spec, nil)
		if err != nil {
			return newErr(CodeBadSource, "bad archive URL: %v", err)
		}
		client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" {
				return errors.New("redirect to a non-https URL refused")
			}
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			return nil
		}}
		resp, err := client.Do(req)
		if err != nil {
			return &Error{Code: CodeBadSource, Msg: "download failed", Err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return newErr(CodeBadSource, "download failed: HTTP %d", resp.StatusCode)
		}
		r = resp.Body
	} else {
		// Stat before Open: opening a FIFO would block forever.
		if st, err := os.Stat(src.Spec); err != nil || !st.Mode().IsRegular() {
			return newErr(CodeBadSource, "%s is not a regular file", src.Spec)
		}
		f, err := os.Open(src.Spec)
		if err != nil {
			return &Error{Code: CodeBadSource, Msg: "cannot open archive", Err: err}
		}
		defer f.Close()
		r = f
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, MaxArchiveBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return &Error{Code: CodeBadSource, Msg: "reading the archive failed", Err: err}
	}
	if n > MaxArchiveBytes {
		return newErr(CodeTooLarge, "archive is over the %d byte cap", MaxArchiveBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != src.Pin {
		return newErr(CodeHashMismatch, "archive sha256 is %s but the pin is %s; nothing was extracted", got, src.Pin)
	}
	return nil
}
