// Command qaframes is the validator's frame harness (ADR-0045 §7): it starts an
// ISOLATED daemon, shoots the contract's frame items in a real browser (light and
// dark, 1280 and 390, transition frames, contact sheets of at most 12) and writes
// an evidence report. It is a helper run with `go run`, not part of smind's CLI.
//
// Exit codes: 0 every frame captured; 1 failure; 2 usage or a refused send;
// 3 blocked (no browser / no toolchain): the report says which, never a pass.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spacingmind/smind/internal/qaframes"
	"github.com/spacingmind/smind/internal/validation"
)

func main() { os.Exit(run(os.Args[1:])) }

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func run(args []string) int {
	fs := flag.NewFlagSet("qaframes", flag.ContinueOnError)
	var (
		framesFile = fs.String("frames", "", "JSON file: the contract's frames array (or the contract object with a frames key)")
		scenFile   = fs.String("scenarios", "", "JSON file: {frameId: [steps]} overriding the default steps")
		repo       = fs.String("repo", ".", "worktree root (evidence paths are relative to it)")
		evidence   = fs.String("evidence-dir", qaframes.DefaultEvidenceRel, "evidence dir, relative to --repo")
		diffBase   = fs.String("diff-base", "", "git ref: map `git diff --name-only <ref>...HEAD` to screens")
		allowSend  = fs.Bool("allow-send", false, "allow scenario steps that send a prompt (needs --model "+qaframes.HaikuModel+")")
		model      = fs.String("model", "", "model a scripted send uses; only "+qaframes.HaikuModel+" is accepted")
		bin        = fs.String("bin", "", "smind binary to serve (default: build web + binary from --repo)")
		noBuild    = fs.Bool("no-build", false, "do not build the web UI (use internal/server/dist as it is)")
		fakeAgent  = fs.Bool("fake-agent", false, "build the fake ACP agent and point the daemon's GLM provider at it")
		seed       = fs.String("seed", "", "JSON [{method, params}] RPCs to run on the isolated daemon before shooting")
		chrome     = fs.String("chrome", "", "Chromium/Chrome executable")
		pwDir      = fs.String("playwright-dir", "", "node_modules directory holding playwright-core or playwright")
		passEnv    listFlag
	)
	fs.Var(&passEnv, "pass-env", "caller env var name to pass to the daemon (live sends only; repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *framesFile == "" {
		fmt.Fprintln(os.Stderr, "qaframes: --frames is required")
		return 2
	}
	frames, err := readFrames(*framesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qaframes:", err)
		return 2
	}
	scen := map[string][]qaframes.Step{}
	if *scenFile != "" {
		if err := readJSON(*scenFile, &scen); err != nil {
			fmt.Fprintln(os.Stderr, "qaframes:", err)
			return 2
		}
	}
	var changed []string
	if *diffBase != "" {
		out, err := exec.Command("git", "-C", *repo, "diff", "--name-only", *diffBase+"...HEAD").Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "qaframes: git diff:", err)
			return 1
		}
		changed = strings.Fields(string(out))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, err := qaframes.Run(ctx, qaframes.Options{
		Frames: frames, Scenarios: scen, RepoRoot: *repo, EvidenceRel: *evidence, ChangedFiles: changed,
		AllowSend: *allowSend, Model: *model, Env: qaframes.OSEnv(*repo), Chrome: *chrome, PlaywrightDir: *pwDir,
		Launcher: &qaframes.RealLauncher{Bin: *bin, NoBuild: *noBuild, FakeAgent: *fakeAgent, SeedFile: *seed, PassEnv: passEnv,
			AllowSend: *allowSend, RepoRoot: *repo, Log: os.Stderr},
	})
	if rep != nil {
		fmt.Printf("qaframes: %s%s; report %s/%s/report.md\n", rep.Status, reason(rep.BlockedReason), *repo, rep.EvidenceDir)
	}
	switch {
	case errors.Is(err, qaframes.ErrSendRefused):
		fmt.Fprintln(os.Stderr, "qaframes:", err)
		return 2
	case errors.Is(err, qaframes.ErrNoBrowser), errors.Is(err, qaframes.ErrToolchain):
		fmt.Fprintln(os.Stderr, "qaframes:", err)
		return 3
	case err != nil:
		fmt.Fprintln(os.Stderr, "qaframes:", err)
		return 1
	}
	if rep.Status != qaframes.StatusCaptured {
		return 3
	}
	return 0
}

func reason(r string) string {
	if r == "" {
		return ""
	}
	return " (" + r + ")"
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func readFrames(path string) ([]validation.Frame, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var frames []validation.Frame
	if json.Unmarshal(b, &frames) == nil {
		return frames, nil
	}
	var c struct {
		Frames []validation.Frame `json:"frames"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: not a frames array or a contract object: %w", path, err)
	}
	return c.Frames, nil
}
