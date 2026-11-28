package gitsafe

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

// The stripped-mode tests drive the config scan by replacing the package
// variable scanConfig with a stub returning canned records, so they must
// not run in parallel with each other: none of them calls t.Parallel.

// scanRec is one (scope, origin, key, value) tuple of canned scan output.
type scanRec struct {
	scope, origin, key, value string
}

// scanBytes encodes records the way "git config --show-scope --show-origin
// --null --get-regexp" prints them: scope, origin and "key\nvalue", each
// field NUL-terminated (a key with no newline has the empty value).
func scanBytes(recs ...scanRec) []byte {
	var b bytes.Buffer
	for _, r := range recs {
		b.WriteString(r.scope)
		b.WriteByte(0)
		b.WriteString(r.origin)
		b.WriteByte(0)
		b.WriteString(r.key)
		if r.value != "" {
			b.WriteByte('\n')
			b.WriteString(r.value)
		}
		b.WriteByte(0)
	}
	return b.Bytes()
}

// stubScan replaces scanConfig with a stub returning recs (an error when
// err is not nil) and counting calls; the original is restored when the
// test ends.
func stubScan(t *testing.T, err error, recs ...scanRec) *int {
	t.Helper()
	calls := 0
	orig := scanConfig
	scanConfig = func(ctx context.Context, dir string) ([]byte, error) {
		calls++
		if err != nil {
			return nil, err
		}
		return scanBytes(recs...), nil
	}
	t.Cleanup(func() { scanConfig = orig })
	return &calls
}

// staticPrefix is the leading argv every stripped call carries.
var staticPrefix = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=" + os.DevNull,
	"-c", "commit.gpgsign=false",
	"-c", "tag.gpgsign=false",
}

// wantArgv builds an expected argv: the static prefix, then tail.
func wantArgv(tail ...string) []string {
	return append(append([]string{}, staticPrefix...), tail...)
}

// assertArgv fails when StrippedArgs errored or returned anything but want.
func assertArgv(t *testing.T, got []string, err error, want []string) {
	t.Helper()
	if err != nil {
		t.Fatalf("StrippedArgs: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StrippedArgs\n got %q\nwant %q", got, want)
	}
}

// Test_stripped_args_static_prefix covers the shape of a call with an
// empty scan: the baseline fsmonitor pair, then the three static stripped
// overrides, then the input; a diff-family subcommand still carries its
// two flags right after the verb; the input slice is never modified.
func Test_stripped_args_static_prefix(t *testing.T) {
	stubScan(t, nil) // no config entries at all

	got, err := StrippedArgs(context.Background(), "", "status")
	assertArgv(t, got, err, wantArgv("status"))

	in := []string{"diff", "-p"}
	before := append([]string(nil), in...)
	got, err = StrippedArgs(context.Background(), "", in...)
	assertArgv(t, got, err, wantArgv("diff", "--no-ext-diff", "--no-textconv", "-p"))
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("StrippedArgs mutated its input: %q", in)
	}
}

// Test_stripped_scan_parse_scope_and_origin covers the trust rule over
// every scope and origin shape: an untrusted entry yields the neutral
// override, a trusted one yields none.
func Test_stripped_scan_parse_scope_and_origin(t *testing.T) {
	const dir = "/base/repo"
	tests := []struct {
		name    string
		scope   string
		origin  string
		trusted bool
	}{
		{"local", "local", "file:.git/config", false},
		{"worktree", "worktree", "file:/base/repo/.git/worktrees/w/config.worktree", false},
		{"system", "system", "file:/etc/gitconfig", true},
		{"global", "global", "file:/home/u/.gitconfig", true},
		{"command", "command", "command line:", true},
		{"unknown with an absolute origin outside dir", "unknown", "file:/opt/homebrew/etc/gitconfig", true},
		{"unknown with a relative origin", "unknown", "file:.git/config", false},
		{"unknown with an origin under dir", "unknown", "file:/base/repo/payload/config", false},
		{"unknown with an origin containing /.git/", "unknown", "file:/elsewhere/.git/config", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubScan(t, nil, scanRec{tt.scope, tt.origin, "filter.x.clean", "evil"})
			got, err := StrippedArgs(context.Background(), dir, "status")
			if tt.trusted {
				assertArgv(t, got, err, wantArgv("status"))
				return
			}
			assertArgv(t, got, err, wantArgv("-c", "filter.x.clean=", "status"))
		})
	}
}

// Test_stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack
// covers one local entry per class: every override comes out with the
// neutral value (no trusted entry exists to reassert), the pack programs
// with the default program name, and the credential helper as a bare
// reset, all sorted by key.
func Test_stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack(t *testing.T) {
	stubScan(t, nil,
		scanRec{"local", "file:.git/config", "filter.evil.clean", "/m/clean.sh"},
		scanRec{"local", "file:.git/config", "filter.evil.smudge", "/m/smudge.sh"},
		scanRec{"local", "file:.git/config", "filter.evil.process", "/m/process.sh"},
		scanRec{"local", "file:.git/config", "merge.evilm.driver", "/m/merge.sh"},
		scanRec{"local", "file:.git/config", "core.sshcommand", "/m/ssh.sh"},
		scanRec{"local", "file:.git/config", "core.askpass", "/m/askpass.sh"},
		scanRec{"local", "file:.git/config", "remote.origin.uploadpack", "/m/uploadpack.sh"},
		scanRec{"local", "file:.git/config", "remote.origin.receivepack", "/m/receivepack.sh"},
		scanRec{"local", "file:.git/config", "credential.helper", "/m/cred.sh"},
	)
	got, err := StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv(
		"-c", "core.askpass=",
		"-c", "core.sshcommand=ssh",
		"-c", "credential.helper=",
		"-c", "filter.evil.clean=",
		"-c", "filter.evil.process=",
		"-c", "filter.evil.smudge=",
		"-c", "merge.evilm.driver=",
		"-c", "remote.origin.receivepack=git-receive-pack",
		"-c", "remote.origin.uploadpack=git-upload-pack",
		"status",
	))
}

// Test_stripped_keeps_global_filter_and_credential_helper covers the other
// half of AC3: a filter and a credential helper defined in the person's
// global configuration produce no override at all.
func Test_stripped_keeps_global_filter_and_credential_helper(t *testing.T) {
	stubScan(t, nil,
		scanRec{"global", "file:/home/u/.gitconfig", "filter.lfs.clean", "git-lfs clean -- %f"},
		scanRec{"global", "file:/home/u/.gitconfig", "credential.helper", "/home/u/bin/cred"},
	)
	got, err := StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv("status"))
}

// Test_stripped_local_override_of_a_global_filter_is_reasserted covers the
// case a plain reset would get wrong: a global filter value the local
// scope shadows is reasserted, not emptied.
func Test_stripped_local_override_of_a_global_filter_is_reasserted(t *testing.T) {
	stubScan(t, nil,
		scanRec{"global", "file:/home/u/.gitconfig", "filter.x.clean", "A"},
		scanRec{"local", "file:.git/config", "filter.x.clean", "evil"},
	)
	got, err := StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv("-c", "filter.x.clean=A", "status"))
}

// Test_stripped_trusts_exact_lfs_command_lines covers the git-lfs
// exception: the three exact command lines stay even from a local scope,
// and nothing that merely starts like them does.
func Test_stripped_trusts_exact_lfs_command_lines(t *testing.T) {
	exact := []scanRec{
		scanRec{"local", "file:.git/config", "filter.lfs.clean", "git-lfs clean -- %f"},
		scanRec{"local", "file:.git/config", "filter.lfs.smudge", "git-lfs smudge -- %f"},
		scanRec{"local", "file:.git/config", "filter.lfs.process", "git-lfs filter-process"},
	}
	stubScan(t, nil, exact...)
	got, err := StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv("status"))

	tampered := append([]scanRec{
		scanRec{"local", "file:.git/config", "filter.lfs.clean", "git-lfs clean -- %f; touch x"},
	}, exact[1:]...)
	stubScan(t, nil, tampered...)
	got, err = StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv("-c", "filter.lfs.clean=", "status"))
}

// Test_stripped_credential_helper_reset_keeps_trusted_helpers covers the
// credential list: one untrusted helper resets it, the trusted ones come
// back in their original order, in config order.
func Test_stripped_credential_helper_reset_keeps_trusted_helpers(t *testing.T) {
	stubScan(t, nil,
		scanRec{"global", "file:/home/u/.gitconfig", "credential.helper", "H1"},
		scanRec{"local", "file:.git/config", "credential.helper", "evil"},
		scanRec{"global", "file:/home/u/.gitconfig", "credential.helper", "H2"},
	)
	got, err := StrippedArgs(context.Background(), "/base/repo", "status")
	assertArgv(t, got, err, wantArgv(
		"-c", "credential.helper=",
		"-c", "credential.helper=H1",
		"-c", "credential.helper=H2",
		"status",
	))
}

// Test_stripped_scan_failure_fails_closed covers fail-closed: a scan that
// errors, and output that is not three NUL-terminated fields per record,
// both return no argv at all, so the caller cannot run git.
func Test_stripped_scan_failure_fails_closed(t *testing.T) {
	orig := scanConfig
	t.Cleanup(func() { scanConfig = orig })

	scanConfig = func(ctx context.Context, dir string) ([]byte, error) {
		return nil, errors.New("scan exploded")
	}
	if got, err := StrippedArgs(context.Background(), "/base/repo", "status"); err == nil || got != nil {
		t.Fatalf("scan error: got (%q, %v), want (nil, error)", got, err)
	}

	scanConfig = func(ctx context.Context, dir string) ([]byte, error) {
		return []byte("local\x00file:.git/config\x00filter.x.clean"), nil // truncated: no final NUL
	}
	if got, err := StrippedArgs(context.Background(), "/base/repo", "status"); err == nil || got != nil {
		t.Fatalf("truncated record: got (%q, %v), want (nil, error)", got, err)
	}

	scanConfig = func(ctx context.Context, dir string) ([]byte, error) {
		return []byte("a\x00b\x00c\x00d\x00"), nil // a fourth field: not a record of three
	}
	if got, err := StrippedArgs(context.Background(), "/base/repo", "status"); err == nil || got != nil {
		t.Fatalf("extra field: got (%q, %v), want (nil, error)", got, err)
	}
}

// Test_stripped_scan_exit_one_is_no_match runs the REAL defaultScan (no
// stub): a fresh repository whose global and system configuration are
// switched off matches nothing the overrides act on, so the call succeeds
// with only the static extras.
func Test_stripped_scan_exit_one_is_no_match(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	got, err := StrippedArgs(context.Background(), dir, "status")
	assertArgv(t, got, err, wantArgv("status"))
}

// Test_stripped_skips_the_scan_for_ref_only_subcommands covers the
// allow-list: subcommands that only read refs or the configuration are
// not scanned (0 calls) but still carry the static extras; status is.
func Test_stripped_skips_the_scan_for_ref_only_subcommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"rev-parse", []string{"rev-parse", "HEAD"}, wantArgv("rev-parse", "HEAD")},
		{"rev-list behind a -C", []string{"-C", "x", "rev-list", "HEAD"}, wantArgv("-C", "x", "rev-list", "HEAD")},
		{"for-each-ref", []string{"for-each-ref"}, wantArgv("for-each-ref")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stubScan(t, nil)
			got, err := StrippedArgs(context.Background(), "", tt.args...)
			assertArgv(t, got, err, tt.want)
			if *calls != 0 {
				t.Fatalf("scan ran %d times, want 0", *calls)
			}
		})
	}

	t.Run("status is scanned", func(t *testing.T) {
		calls := stubScan(t, nil)
		got, err := StrippedArgs(context.Background(), "", "status")
		assertArgv(t, got, err, wantArgv("status"))
		if *calls != 1 {
			t.Fatalf("scan ran %d times, want 1", *calls)
		}
	})
}

// Test_stripped_scans_an_unknown_subcommand covers the fail-safe default:
// a subcommand the allow-list does not know is scanned, and so is an
// argument list with no subcommand at all.
func Test_stripped_scans_an_unknown_subcommand(t *testing.T) {
	t.Run("frobnicate", func(t *testing.T) {
		calls := stubScan(t, nil)
		got, err := StrippedArgs(context.Background(), "", "frobnicate", "--x")
		assertArgv(t, got, err, wantArgv("frobnicate", "--x"))
		if *calls != 1 {
			t.Fatalf("scan ran %d times, want 1", *calls)
		}
	})

	t.Run("no subcommand", func(t *testing.T) {
		calls := stubScan(t, nil)
		got, err := StrippedArgs(context.Background(), "", "--no-pager")
		assertArgv(t, got, err, wantArgv("--no-pager"))
		if *calls != 1 {
			t.Fatalf("scan ran %d times, want 1", *calls)
		}
	})
}
