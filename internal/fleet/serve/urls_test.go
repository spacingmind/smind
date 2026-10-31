package serve

import "testing"

func TestSafeRepoURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://user:ghp_secretsecretsecretsecretsecretsecret@github.com/o/r.git?token=abc#frag", "https://github.com/o/r.git"},
		{"https://github.com/o/r.git", "https://github.com/o/r.git"},
		{"git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"bob@host:o/r.git", "host:o/r.git"},
		{"/Users/x/repo", ""},
		{"file:///Users/x/repo", ""},
		{"C:\\Users\\x\\repo", ""},
		{"C:/Users/x/repo", ""},
		{"../repo", ""},
		{"ftp://h/r", ""},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := SafeRepoURL(c.in); got != c.want {
			t.Errorf("SafeRepoURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
