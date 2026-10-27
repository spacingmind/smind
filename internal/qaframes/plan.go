package qaframes

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// uiPrefix is the only tree the diff mapping covers: the web UI sources.
const uiPrefix = "web/packages/ui/src/"

// Target is a screen worth shooting for a changed UI file.
type Target struct {
	Name  string `json:"name"`
	Route string `json:"route"` // hash route on the isolated daemon, "#/" is the shell
	Hint  string `json:"hint"`  // what the validator must do to reach the changed state
}

const chatRoute = "#/workspace/1/task/1/chat/1"

type mapRule struct {
	prefix string // under uiPrefix
	Target
}

// mapRules is checked in order, first prefix match wins. A path under the UI
// tree that matches none is reported unmapped, never skipped.
var mapRules = []mapRule{
	{"components/inbox/", Target{"inbox", "#/", "open Inbox from the sidebar"}},
	{"components/account-usage/", Target{"usage", "#/", "open the usage sidebar item or modal"}},
	{"components/accounts-dialog", Target{"accounts", "#/", "open the accounts dialog"}},
	{"components/composer/", Target{"composer", chatRoute, "focus the composer; exercise the changed control"}},
	{"components/permission/", Target{"permission", chatRoute, "seed a pending permission request"}},
	{"components/git/", Target{"git-pr", "#/workspace/1/task/1/pr", "open the PR tab"}},
	{"components/pr-panel/", Target{"git-pr", "#/workspace/1/task/1/pr", "open the PR tab"}},
	{"components/settings/", Target{"settings", "#/", "open Settings from the app menu"}},
	{"components/plugins/", Target{"plugins", "#/", "open the plugins settings page"}},
	{"components/learning/", Target{"learning", "#/", "open the learning badge or page"}},
	{"components/new-task/", Target{"new-task", "#/", "open the new-task dialog"}},
	{"components/app-sidebar", Target{"shell", "#/", "sidebar at rest and while expanding/collapsing"}},
	{"components/app-header", Target{"shell", "#/", "header at rest"}},
	{"components/diff-", Target{"diff", "#/workspace/1/task/1/diff", "open the diff tab"}},
	{"components/file-", Target{"files", "#/workspace/1/task/1/files", "open the files tab"}},
	{"index.css", Target{"shell", "#/", "every screen: a global stylesheet changed"}},
	{"desktop-chrome.css", Target{"shell", "#/", "desktop chrome only: web shows no difference"}},
	{"App.tsx", Target{"shell", "#/", "the app shell"}},
	{"lib/theme", Target{"shell", "#/", "every screen in both themes"}},
}

var testFileRe = regexp.MustCompile(`(\.test\.[jt]sx?|\.spec\.[jt]sx?|/__fixtures__/|/test/)`)

// Mapping is the result of mapping a diff.
type Mapping struct {
	Targets  []MappedTarget `json:"targets"`
	Unmapped []string       `json:"unmapped"` // UI files with no rule: the validator must pick the screen
	Ignored  []Ignored      `json:"ignored"`  // files deliberately not shot, with the reason
}

// MappedTarget is a target and the changed files that led to it.
type MappedTarget struct {
	Target
	Files []string `json:"files"`
}

// Ignored is a changed file the mapping did not turn into a screen.
type Ignored struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// MapDiff maps changed repo-relative files to screens to shoot. Every file is
// accounted for exactly once: mapped, unmapped or ignored with a reason.
func MapDiff(files []string) Mapping {
	m := Mapping{Targets: []MappedTarget{}, Unmapped: []string{}, Ignored: []Ignored{}}
	byName := map[string]*MappedTarget{}
	var order []string
	for _, f := range files {
		f = path.Clean(strings.ReplaceAll(f, "\\", "/"))
		if !strings.HasPrefix(f, uiPrefix) {
			m.Ignored = append(m.Ignored, Ignored{f, "not under " + uiPrefix})
			continue
		}
		if testFileRe.MatchString(f) {
			m.Ignored = append(m.Ignored, Ignored{f, "test file"})
			continue
		}
		rel := strings.TrimPrefix(f, uiPrefix)
		matched := false
		for _, r := range mapRules {
			if strings.HasPrefix(rel, r.prefix) {
				mt, ok := byName[r.Name]
				if !ok {
					mt = &MappedTarget{Target: r.Target}
					byName[r.Name] = mt
					order = append(order, r.Name)
				}
				mt.Files = append(mt.Files, f)
				matched = true
				break
			}
		}
		if !matched {
			m.Unmapped = append(m.Unmapped, f)
		}
	}
	for _, n := range order {
		m.Targets = append(m.Targets, *byName[n])
	}
	sort.Strings(m.Unmapped)
	return m
}

var surfaceRe = regexp.MustCompile(`^\s*web\s+(\S+)`)

// SurfaceRoute turns a contract frame surface ("web /inbox", "web #/workspace/1/task/1/chat/1")
// into a hash route on the isolated daemon. ok is false when the surface is not a
// web surface or names no route; the frame is then reported blocked, not skipped.
func SurfaceRoute(surface string) (route string, ok bool) {
	m := surfaceRe.FindStringSubmatch(surface)
	if m == nil {
		return "", false
	}
	r := m[1]
	switch {
	case strings.HasPrefix(r, "#/"):
		return r, true
	case r == "/":
		return "#/", true
	case strings.HasPrefix(r, "/"):
		return "#" + r, true
	}
	return "", false
}
