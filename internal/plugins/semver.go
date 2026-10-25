package plugins

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is the small subset of semantic versioning engines.smind needs:
// parse, compare (prereleases ordered per semver.org section 11) and npm-style
// ranges. No third-party semver module is in go.mod and the grammar is tiny.
type semver struct {
	major, minor, patch uint64
	pre                 []string // prerelease identifiers, nil for a release
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 { // build metadata is ignored
		s = s[:i]
	}
	var pre string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, pre = s[:i], s[i+1:]
		if pre == "" {
			return semver{}, fmt.Errorf("empty prerelease in %q", s)
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
	}
	var v semver
	for i, dst := range []*uint64{&v.major, &v.minor, &v.patch} {
		if parts[i] == "" || (len(parts[i]) > 1 && parts[i][0] == '0') {
			return semver{}, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
		}
		n, err := strconv.ParseUint(parts[i], 10, 32)
		if err != nil {
			return semver{}, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
		}
		*dst = n
	}
	if pre != "" {
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" || !isSemverIdent(id) {
				return semver{}, fmt.Errorf("invalid prerelease %q", pre)
			}
		}
	}
	return v, nil
}

func isSemverIdent(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compare returns -1, 0 or 1. A release is greater than any prerelease of the
// same MAJOR.MINOR.PATCH.
func (a semver) compare(b semver) int {
	if c := cmpUint(a.major, b.major); c != 0 {
		return c
	}
	if c := cmpUint(a.minor, b.minor); c != 0 {
		return c
	}
	if c := cmpUint(a.patch, b.patch); c != 0 {
		return c
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, yn := isNumeric(x), isNumeric(y)
		switch {
		case xn && yn:
			xv, _ := strconv.ParseUint(x, 10, 64)
			yv, _ := strconv.ParseUint(y, 10, 64)
			if c := cmpUint(xv, yv); c != 0 {
				return c
			}
		case xn:
			return -1
		case yn:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return cmpUint(uint64(len(a.pre)), uint64(len(b.pre)))
}

type comparator struct {
	op string // ">=", ">", "<=", "<", "="
	v  semver
}

func (c comparator) ok(v semver) bool {
	r := v.compare(c.v)
	switch c.op {
	case ">=":
		return r >= 0
	case ">":
		return r > 0
	case "<=":
		return r <= 0
	case "<":
		return r < 0
	}
	return r == 0
}

// versionRange is an OR of comparator sets (separated by "||" in the source);
// a set is satisfied when all its comparators are.
type versionRange [][]comparator

// parseRange accepts comparators >=, >, <=, <, = and a bare version, the caret
// and tilde shorthands, and "*", AND-ed by whitespace or commas and OR-ed by
// "||".
func parseRange(s string) (versionRange, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("empty range")
	}
	var out versionRange
	for _, alt := range strings.Split(s, "||") {
		fields := strings.FieldsFunc(alt, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
		if len(fields) == 0 {
			return nil, fmt.Errorf("empty comparator set in %q", s)
		}
		var set []comparator
		for _, f := range fields {
			cs, err := parseComparator(f)
			if err != nil {
				return nil, err
			}
			set = append(set, cs...)
		}
		out = append(out, set)
	}
	return out, nil
}

func parseComparator(f string) ([]comparator, error) {
	if f == "*" {
		return []comparator{{op: ">=", v: semver{}}}, nil
	}
	op := ""
	for _, p := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(f, p) {
			op, f = p, strings.TrimPrefix(f, p)
			break
		}
	}
	v, err := parseSemver(f)
	if err != nil {
		return nil, fmt.Errorf("bad version %q in range: %v", f, err)
	}
	switch op {
	case "^":
		var upper semver
		switch {
		case v.major > 0:
			upper = semver{major: v.major + 1}
		case v.minor > 0:
			upper = semver{minor: v.minor + 1}
		default:
			upper = semver{patch: v.patch + 1}
		}
		return []comparator{{">=", v}, {"<", upper}}, nil
	case "~":
		return []comparator{{">=", v}, {"<", semver{major: v.major, minor: v.minor + 1}}}, nil
	case "":
		op = "="
	}
	return []comparator{{op, v}}, nil
}

// satisfies applies the npm prerelease rule: a prerelease version is only
// admitted by a comparator set that names a prerelease of the same
// MAJOR.MINOR.PATCH, so ">=0.14.0" does not silently match 0.15.0-rc.1.
func (r versionRange) satisfies(v semver) bool {
	for _, set := range r {
		ok := true
		for _, c := range set {
			if !c.ok(v) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if len(v.pre) == 0 {
			return true
		}
		for _, c := range set {
			if len(c.v.pre) > 0 && c.v.major == v.major && c.v.minor == v.minor && c.v.patch == v.patch {
				return true
			}
		}
	}
	return false
}
