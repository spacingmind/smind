package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ChecksumVerifier verifies the downloaded checksums.txt of release `version`
// (see SignatureVerifier, ADR-0031 decision 1) BEFORE any digest in it is
// trusted, fetching sibling release assets (checksums.txt.minisig) through
// fetch. A non-nil error aborts the update with nothing changed. Without a
// verifier (nil) checksums alone detect corruption but not a swapped archive
// plus a swapped checksums.txt; cmd/smind always installs one.
type ChecksumVerifier func(ctx context.Context, version string, checksums []byte, fetch func(ctx context.Context, asset string) ([]byte, error)) error

// Options are the `smind update` flags.
type Options struct {
	Version string // exact target ("" = latest stable)
	Force   bool   // override the active-work and downgrade refusals
	DryRun  bool   // do everything except the swap
}

// Updater carries every collaborator; the zero value is not usable, see
// cmd/smind/update.go for the production wiring.
type Updater struct {
	Source  *Source
	Current string // running version, "dev" for an unstamped build
	Exe     string // resolved path of the running binary (symlinks followed)
	GOOS    string
	GOARCH  string

	Daemon Daemon
	// ActiveWork reports running runs. An error means "a daemon is there but
	// could not be asked"; the update then refuses unless forced.
	ActiveWork func(ctx context.Context) (Activity, error)
	// RunVersion executes a binary's `version` and returns the version it
	// reports. nil = really execute it.
	RunVersion func(ctx context.Context, exe string) (string, error)
	// VerifyChecksums authenticates checksums.txt; production wiring in
	// cmd/smind sets SignatureVerifier(UpdaterPublicKey).
	VerifyChecksums ChecksumVerifier

	Getenv      func(string) string
	Exists      func(string) bool
	InContainer func() bool // nil = InContainer(GOOS, Getenv, Exists)

	// HealthTimeout bounds the wait for a restarted daemon to report the new
	// version (30 s in production).
	HealthTimeout time.Duration
	HealthPoll    time.Duration
	// Aside selects rename-aside swapping; nil means RenameAside (GOOS-based).
	Aside *bool

	Log io.Writer // progress lines; nil = discard
}

func (u *Updater) logf(format string, a ...any) {
	if u.Log != nil {
		fmt.Fprintf(u.Log, format+"\n", a...)
	}
}

func (u *Updater) defaults() {
	if u.GOOS == "" {
		u.GOOS = runtime.GOOS
	}
	if u.GOARCH == "" {
		u.GOARCH = runtime.GOARCH
	}
	if u.Getenv == nil {
		u.Getenv = os.Getenv
	}
	if u.Exists == nil {
		u.Exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	if u.RunVersion == nil {
		u.RunVersion = execVersion
	}
	if u.HealthTimeout == 0 {
		u.HealthTimeout = 30 * time.Second
	}
	if u.HealthPoll == 0 {
		u.HealthPoll = 500 * time.Millisecond
	}
}

func (u *Updater) aside() bool {
	if u.Aside != nil {
		return *u.Aside
	}
	return RenameAside
}

// CheckResult is `smind update --check`.
type CheckResult struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// Check resolves the latest stable release and compares it to the running
// version. A dev build is never "updatable" (Update refuses it).
func (u *Updater) Check(ctx context.Context) (CheckResult, error) {
	u.defaults()
	latest, err := u.Source.LatestVersion(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	res := CheckResult{Current: u.Current, Latest: latest}
	if IsDevBuild(u.Current) {
		return res, nil // a dev build is never updatable; Update refuses it
	}
	c, err := CompareVersions(u.Current, latest)
	if err != nil {
		return res, fmt.Errorf("compare %q with %s: %w", u.Current, latest, err)
	}
	res.UpdateAvailable = c < 0
	return res, nil
}

// UpdateResult is the outcome of a successful (or dry-run) Update.
type UpdateResult struct {
	Current string `json:"current"`
	Target  string `json:"target"`
	// Updated is true once the new binary is installed; false for a no-op
	// ("already up to date") and for a dry run.
	Updated bool `json:"updated"`
	DryRun  bool `json:"dryRun"`
	// DaemonRestarted is true when a service-managed daemon was restarted onto
	// the new version.
	DaemonRestarted bool `json:"daemonRestarted"`
	// DaemonNeedsRestart is true when a hand-started daemon keeps running the
	// old binary; the printed instructions say how to restart it.
	DaemonNeedsRestart bool `json:"daemonNeedsRestart"`
}

// Update runs the whole flow. A *Refusal error means a guard declined and
// nothing changed; any other error also leaves the installed binary
// byte-identical, except where it says a rollback itself failed.
func (u *Updater) Update(ctx context.Context, opts Options) (UpdateResult, error) {
	u.defaults()
	res := UpdateResult{Current: u.Current, DryRun: opts.DryRun}
	dir := filepath.Dir(u.Exe)

	// Guards that need no network come first so a refusal is instant.
	if IsDevBuild(u.Current) {
		return res, refuse("this is a %q build, not a release; install a release with the install script to use `smind update`", u.Current)
	}
	if reason, owned := DesktopOwned(u.Exe, u.GOOS, u.Getenv, u.Exists); owned {
		return res, refuse("%s (%s)", reason, u.Exe)
	}
	inContainer := InContainer
	if u.InContainer != nil {
		inContainer = func(string, func(string) string, func(string) bool) bool { return u.InContainer() }
	}
	if inContainer(u.GOOS, u.Getenv, u.Exists) {
		return res, refuse("running inside a container; update by pulling a new image instead")
	}
	if err := CheckWritable(dir); err != nil {
		return res, err
	}
	if u.aside() {
		CleanupStale(u.Exe) // an earlier update's still-locked .old, now deletable
	}

	target := NormalizeVersion(opts.Version)
	if target == "" {
		v, err := u.Source.LatestVersion(ctx)
		if err != nil {
			return res, err
		}
		target = v
	} else if !ValidVersion(target) {
		return res, refuse("%q is not a valid version (want e.g. 0.12.0)", opts.Version)
	}
	res.Target = target

	cmp, err := CompareVersions(u.Current, target)
	if err != nil {
		return res, err
	}
	switch {
	case cmp == 0 && !opts.Force:
		u.logf("smind %s is already up to date", u.Current)
		return res, nil
	case cmp > 0 && !opts.Force:
		return res, refuse("%s is older than the installed %s; a downgrade can strand data migrated forward — pass --force to downgrade anyway", target, u.Current)
	}

	checkActive := func() error {
		if opts.Force {
			return nil
		}
		act, err := u.ActiveWork(ctx)
		switch {
		case err != nil:
			return refuse("a daemon is running but could not be asked whether work is active (%v); pass --force to update anyway", err)
		case act.RunningRuns > 0:
			return refuse("%d run(s) are running (a run waiting on a permission counts); let them finish or pass --force", act.RunningRuns)
		}
		return nil
	}
	// Checked first so a refusal costs no download, and again just before the
	// swap: the download can take minutes, during which a run may have started.
	if err := checkActive(); err != nil {
		return res, err
	}

	staged, cleanup, err := u.download(ctx, target, dir)
	if err != nil {
		return res, err
	}
	defer cleanup()

	got, err := u.RunVersion(ctx, staged)
	if err != nil {
		return res, fmt.Errorf("the downloaded binary does not run: %w", err)
	}
	if NormalizeVersion(got) != target {
		return res, fmt.Errorf("the downloaded binary reports version %q, expected %s; not installing", got, target)
	}

	if opts.DryRun {
		u.logf("dry run: smind %s verified; would replace %s (%s -> %s)", target, u.Exe, u.Current, target)
		return res, nil
	}

	status, err := u.Daemon.Status(ctx)
	if err != nil && !opts.Force {
		return res, refuse("could not tell whether a daemon is running (%v); pass --force to update anyway", err)
	}
	if err := checkActive(); err != nil {
		return res, err
	}

	if err := Swap(u.Exe, staged, u.aside()); err != nil {
		return res, err
	}
	rollback := func(cause error) error {
		if rerr := Restore(u.Exe, u.aside()); rerr != nil {
			kept := Preserve(u.Exe)
			return fmt.Errorf("%w; ROLLBACK FAILED, the previous binary is kept at %s: %v", cause, kept, rerr)
		}
		return fmt.Errorf("%w; rolled back to smind %s", cause, u.Current)
	}

	if post, err := u.RunVersion(ctx, u.Exe); err != nil {
		return res, rollback(fmt.Errorf("the installed binary does not run: %w", err))
	} else if NormalizeVersion(post) != target {
		return res, rollback(fmt.Errorf("the installed binary reports version %q, expected %s", post, target))
	}
	res.Updated = true

	switch {
	case status.Running && status.Managed:
		u.logf("restarting the service-managed daemon...")
		if err := u.restartAndWait(ctx, target); err != nil {
			res.Updated = false
			cause := rollback(err)
			// Bring the old version back up; best effort, its error is secondary.
			// A Ctrl-C that aborted the health wait must not also abort this.
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), u.HealthTimeout)
			defer cancel()
			if rerr := u.Daemon.Restart(rctx, u.Exe); rerr != nil {
				cause = fmt.Errorf("%w (restarting the old daemon also failed: %v)", cause, rerr)
			}
			return res, cause
		}
		res.DaemonRestarted = true
		u.logf("daemon is healthy on smind %s", target)
	case status.Running:
		res.DaemonNeedsRestart = true
	}

	Discard(u.Exe)
	u.logf("updated smind %s -> %s (%s)", u.Current, target, u.Exe)
	if res.DaemonNeedsRestart {
		u.logf("the running daemon is still smind %s; restart it with `smind stop && smind serve` to use %s", status.Version, target)
	}
	return res, nil
}

func (u *Updater) restartAndWait(ctx context.Context, target string) error {
	if err := u.Daemon.Restart(ctx, u.Exe); err != nil {
		return fmt.Errorf("restart the service: %w", err)
	}
	deadline := time.Now().Add(u.HealthTimeout)
	var last string
	for {
		st, err := u.Daemon.Status(ctx)
		if err == nil && st.Running {
			last = st.Version
			if NormalizeVersion(st.Version) == target {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if last == "" {
				return fmt.Errorf("the daemon did not come back within %s", u.HealthTimeout)
			}
			return fmt.Errorf("the daemon still reports version %s after %s, wanted %s", last, u.HealthTimeout, target)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(u.HealthPoll):
		}
	}
}

// download fetches and verifies the archive for target and stages the binary
// in dir (same filesystem as the install, so the swap is a rename). The
// returned cleanup removes the staged file and the download directory; after a
// successful Swap the staged path no longer exists and removal is a no-op.
func (u *Updater) download(ctx context.Context, target, dir string) (stagedPath string, cleanupFn func(), err error) {
	name := AssetName(target, u.GOOS, u.GOARCH)
	tmp, err := os.MkdirTemp("", "smind-update-")
	if err != nil {
		return "", nil, err
	}
	staged := filepath.Join(dir, fmt.Sprintf(".smind.new.%d%s", os.Getpid(), exeSuffix(u.GOOS)))
	cleanup := func() {
		_ = os.RemoveAll(tmp)
		_ = os.Remove(staged)
	}
	// The error returns below assign the named results (nil cleanup), so the
	// deferred cleanup must use the locals, not the results.
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	u.logf("downloading %s ...", name)
	checksums, err := u.Source.Fetch(ctx, target, ChecksumsName, maxChecksumsSize)
	if err != nil {
		return "", nil, fmt.Errorf("%w (does release v%s exist?)", err, target)
	}
	if u.VerifyChecksums != nil {
		fetch := func(ctx context.Context, asset string) ([]byte, error) {
			return u.Source.Fetch(ctx, target, asset, maxChecksumsSize)
		}
		if err := u.VerifyChecksums(ctx, target, checksums, fetch); err != nil {
			return "", nil, fmt.Errorf("verify the signature of %s: %w", ChecksumsName, err)
		}
	}
	want, err := ExpectedChecksum(checksums, name)
	if err != nil {
		return "", nil, fmt.Errorf("%w (release v%s has no build for %s/%s?)", err, target, u.GOOS, u.GOARCH)
	}
	archive := filepath.Join(tmp, name)
	got, err := u.Source.DownloadFile(ctx, target, name, archive, maxArchiveSize)
	if err != nil {
		if errors.Is(err, ErrAssetNotFound) {
			return "", nil, fmt.Errorf("%w (release v%s has no build for %s/%s?)", err, target, u.GOOS, u.GOARCH)
		}
		return "", nil, err
	}
	if got != want {
		return "", nil, fmt.Errorf("%w for %s: expected %s, downloaded %s", ErrChecksumMismatch, name, want, got)
	}
	u.logf("checksum verified (sha256 %s)", got)

	if err := ExtractBinary(archive, BinaryName(u.GOOS), staged, maxBinarySize); err != nil {
		return "", nil, fmt.Errorf("extract %s: %w", name, err)
	}
	// Installed binaries are world-executable (install.sh does chmod 755); do
	// not let the extractor's umask-filtered mode lock other users out.
	if err := os.Chmod(staged, 0o755); err != nil {
		return "", nil, err
	}
	return staged, cleanup, nil
}

func exeSuffix(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// execVersion runs `<exe> version` and parses "smind <version> [(<commit>)]".
func execVersion(ctx context.Context, exe string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, exe, "version")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	f := strings.Fields(out.String())
	if len(f) < 2 || f[0] != "smind" {
		return "", fmt.Errorf("unexpected `version` output %q", strings.TrimSpace(out.String()))
	}
	return f[1], nil
}
