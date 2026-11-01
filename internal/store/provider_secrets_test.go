package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestProviderSecretStore_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if err := s.SetProviderSecret("inst-1", "api_key", "sentinel-first-0000"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetProviderSecret("inst-1", "api_key")
	if err != nil || !ok || got != "sentinel-first-0000" {
		t.Fatalf("get = %q ok=%v err=%v, want sentinel-first-0000", got, ok, err)
	}
	if _, ok, _ := s.GetProviderSecret("inst-1", "other"); ok {
		t.Fatal("secret reported under a name that was never set")
	}
	if _, ok, _ := s.GetProviderSecret("inst-2", "api_key"); ok {
		t.Fatal("secret reported for another instance")
	}

	before, err := s.ListProviderSecrets("inst-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("list = %v, want one entry", before)
	}
	first := before["api_key"].Fingerprint

	if err := s.SetProviderSecret("inst-1", "api_key", "sentinel-second-1111"); err != nil {
		t.Fatal(err)
	}
	got, ok, err = s.GetProviderSecret("inst-1", "api_key")
	if err != nil || !ok || got != "sentinel-second-1111" {
		t.Fatalf("get after overwrite = %q ok=%v err=%v, want sentinel-second-1111", got, ok, err)
	}
	after, err := s.ListProviderSecrets("inst-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("list after overwrite = %v, want one entry", after)
	}
	if second := after["api_key"].Fingerprint; second == first || second != SecretFingerprint("sentinel-second-1111") {
		t.Fatalf("fingerprint after overwrite = %q, first was %q, want the new value's", second, first)
	}

	if err := s.ClearProviderSecret("inst-1", "api_key"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetProviderSecret("inst-1", "api_key"); ok {
		t.Fatal("secret still reported after clear")
	}
	if err := s.ClearProviderSecret("inst-1", "api_key"); err != nil {
		t.Fatalf("clearing twice error = %v, want nil", err)
	}
}

func TestProviderSecretStore_ListShowsFingerprintOnly(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	const sentinel = "sentinel-NOT-A-REAL-KEY-12345"
	if err := s.SetProviderSecret("inst-1", "api_key", sentinel); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListProviderSecrets("inst-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("list = %v, want one entry", got)
	}
	info, ok := got["api_key"]
	if !ok {
		t.Fatalf("list = %v, want name api_key", got)
	}
	if len(info.Fingerprint) != 4 {
		t.Fatalf("fingerprint %q has %d chars, want 4", info.Fingerprint, len(info.Fingerprint))
	}
	if info.Fingerprint != SecretFingerprint(sentinel) {
		t.Fatalf("fingerprint = %q, want %q", info.Fingerprint, SecretFingerprint(sentinel))
	}
	if rendered := fmt.Sprintf("%+v", got); strings.Contains(rendered, sentinel) {
		t.Fatalf("list rendered as %q, which contains the secret value", rendered)
	}
}

func TestProviderSecretStore_ErrorsNeverContainValue(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	const sentinel = "sentinel-NOT-A-REAL-KEY-67890"
	if err := s.SetProviderSecret("", "N", sentinel); err == nil {
		t.Fatal("SetProviderSecret with empty instance id error = nil, want error")
	} else if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error text %q contains the secret value", err.Error())
	}
	if err := s.SetProviderSecret("inst-1", "", sentinel); err == nil {
		t.Fatal("SetProviderSecret with empty name error = nil, want error")
	} else if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error text %q contains the secret value", err.Error())
	}
	if err := s.SetProviderSecret("inst-1", "N", ""); err == nil {
		t.Fatal("SetProviderSecret with empty value error = nil, want error")
	}
}

func TestProviderSecretStore_DeleteAndPrune(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	for _, inst := range []string{"a", "b", "c"} {
		if err := s.SetProviderSecret(inst, "api_key", "value-of-"+inst); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteProviderSecrets("a"); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM provider_secrets`); got != 2 {
		t.Fatalf("rows after delete of a = %d, want 2", got)
	}
	if got, _ := s.ListProviderSecrets("a"); len(got) != 0 {
		t.Fatalf("secrets of a after delete = %v, want none", got)
	}

	deleted, err := s.PruneProviderSecrets([]string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("prune keeping b deleted %d, want 1", deleted)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM provider_secrets`); got != 1 {
		t.Fatalf("rows after prune keeping b = %d, want 1", got)
	}
	if got, _ := s.ListProviderSecrets("c"); len(got) != 0 {
		t.Fatalf("secrets of c after prune = %v, want none", got)
	}

	deleted, err = s.PruneProviderSecrets(nil)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("prune with nil keep list deleted %d, want 1", deleted)
	}
	if got := countRows(t, s, `SELECT COUNT(*) FROM provider_secrets`); got != 0 {
		t.Fatalf("rows after pruning everything = %d, want 0", got)
	}
}

func TestPruneProviderSecrets_KeepsPluginProviderIDs(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	for _, id := range []string{"acme/agent", "custom-one"} {
		if err := s.SetProviderSecret(id, "K", "value-1234"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PruneProviderSecrets(nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListProviderSecrets("acme/agent"); len(got) != 1 {
		t.Fatalf("a plugin provider's secret was pruned with its plugin disabled: %v", got)
	}
	if got, _ := s.ListProviderSecrets("custom-one"); len(got) != 0 {
		t.Fatalf("an orphaned custom secret survived: %v", got)
	}
}
