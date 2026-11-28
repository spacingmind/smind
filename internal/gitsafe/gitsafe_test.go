package gitsafe

import (
	"reflect"
	"strings"
	"testing"
)

// Test_gitsafe_args_orders_overrides_and_diff_flags covers the plain shape
// of a call: the fsmonitor override first, the two diff-driver flags right
// after the subcommand, the rest as given.
func Test_gitsafe_args_orders_overrides_and_diff_flags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "diff",
			in:   []string{"diff", "--cached", "HEAD"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--cached", "HEAD"},
		},
		{
			name: "show",
			in:   []string{"show", "HEAD"},
			want: []string{"-c", "core.fsmonitor=false", "show", "--no-ext-diff", "--no-textconv", "HEAD"},
		},
		{
			name: "log",
			in:   []string{"log", "--reverse", "--format=%H"},
			want: []string{"-c", "core.fsmonitor=false", "log", "--no-ext-diff", "--no-textconv", "--reverse", "--format=%H"},
		},
		{
			name: "whatchanged",
			in:   []string{"whatchanged", "-n", "1"},
			want: []string{"-c", "core.fsmonitor=false", "whatchanged", "--no-ext-diff", "--no-textconv", "-n", "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]string(nil), tt.in...)
			got := Args(tt.in...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Args(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			if !reflect.DeepEqual(tt.in, before) {
				t.Fatalf("Args mutated its input: %q", tt.in)
			}
		})
	}
}

// Test_gitsafe_args_finds_the_subcommand_behind_global_options covers the
// git <global options> <verb> shape: the diff flags still land right after
// the verb, not after a global option's value.
func Test_gitsafe_args_finds_the_subcommand_behind_global_options(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "lone option and -C then diff",
			in:   []string{"--no-optional-locks", "-C", "/r", "diff", "x"},
			want: []string{"-c", "core.fsmonitor=false", "--no-optional-locks", "-C", "/r", "diff", "--no-ext-diff", "--no-textconv", "x"},
		},
		{
			name: "-c and -C then log",
			in:   []string{"-c", "a=b", "-C", "/r", "log", "-g"},
			want: []string{"-c", "core.fsmonitor=false", "-c", "a=b", "-C", "/r", "log", "--no-ext-diff", "--no-textconv", "-g"},
		},
		{
			name: "--git-dir= then show",
			in:   []string{"--git-dir=/g", "show", "-s"},
			want: []string{"-c", "core.fsmonitor=false", "--git-dir=/g", "show", "--no-ext-diff", "--no-textconv", "-s"},
		},
		{
			name: "--git-dir as a pair then log",
			in:   []string{"--git-dir", "/g", "log"},
			want: []string{"-c", "core.fsmonitor=false", "--git-dir", "/g", "log", "--no-ext-diff", "--no-textconv"},
		},
		{
			name: "--work-tree and --namespace consume their values",
			in:   []string{"--work-tree", "/w", "--namespace", "ns", "show"},
			want: []string{"-c", "core.fsmonitor=false", "--work-tree", "/w", "--namespace", "ns", "show", "--no-ext-diff", "--no-textconv"},
		},
		{
			name: "no subcommand: prefix plus input unchanged",
			in:   []string{"--no-pager"},
			want: []string{"-c", "core.fsmonitor=false", "--no-pager"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Args(tt.in...); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Args(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// Test_gitsafe_args_does_not_duplicate_or_touch_other_verbs covers the
// no-op cases: a call that already carries a flag keeps one of each, and
// non-diff verbs only get the fsmonitor prefix.
func Test_gitsafe_args_does_not_duplicate_or_touch_other_verbs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "both diff flags already present",
			in:   []string{"diff", "--no-ext-diff", "--no-textconv", "x"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "x"},
		},
		{
			name: "only --no-textconv present: just --no-ext-diff is inserted",
			in:   []string{"diff", "--cached", "--no-textconv"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--cached", "--no-textconv"},
		},
		{
			name: "status",
			in:   []string{"status", "--porcelain"},
			want: []string{"-c", "core.fsmonitor=false", "status", "--porcelain"},
		},
		{
			name: "worktree",
			in:   []string{"worktree", "add", "p"},
			want: []string{"-c", "core.fsmonitor=false", "worktree", "add", "p"},
		},
		{
			name: "rev-parse",
			in:   []string{"rev-parse", "HEAD"},
			want: []string{"-c", "core.fsmonitor=false", "rev-parse", "HEAD"},
		},
		{
			name: "fsmonitor pair already present: no second one",
			in:   []string{"-c", "core.fsmonitor=false", "diff", "x"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Args(tt.in...); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Args(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// Test_gitsafe_args_leaves_pathspecs_after_double_dash covers pathspecs:
// an element after a bare "--" that looks like a flag is not counted as
// present and is never moved.
func Test_gitsafe_args_leaves_pathspecs_after_double_dash(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "pathspec that looks like --no-textconv is untouched and not counted",
			in:   []string{"diff", "--cached", "--", "--no-textconv", "a.txt"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--cached", "--", "--no-textconv", "a.txt"},
		},
		{
			name: "a pathspec pair reading -c core.fsmonitor=false does not suppress the prefix",
			in:   []string{"status", "--", "-c", "core.fsmonitor=false"},
			want: []string{"-c", "core.fsmonitor=false", "status", "--", "-c", "core.fsmonitor=false"},
		},
		{
			name: "a real flag before the -- counts as present",
			in:   []string{"diff", "--no-textconv", "--", "a.txt"},
			want: []string{"-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "a.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Args(tt.in...); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Args(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// Test_gitsafe_env_drops_external_diff_and_sets_the_safe_defaults covers
// Env's contract: the four program- and lock-naming variables are dropped,
// everything else keeps its order, the three safe defaults are appended.
func Test_gitsafe_env_drops_external_diff_and_sets_the_safe_defaults(t *testing.T) {
	base := []string{
		"PATH=/usr/bin:/bin",
		"GIT_EXTERNAL_DIFF=/tmp/evil.sh",
		"GIT_DIR=/elsewhere",
		"GIT_PAGER=less",
		"HOME=/home/u",
		"GIT_TERMINAL_PROMPT=1",
		"GIT_OPTIONAL_LOCKS=1",
	}
	want := []string{
		"PATH=/usr/bin:/bin",
		"GIT_DIR=/elsewhere",
		"HOME=/home/u",
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	}
	if got := Env(base); !reflect.DeepEqual(got, want) {
		t.Fatalf("Env(base)\n got %q\nwant %q", got, want)
	}

	// nil base means the process's own environment, whatever it happens to
	// carry: the counts must still come out exactly one of each.
	got := Env(nil)
	for _, e := range got {
		if strings.HasPrefix(e, "GIT_EXTERNAL_DIFF=") {
			t.Fatalf("Env(nil) kept %q", e)
		}
	}
	for name, wantVal := range map[string]string{
		"GIT_PAGER":           "cat",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_OPTIONAL_LOCKS":  "0",
	} {
		count, val := 0, ""
		for _, e := range got {
			if k, v, ok := strings.Cut(e, "="); ok && k == name {
				count, val = count+1, v
			}
		}
		if count != 1 || val != wantVal {
			t.Fatalf("Env(nil): %s appears %d times (last %q), want exactly once as %s=%s", name, count, val, name, wantVal)
		}
	}
}

// Test_gitsafe_env_does_not_mutate_its_input covers Env's other promise:
// neither the call nor a later write to the result reaches base.
func Test_gitsafe_env_does_not_mutate_its_input(t *testing.T) {
	base := []string{
		"PATH=/usr/bin:/bin",
		"GIT_EXTERNAL_DIFF=/tmp/evil.sh",
		"GIT_PAGER=less",
		"GIT_TERMINAL_PROMPT=1",
		"GIT_OPTIONAL_LOCKS=1",
	}
	before := append([]string(nil), base...)
	got := Env(base)
	if !reflect.DeepEqual(base, before) {
		t.Fatalf("Env mutated its input: %q", base)
	}
	if len(got) == 0 {
		t.Fatal("Env returned an empty environment")
	}
	got[0] = "MUTATED"
	if !reflect.DeepEqual(base, before) {
		t.Fatalf("writing to Env's result reached its input: %q", base)
	}
}
