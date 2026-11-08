package wsapi

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/steward"
)

// planPathRe matches a contract's planRef that is a plain in-repository
// relative path the collector may read at the head.
var planPathRe = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/-]{0,199}$`)

// testNameRe finds test function names in plan text and contract criteria.
var testNameRe = regexp.MustCompile(`\bTest[A-Za-z0-9_]{1,100}\b`)

// stewardPlanMax bounds the plan text read at the head.
const stewardPlanMax = 64 * 1024

// twoAxisInputs gathers what the Standards and Spec passes need beyond the
// single review's inputs (ADR-0054 §3). Read-only, like the rest of the
// collector. A failure leaves that part empty, which the digest and the
// passes treat as "not available" rather than guessing: an unresolvable
// merge-base leaves StandardsInput.BaseRef empty and the Standards pass is
// then blocked (D11), never run against the head.
func (c *stewardCollector) twoAxisInputs(ctx context.Context, dir, diffBase, head string, cfg config.StewardConfig, in steward.Inputs) *steward.TwoAxisInputs {
	out := &steward.TwoAxisInputs{
		Standards: steward.StandardsInput{Files: []steward.StandardsFileInput{}, Rules: append([]string{}, cfg.Standards.Rules...)},
		Spec:      steward.SpecInput{ContractCriteria: []steward.SpecCriterionInput{}, ADRFiles: []string{}, TestsInTree: map[string]bool{}},
	}
	if base, err := steward.MergeBase(ctx, dir, diffBase, head); err == nil {
		out.Standards.BaseRef = base
		if files, err := steward.StandardsAtBase(ctx, dir, base, cfg.Standards.Files); err == nil {
			out.Standards.Files = files
			if diff, err := steward.StandardsHeadDiff(ctx, dir, base, head, files); err == nil {
				out.Standards.HeadDiff = diff
			}
		} else {
			// Could not read the standards at the base: nothing may be judged.
			out.Standards.BaseRef = ""
		}
	}

	names := map[string]bool{}
	if rec, err := c.val.LatestContract(in.Task.ID); err == nil {
		for _, ac := range rec.Contract.ACs {
			ci := steward.SpecCriterionInput{ID: ac.ID, Text: ac.Text, NamedTests: append([]string{}, ac.Covers.Tests...)}
			out.Spec.ContractCriteria = append(out.Spec.ContractCriteria, ci)
			for _, n := range ci.NamedTests {
				names[n] = true
			}
		}
	}
	if p := in.PlanFile; p != "" && planPathRe.MatchString(p) && !strings.Contains(p, "..") && !strings.HasPrefix(p, "/") {
		out.Spec.PlanPath = p
		if txt, err := gitShowAt(ctx, dir, head, p, stewardPlanMax); err == nil {
			out.Spec.PlanText = txt
			for _, n := range testNameRe.FindAllString(txt, -1) {
				names[n] = true
			}
		}
	}
	if files, err := gitListDir(ctx, dir, head, "docs/decisions"); err == nil {
		out.Spec.ADRFiles = files
	}
	for n := range names {
		if len(out.Spec.TestsInTree) >= 80 {
			break
		}
		out.Spec.TestsInTree[n] = gitHasTestFunc(ctx, dir, head, n)
	}
	return out
}

// gitShowAt reads a regular file at head, through the object database (a
// symlink or a missing path is an error), cut to max bytes.
func gitShowAt(ctx context.Context, dir, head, path string, max int) (string, error) {
	obj := head + ":" + path
	size, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "cat-file", "-s", obj).Output()
	if err != nil {
		return "", err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > 8*max {
		return "", errors.New("file too large")
	}
	out, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "cat-file", "blob", obj).Output()
	if err != nil {
		return "", err
	}
	if len(out) > max {
		out = out[:max]
	}
	return string(out), nil
}

// gitListDir lists the entry names of a directory at head.
func gitListDir(ctx context.Context, dir, head, path string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "ls-tree", "--name-only", head, path+"/").Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			names = append(names, strings.TrimPrefix(l, path+"/"))
		}
	}
	return names, nil
}

// gitHasTestFunc reports whether a Go test function of that name exists in a
// *_test.go file at head (a hint for the Spec pass, never a gate).
func gitHasTestFunc(ctx context.Context, dir, head, name string) bool {
	if !regexp.MustCompile(`^[A-Za-z0-9_]{1,100}$`).MatchString(name) {
		return false
	}
	err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "grep", "-q", "-F", "func "+name+"(", head, "--", "*_test.go").Run()
	return err == nil
}
