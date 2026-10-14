package selfupdate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/selfupdate/selfupdatetest"
)

var sampleChecksums = []byte(strings.Repeat("ab", 32) + "  smind_1.2.3_linux_amd64.tar.gz\n")

func fetchFrom(files map[string][]byte) func(context.Context, string) ([]byte, error) {
	return func(_ context.Context, name string) ([]byte, error) {
		if b, ok := files[name]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("download %s: %w", name, ErrAssetNotFound)
	}
}

func TestVerifyChecksumsSignatureAccepts(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	verify := SignatureVerifier(signer.PublicKey())
	for name, sig := range map[string][]byte{
		"prehashed ED (minisign, tauri)": signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3")),
		"CRLF line endings":              []byte(strings.ReplaceAll(string(signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))), "\n", "\r\n")),
	} {
		if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: sig})); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The fixture was produced by the real `tauri signer sign --app-version 9.9.9`
// (Tauri CLI 2.11.5, the version desktop/package-lock.json pins) with a
// throwaway key, so this is the actual release-signing output format.
func TestVerifyAcceptsRealTauriSignerOutput(t *testing.T) {
	read := func(n string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "tauri", n))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	pub := strings.TrimSpace(string(read("throwaway.pub")))
	verify := SignatureVerifier(pub)
	files := map[string][]byte{SignatureName: read("checksums.txt.minisig")}
	if err := verify(context.Background(), "9.9.9", read("checksums.txt"), fetchFrom(files)); err != nil {
		t.Fatalf("real tauri signature rejected: %v", err)
	}
	if err := verify(context.Background(), "9.9.8", read("checksums.txt"), fetchFrom(files)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a signature for 9.9.9 must not authorise 9.9.8: %v", err)
	}
	if err := verify(context.Background(), "9.9.9", append(read("checksums.txt"), "x"...), fetchFrom(files)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered checksums accepted: %v", err)
	}
}

// The algorithm byte is not covered by either signature, so the legacy "Ed"
// (sign the file itself) is refused outright rather than risk relabelling.
func TestVerifyRejectsLegacyEdAlgorithm(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	sig := signer.SignLegacy(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))
	err := SignatureVerifier(signer.PublicKey())(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: sig}))
	if !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "unsupported signature algorithm") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyRejectsDuplicateTrustedCommentFields(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	verify := SignatureVerifier(signer.PublicKey())
	for _, comment := range []string{
		"timestamp:1\tfile:checksums.txt\tversion:1.2.2\tversion:1.2.3",
		"timestamp:1\tfile:checksums.txt\tversion:1.2.3\tversion:1.2.2",
		"timestamp:1\tfile:latest.json\tfile:checksums.txt\tversion:1.2.3",
	} {
		sig := signer.Sign(sampleChecksums, comment)
		if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: sig})); !errors.Is(err, ErrBadSignature) {
			t.Errorf("comment %q accepted: %v", comment, err)
		}
	}
}

// The trusted comment is signed byte for byte; outer whitespace is part of it
// and must neither be trimmed away (breaking the signature) nor change which
// fields are read.
func TestVerifyKeepsTrustedCommentBytesIntact(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	verify := SignatureVerifier(signer.PublicKey())
	sig := signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3")+"\t")
	if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: sig})); err != nil {
		t.Fatalf("a validly signed comment with a trailing tab was rejected: %v", err)
	}
}

func TestVerifyRejectsOversizeSignatureFile(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	good := signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))
	big := append(append([]byte(nil), good...), []byte("\n"+strings.Repeat("A", 8192)+"\n")...)
	err := SignatureVerifier(signer.PublicKey())(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: big}))
	if !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "signature file is") {
		t.Fatalf("err = %v, want the size limit to trip on an otherwise valid signature", err)
	}
}

func TestUpdateRefusesWithoutAVerifier(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.VerifyChecksums = nil
	if _, err := e.u.Update(context.Background(), Options{Force: true}); err == nil || !strings.Contains(err.Error(), "no checksum verifier") {
		t.Fatalf("err = %v, want a refusal to run unverified", err)
	}
	e.wantUntouched("0.11.1")
}

func TestVerifyRejectsTamperedChecksums(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	sig := signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))
	evil := []byte(strings.Repeat("cd", 32) + "  smind_1.2.3_linux_amd64.tar.gz\n")
	err := SignatureVerifier(signer.PublicKey())(context.Background(), "1.2.3", evil, fetchFrom(map[string][]byte{SignatureName: sig}))
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestVerifyRejectsMissingMinisig(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	err := SignatureVerifier(signer.PublicKey())(context.Background(), "1.2.3", sampleChecksums, fetchFrom(nil))
	if !errors.Is(err, ErrNoSignature) || !strings.Contains(err.Error(), "v1.2.3 has no checksums.txt.minisig") {
		t.Fatalf("err = %v, want ErrNoSignature naming the release", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	signer, other := selfupdatetest.NewSigner(t), selfupdatetest.NewSigner(t)
	sig := signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))
	err := SignatureVerifier(other.PublicKey())(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: sig}))
	if !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "signed by key") {
		t.Fatalf("err = %v, want a wrong-key rejection", err)
	}
	// Same key id forged by an attacker holding a different private key.
	forged := selfupdatetest.NewSigner(t).WithKeyIDOf(signer)
	forgedSig := forged.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3"))
	if err := SignatureVerifier(signer.PublicKey())(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: forgedSig})); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a signature from another key was accepted: %v", err)
	}
}

func TestVerifyRejectsAlteredTrustedCommentAndReplays(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	verify := SignatureVerifier(signer.PublicKey())
	sig := string(signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3")))

	// Editing the trusted comment (to claim another version) breaks the global signature.
	edited := strings.Replace(sig, "version:1.2.3", "version:1.2.4", 1)
	if err := verify(context.Background(), "1.2.4", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: []byte(edited)})); !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "trusted comment") {
		t.Fatalf("edited trusted comment: %v", err)
	}
	// A genuine signature for another version.
	if err := verify(context.Background(), "1.2.4", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: []byte(sig)})); !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("replayed signature: %v", err)
	}
	// A genuine signature over a different file name.
	other := signer.Sign(sampleChecksums, "timestamp:1\tfile:latest.json\tversion:1.2.3")
	if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: other})); !errors.Is(err, ErrBadSignature) || !strings.Contains(err.Error(), "latest.json") {
		t.Fatalf("signature of another file: %v", err)
	}
	// No version field at all.
	if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: signer.Sign(sampleChecksums, "timestamp:1\tfile:checksums.txt")})); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("unversioned signature accepted: %v", err)
	}
}

func TestVerifyRejectsMalformedSignatures(t *testing.T) {
	signer := selfupdatetest.NewSigner(t)
	verify := SignatureVerifier(signer.PublicKey())
	good := string(signer.Sign(sampleChecksums, selfupdatetest.TrustedComment("1.2.3")))
	lines := strings.Split(strings.TrimSpace(good), "\n")
	cases := map[string]string{
		"empty":               "",
		"garbage":             "not a signature",
		"three lines":         strings.Join(lines[:3], "\n"),
		"bad base64":          strings.Join([]string{lines[0], "!!!", lines[2], lines[3]}, "\n"),
		"short signature":     strings.Join([]string{lines[0], base64.StdEncoding.EncodeToString([]byte("EDshort")), lines[2], lines[3]}, "\n"),
		"missing comment tag": strings.Join([]string{lines[0], lines[1], "comment: x", lines[3]}, "\n"),
		"unknown algorithm":   strings.Join([]string{lines[0], algSwap(t, lines[1], "XX"), lines[2], lines[3]}, "\n"),
	}
	for name, sig := range cases {
		err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: []byte(sig)}))
		if !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v, want ErrBadSignature", name, err)
		}
	}
	// A prehashed signature relabelled "Ed" must not verify (algorithm confusion).
	relabelled := strings.Join([]string{lines[0], algSwap(t, lines[1], "Ed"), lines[2], lines[3]}, "\n")
	if err := verify(context.Background(), "1.2.3", sampleChecksums, fetchFrom(map[string][]byte{SignatureName: []byte(relabelled)})); !errors.Is(err, ErrBadSignature) {
		t.Errorf("ED relabelled as Ed accepted: %v", err)
	}
}

func algSwap(t *testing.T, sigLine, alg string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(sigLine)
	if err != nil {
		t.Fatal(err)
	}
	copy(raw[:2], alg)
	return base64.StdEncoding.EncodeToString(raw)
}

func TestParsePublicKeyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString([]byte("just one line")),
		base64.StdEncoding.EncodeToString([]byte("untrusted comment: x\nQUJD\n"))} {
		if _, err := ParsePublicKey(bad); err == nil {
			t.Errorf("ParsePublicKey(%q) succeeded", bad)
		}
	}
	err := SignatureVerifier("garbage")(context.Background(), "1.2.3", sampleChecksums, fetchFrom(nil))
	if err == nil || !strings.Contains(err.Error(), "embedded release key") {
		t.Fatalf("a broken embedded key must fail closed, got %v", err)
	}
}

func TestEmbeddedPubkeyMatchesTauriConf(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "desktop", "src-tauri", "tauri.conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Plugins struct {
			Updater struct {
				Pubkey string `json:"pubkey"`
			} `json:"updater"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(b, &conf); err != nil {
		t.Fatal(err)
	}
	if conf.Plugins.Updater.Pubkey == "" || conf.Plugins.Updater.Pubkey != UpdaterPublicKey {
		t.Fatalf("selfupdate.UpdaterPublicKey differs from plugins.updater.pubkey in tauri.conf.json; there is one release key (ADR-0023), update both together:\n embedded: %s\n tauri:    %s", UpdaterPublicKey, conf.Plugins.Updater.Pubkey)
	}
	if _, err := ParsePublicKey(UpdaterPublicKey); err != nil {
		t.Fatalf("the embedded key does not parse: %v", err)
	}
}

func signedEnv(t *testing.T) (*testEnv, *selfupdatetest.Signer) {
	t.Helper()
	e := newEnv(t, "0.11.1", "0.12.0")
	signer := selfupdatetest.NewSigner(t)
	e.srv.Signer = signer
	e.u.VerifyChecksums = SignatureVerifier(signer.PublicKey())
	return e, signer
}

func TestUpdateInstallsASignedRelease(t *testing.T) {
	e, _ := signedEnv(t)
	if _, err := e.u.Update(context.Background(), Options{}); err != nil {
		t.Fatalf("Update: %v\n%s", err, e.log)
	}
	e.wantInstalled("0.12.0")
}

func TestUpdateRefusesReleaseWithoutSignature(t *testing.T) {
	e, _ := signedEnv(t)
	e.srv.Signer = nil // a release published before signing: no .minisig
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrNoSignature) {
		t.Fatalf("err = %v, want ErrNoSignature", err)
	}
	e.wantUntouched("0.11.1")
	assertArchiveNeverDownloaded(t, e)
}

func TestUpdateRefusesBadSignatureAndNeverDownloadsTheArchive(t *testing.T) {
	e, _ := signedEnv(t)
	e.srv.Signer = selfupdatetest.NewSigner(t) // signed by someone else
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
	e.wantUntouched("0.11.1")
	assertArchiveNeverDownloaded(t, e)
}

func TestUpdateRefusesSwappedChecksumsEvenIfTheArchiveMatchesThem(t *testing.T) {
	// The attack the signature exists for: archive AND checksums.txt replaced.
	e, signer := signedEnv(t)
	name := AssetName("0.12.0", runtime.GOOS, testArch)
	evil := selfupdatetest.Archive(t, runtime.GOOS, BinaryName(runtime.GOOS), selfupdatetest.Binary("6.6.6"))
	signed := signer.Sign(e.srv.Assets["0.12.0/checksums.txt"], selfupdatetest.TrustedComment("0.12.0"))
	e.srv.Signer = nil
	e.srv.Assets["0.12.0/"+SignatureName] = signed // the genuine signature of the genuine checksums
	e.srv.AddArchive(t, "0.12.0", runtime.GOOS, testArch, evil)
	// checksums.txt now lists the evil archive's digest (consistent), but the signature no longer matches it.
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature (name %s)", err, name)
	}
	e.wantUntouched("0.11.1")
}

func assertArchiveNeverDownloaded(t *testing.T, e *testEnv) {
	t.Helper()
	for _, p := range e.srv.Paths() {
		if strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".zip") {
			t.Fatalf("the archive was downloaded before the signature was verified: %v", e.srv.Paths())
		}
	}
}

// TestVerifyReleaseFilesFromEnv lets CI prove that what the publish job signed
// verifies with this package's verifier: it runs only when
// SMIND_VERIFY_CHECKSUMS, SMIND_VERIFY_SIG, SMIND_VERIFY_PUBKEY (Tauri form)
// and SMIND_VERIFY_VERSION are set.
func TestVerifyReleaseFilesFromEnv(t *testing.T) {
	sums, sigPath, pub, ver := os.Getenv("SMIND_VERIFY_CHECKSUMS"), os.Getenv("SMIND_VERIFY_SIG"), os.Getenv("SMIND_VERIFY_PUBKEY"), os.Getenv("SMIND_VERIFY_VERSION")
	if sums == "" || sigPath == "" || pub == "" || ver == "" {
		t.Skip("set SMIND_VERIFY_{CHECKSUMS,SIG,PUBKEY,VERSION} to verify real release files")
	}
	data, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := SignatureVerifier(pub)(context.Background(), ver, data, fetchFrom(map[string][]byte{SignatureName: sig})); err != nil {
		t.Fatalf("release signature rejected: %v", err)
	}
}
