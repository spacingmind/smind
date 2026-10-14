// Package selfupdate implements `smind update` (ADR-0031 D1): resolve a
// release, download and verify its archive, stage the new binary, swap it in
// atomically and (for a service-managed daemon) restart onto it, rolling back
// whenever a post-swap check fails.
//
// The package takes every environment-dependent piece (release source,
// running daemon, version probe, platform) as an injected field so the whole
// flow is testable against an httptest release server without touching the
// real binary or daemon.
package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is the subset of semantic versioning smind's release tags use:
// MAJOR.MINOR.PATCH with an optional -prerelease; +build metadata is parsed
// and ignored for precedence, per the spec.
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

// NormalizeVersion trims whitespace and one leading "v" ("v0.12.0" ->
// "0.12.0"), the form release asset names use.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// ValidVersion reports whether v (already normalized) is a version we accept
// from a user or a redirect before it is spliced into a URL and a file name:
// a parseable semver, which also rules out path separators and other junk.
func ValidVersion(v string) bool {
	_, err := parseSemver(v)
	return err == nil
}

func parseSemver(v string) (semver, error) {
	orig := v
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var pre string
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
		if pre == "" {
			return semver{}, fmt.Errorf("invalid version %q: empty prerelease", orig)
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", orig)
	}
	var nums [3]uint64
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return semver{}, fmt.Errorf("invalid version %q: bad number %q", orig, p)
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return semver{}, fmt.Errorf("invalid version %q: bad number %q", orig, p)
		}
		nums[i] = n
	}
	s := semver{major: nums[0], minor: nums[1], patch: nums[2]}
	if pre != "" {
		s.pre = strings.Split(pre, ".")
		for _, id := range s.pre {
			if id == "" || strings.Trim(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
				return semver{}, fmt.Errorf("invalid version %q: bad prerelease %q", orig, id)
			}
		}
	}
	return s, nil
}

// CompareVersions returns -1, 0 or 1 as a is older than, equal to or newer
// than b. A release outranks its own prereleases (1.0.0 > 1.0.0-rc.1).
func CompareVersions(a, b string) (int, error) {
	x, err := parseSemver(NormalizeVersion(a))
	if err != nil {
		return 0, err
	}
	y, err := parseSemver(NormalizeVersion(b))
	if err != nil {
		return 0, err
	}
	for _, d := range [][2]uint64{{x.major, y.major}, {x.minor, y.minor}, {x.patch, y.patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case len(x.pre) == 0 && len(y.pre) == 0:
		return 0, nil
	case len(x.pre) == 0:
		return 1, nil
	case len(y.pre) == 0:
		return -1, nil
	}
	for i := 0; i < len(x.pre) && i < len(y.pre); i++ {
		if c := comparePreID(x.pre[i], y.pre[i]); c != 0 {
			return c, nil
		}
	}
	switch {
	case len(x.pre) < len(y.pre):
		return -1, nil
	case len(x.pre) > len(y.pre):
		return 1, nil
	}
	return 0, nil
}

func comparePreID(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		if an < bn {
			return -1
		}
		if an > bn {
			return 1
		}
		return 0
	case aerr == nil: // numeric identifiers sort before alphanumeric ones
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
