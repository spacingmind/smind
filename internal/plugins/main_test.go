package plugins

import (
	"os"
	"testing"

	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
)

func TestMain(m *testing.M) {
	if hosttest.Main() {
		return
	}
	os.Exit(m.Run())
}
