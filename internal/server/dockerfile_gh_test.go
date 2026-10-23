package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDockerfile_InstallsGh: the daemon image carries the GitHub CLI
// (ADR-0039 section 8), from a pinned release binary whose SHA-256 is checked
// at build time, with a `gh --version` smoke check in the same layer. It is a
// source scan: building the image needs docker and the network (the docker
// CI lane builds it for real).
func TestDockerfile_InstallsGh(t *testing.T) {
	b, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"ARG GH_VERSION=",
		"ARG GH_SHA256_AMD64=",
		"ARG GH_SHA256_ARM64=",
		"https://github.com/cli/cli/releases/download/v${GH_VERSION}/",
		`sha256sum -c -`,
		"gh --version",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("Dockerfile lacks %q", want)
		}
	}
	for _, arg := range []string{"GH_SHA256_AMD64", "GH_SHA256_ARM64"} {
		if !regexp.MustCompile(`ARG ` + arg + `=[0-9a-f]{64}\n`).MatchString(src) {
			t.Errorf("%s is not a pinned 64-hex SHA-256", arg)
		}
	}
	// The checksum is verified before the archive is unpacked, and the
	// version check runs after it: order matters in a single RUN.
	iSum, iTar, iVer := strings.Index(src, `echo "${gh_sha}`), strings.Index(src, `tar -xzf "${gh_tar}"`), strings.LastIndex(src, "gh --version")
	if iSum < 0 || iTar < iSum || iVer < iTar {
		t.Errorf("gh install order is wrong: checksum %d, unpack %d, version check %d", iSum, iTar, iVer)
	}
}
