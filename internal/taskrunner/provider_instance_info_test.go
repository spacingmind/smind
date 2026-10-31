package taskrunner

import (
	"reflect"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func TestProviderInfo_InstanceOverlay(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "rep", Extends: providers.IDClaudeNative, ModelsMode: providers.ModelsReplace,
			Models:          []providers.Model{{ID: "glm-5.3", Label: "glm-5.3"}, {ID: "glm-x", Label: "GLM X", Description: "d", Default: true}},
			DisallowedTools: []string{"WebSearch"}, InheritClaudeConfig: true},
		providers.Spec{ID: "add", Extends: providers.IDClaudeNative, ModelsMode: providers.ModelsAdd,
			Models: []providers.Model{{ID: "extra"}}},
	)
	if base, ok := ProviderInfoFor(Provider(providers.IDClaudeNative)); !ok || base.Instance != nil {
		t.Fatalf("built-in Instance = %+v, want nil", base.Instance)
	}
	rep, ok := r.ProviderCatalogFor("rep")
	if !ok || rep.Instance == nil {
		t.Fatalf("rep = %+v", rep)
	}
	wantModels := []InstanceModel{{ID: "glm-5.3"}, {ID: "glm-x", Label: "GLM X", Description: "d", Default: true}}
	if !reflect.DeepEqual(rep.Instance.Models, wantModels) || rep.Instance.AdditionalModels != nil {
		t.Fatalf("rep overlay = %+v", rep.Instance)
	}
	if !reflect.DeepEqual(rep.Instance.DisallowedTools, []string{"WebSearch"}) || !rep.Instance.InheritClaudeConfig {
		t.Fatalf("rep overlay = %+v", rep.Instance)
	}
	add, ok := r.ProviderCatalogFor("add")
	if !ok || add.Instance == nil || len(add.Instance.AdditionalModels) != 1 || add.Instance.AdditionalModels[0].ID != "extra" || add.Instance.Models != nil {
		t.Fatalf("add overlay = %+v", add.Instance)
	}
}
