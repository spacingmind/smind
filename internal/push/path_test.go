package push

import (
	"path/filepath"
	"testing"
)

func TestRegistryPath(t *testing.T) {
	t.Parallel()
	if got, want := RegistryPath("/home/x"), filepath.Join("/home/x", "push-registrations.json"); got != want {
		t.Fatalf("RegistryPath = %q, want %q", got, want)
	}
}
