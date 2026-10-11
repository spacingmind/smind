package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spacingmind/smind/internal/skills"
)

const cmdSkillUsage = `usage: smind skill ls [--target claude|agents|codex|all] [name...]
       smind skill install [--target claude|agents|codex|all] [--force] [name...]
       smind skill uninstall [--target claude|agents|codex|all] [--force] [name...]`

// skillHomeEnv overrides the home directory skills are installed under
// (tests and sandboxes); default is the user's home directory.
const skillHomeEnv = "SMIND_SKILL_HOME"

// cmdSkill manages the bundled orchestration skills. It is an explicit user
// command only: it never talks to the daemon and nothing syncs at startup.
func cmdSkill(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdSkillUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "install", "uninstall":
	default:
		fmt.Fprintf(os.Stderr, "smind skill: unknown subcommand %q\n%s\n", sub, cmdSkillUsage)
		return 2
	}

	opts := skills.Options{Home: os.Getenv(skillHomeEnv)}
	target := "all"
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--force":
			opts.Force = true
		case a == "--target":
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "smind skill: --target needs a value")
				return 2
			}
			i++
			target = rest[i]
		case strings.HasPrefix(a, "--target="):
			target = strings.TrimPrefix(a, "--target=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "smind skill: unknown flag %q\n%s\n", a, cmdSkillUsage)
			return 2
		default:
			opts.Names = append(opts.Names, a)
		}
	}
	if target != "all" {
		opts.Targets = []string{target}
	}

	var (
		res []skills.Result
		err error
	)
	switch sub {
	case "ls":
		res, err = skills.Status(opts)
	case "install":
		res, err = skills.Install(opts)
	case "uninstall":
		res, err = skills.Uninstall(opts)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind skill %s: %v\n", sub, err)
		return 1
	}
	printSkillResults(osStdout, sub, res)
	return 0
}

func printSkillResults(w io.Writer, sub string, res []skills.Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SKILL\tTARGET\tSTATUS\tPATH")
	skipped := false
	for _, r := range res {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Skill, r.Target, r.Status, r.Dir)
		skipped = skipped || len(r.Skipped) > 0
	}
	tw.Flush()
	if !skipped {
		return
	}
	verb := "overwritten"
	if sub == "uninstall" {
		verb = "removed"
	}
	fmt.Fprintf(w, "\nmodified, skipped (edited since install; left untouched):\n")
	for _, r := range res {
		for _, f := range r.Skipped {
			fmt.Fprintf(w, "  %s/%s (%s)\n", r.Dir, f, r.Target)
		}
	}
	fmt.Fprintf(w, "re-run with --force to have them %s.\n", verb)
}
