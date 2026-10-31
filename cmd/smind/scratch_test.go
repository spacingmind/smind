package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeScratchDial returns a dial func whose fake caller answers one method
// with the canned raw result and records the method it was asked for.
func fakeScratchDial(t *testing.T, wantMethod, rawResult string) (dial func(context.Context) (wsCaller, error), gotMethod *string) {
	t.Helper()
	gotMethod = new(string)
	return func(ctx context.Context) (wsCaller, error) {
		return func(_ context.Context, method string, params any, out any) error {
			*gotMethod = method
			if method != wantMethod {
				t.Fatalf("method = %q, want %q", method, wantMethod)
			}
			if params != nil {
				t.Fatalf("params = %v, want nil (scratch RPCs take no params)", params)
			}
			return json.Unmarshal([]byte(rawResult), out)
		}, nil
	}, gotMethod
}

func TestScratchUsage_Output(t *testing.T) {
	dial, method := fakeScratchDial(t, "scratch.usage",
		`{"totalBytes":734003200,"totalFiles":1234,"dirs":3,"reclaimableDirs":1,"reclaimableBytes":104857600}`)
	var out bytes.Buffer
	if code := runScratchUsage(context.Background(), dial, false, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	if *method != "scratch.usage" {
		t.Fatalf("called %q, want scratch.usage", *method)
	}
	want := "Scratch: 700.0 MB in 3 task dir(s)\n" +
		"Reclaimable (closed tasks): 100.0 MB in 1 dir(s)\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestScratchUsage_JSON(t *testing.T) {
	dial, _ := fakeScratchDial(t, "scratch.usage",
		`{"totalBytes":734003200,"totalFiles":1234,"dirs":3,"reclaimableDirs":1,"reclaimableBytes":104857600,"future":7}`)
	var out bytes.Buffer
	if code := runScratchUsage(context.Background(), dial, true, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	// The daemon's object verbatim: every sent key survives, indented.
	var back scratchUsageResult
	if err := json.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("stdout is not the daemon JSON: %v\n%s", err, out.String())
	}
	if back.TotalBytes != 734003200 || back.Dirs != 3 {
		t.Fatalf("json = %s, want the usage counters", out.String())
	}
	if !strings.Contains(out.String(), `"future": 7`) {
		t.Fatalf("json dropped an unmodelled key: %s", out.String())
	}
}

func TestScratchClean_Output(t *testing.T) {
	dial, method := fakeScratchDial(t, "scratch.clean", `{"files":42,"bytes":15728640,"dirs":2}`)
	var out bytes.Buffer
	if code := runScratchClean(context.Background(), dial, false, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	if *method != "scratch.clean" {
		t.Fatalf("called %q, want scratch.clean", *method)
	}
	want := "Removed 15.0 MB (42 file(s)) from 2 closed task dir(s).\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestScratchClean_NothingToClean(t *testing.T) {
	dial, _ := fakeScratchDial(t, "scratch.clean", `{"files":0,"bytes":0,"dirs":0}`)
	var out bytes.Buffer
	if code := runScratchClean(context.Background(), dial, false, &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0 (nothing to clean is data, not an error)", code)
	}
	if out.String() != "Nothing to clean.\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), "Nothing to clean.\n")
	}
}

func TestScratch_UnknownSubcommandUsage(t *testing.T) {
	if code := cmdScratch([]string{"purge"}); code != 2 {
		t.Fatalf("code = %d, want 2 for an unknown subcommand", code)
	}
	if code := cmdScratch(nil); code != 2 {
		t.Fatalf("code = %d, want 2 with no subcommand", code)
	}
}
