package accounts

import "testing"

func TestModels_ValidateAndMatch(t *testing.T) {
	t.Parallel()

	if err := ValidateModels([]string{"glm-*", "claude-3-?"}); err != nil {
		t.Fatalf("ValidateModels(good) = %v", err)
	}
	if err := ValidateModels([]string{"[bad"}); err == nil {
		t.Fatal("ValidateModels([bad) = nil, want error")
	}
	if enc, err := EncodeModels(nil); err != nil || enc != "" {
		t.Fatalf("EncodeModels(nil) = %q, %v", enc, err)
	}

	a := Account{Models: []string{"glm-*"}}
	if !a.MatchesModel("glm-4.6") || a.MatchesModel("claude-opus") || a.MatchesModel("") {
		t.Fatal("MatchesModel mismatch")
	}
	if (Account{}).MatchesModel("glm-4.6") {
		t.Fatal("account with no list must not explicitly match")
	}
}

func TestRegistry_ModelsRoundTrip(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)
	created, err := reg.AddWithCredentialModels("anthropic", "glm", "sk-k", "", []string{"glm-*"})
	if err != nil {
		t.Fatalf("AddWithCredentialModels() error = %v", err)
	}
	got, err := reg.Get(created.ID)
	if err != nil || len(got.Models) != 1 || got.Models[0] != "glm-*" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if _, err := reg.SetModels(created.ID, []string{"[bad"}); err == nil {
		t.Fatal("SetModels(bad glob) succeeded")
	}
	if _, err := reg.SetModels(created.ID, nil); err != nil {
		t.Fatalf("SetModels(clear) error = %v", err)
	}
	got, _ = reg.Get(created.ID)
	if len(got.Models) != 0 {
		t.Fatalf("Models after clear = %v", got.Models)
	}
}
