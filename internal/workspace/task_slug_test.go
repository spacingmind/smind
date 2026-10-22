package workspace

import (
	"strings"
	"testing"
)

func TestSlugifyCapsLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "short title unchanged", title: "Fix the thing", want: "fix-the-thing"},
		{name: "slug exactly maxSlugLen unchanged", title: strings.Repeat("a", 40), want: strings.Repeat("a", 40)},
		{
			name:  "long title cut back to word boundary",
			title: "chore(mobile): link EAS project and set app identifiers",
			want:  "chore-mobile-link-eas-project-and-set",
		},
		{name: "long single word has no hyphen to cut to", title: strings.Repeat("a", 100), want: strings.Repeat("a", 40)},
		{
			// slug[40] == '-': the cut already lands on a word boundary, so
			// the full maxSlugLen chars are kept with no trailing hyphen.
			name:  "cut lands exactly before a hyphen",
			title: "words word word word word word word word tail",
			want:  "words-word-word-word-word-word-word-word",
		},
		{name: "all punctuation falls back", title: "!!!", want: "task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slugify(tt.title)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.title, got, tt.want)
			}
			if len(got) > maxSlugLen {
				t.Errorf("slugify(%q) = %q, want length <= %d", tt.title, got, maxSlugLen)
			}
		})
	}

	// Unicode title: the regexp strips everything outside [a-z0-9], so
	// only the length and charset invariants are asserted.
	got := slugify("Ünïcödé ñame with accents and a long tail that goes on and on forever")
	if len(got) > maxSlugLen {
		t.Errorf("slugify() = %q, want length <= %d", got, maxSlugLen)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Errorf("slugify() = %q, want no leading or trailing hyphen", got)
	}
	for _, r := range got {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Errorf("slugify() = %q, want only [a-z0-9-] characters", got)
		}
	}
}

func TestCreateTaskLongTitleBranchLength(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	title := strings.Repeat("a", 120)
	task, err := m.CreateTask(w.ID, nil, title)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Branch == nil {
		t.Fatalf("task.Branch is nil")
	}
	branch := *task.Branch
	if !strings.HasPrefix(branch, "smind/task-") {
		t.Fatalf("branch = %q, want prefix %q", branch, "smind/task-")
	}
	// The name after the prefix is slug + "-" + base-36 stamp: the slug is
	// capped at maxSlugLen and today's nanosecond stamps fit in 13 digits.
	if got := len(strings.TrimPrefix(branch, "smind/task-")); got > maxSlugLen+1+13 {
		t.Errorf("branch %q: name length after prefix = %d, want <= %d", branch, got, maxSlugLen+1+13)
	}
	if task.Title != title {
		t.Errorf("task.Title = %q, want the full title %q", task.Title, title)
	}
}
