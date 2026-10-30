package providers

import (
	"strings"
	"testing"
)

func TestVendor_BuiltinTable(t *testing.T) {
	t.Parallel()
	reg := New()
	for id, want := range map[string]string{
		IDClaudeNative: VendorAnthropic,
		IDCodexNative:  VendorOpenAI,
		IDGLM:          VendorZAI,
		IDKimi:         VendorGeneric,
	} {
		s, ok := reg.Get(id)
		if !ok {
			t.Fatalf("%s: not registered", id)
		}
		if s.Vendor != want {
			t.Fatalf("%s: Vendor = %q, want %q", id, s.Vendor, want)
		}
	}
}

func TestVendor_InstanceInheritsBase(t *testing.T) {
	t.Parallel()
	reg := New()
	if err := reg.Register(Spec{ID: "work", Extends: IDClaudeNative}); err != nil {
		t.Fatal(err)
	}
	s, ok := reg.Get("work")
	if !ok {
		t.Fatal("instance not registered")
	}
	if s.Vendor != VendorAnthropic {
		t.Fatalf("Vendor = %q, want %q", s.Vendor, VendorAnthropic)
	}
}

func TestVendor_CompatibleInstanceIsGenericUnlessSet(t *testing.T) {
	t.Parallel()
	reg := New()
	if err := reg.Register(Spec{ID: "compat", Extends: IDClaudeNative, Env: map[string]string{"ANTHROPIC_BASE_URL": "http://x"}}); err != nil {
		t.Fatal(err)
	}
	if s, _ := reg.Get("compat"); s.Vendor != VendorGeneric {
		t.Fatalf("Vendor = %q, want %q", s.Vendor, VendorGeneric)
	}
	reg2 := New()
	if err := reg2.Register(Spec{ID: "compat", Extends: IDClaudeNative, Env: map[string]string{"ANTHROPIC_BASE_URL": "http://x"}, Vendor: VendorZAI}); err != nil {
		t.Fatal(err)
	}
	if s, _ := reg2.Get("compat"); s.Vendor != VendorZAI {
		t.Fatalf("Vendor = %q, want %q", s.Vendor, VendorZAI)
	}
}

func TestVendor_RejectsUnknownVendor(t *testing.T) {
	t.Parallel()
	reg := New()
	err := reg.Register(Spec{ID: "acme", Extends: IDClaudeNative, Vendor: "acme"})
	if err == nil || !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("err = %v, want an unknown-vendor error", err)
	}
}

func TestVendorOf_UnknownIsGeneric(t *testing.T) {
	t.Parallel()
	if got := VendorOf("nope-not-registered"); got != VendorGeneric {
		t.Fatalf("VendorOf(unknown) = %q, want %q", got, VendorGeneric)
	}
}

func TestInstance_NewFieldsCopied(t *testing.T) {
	t.Parallel()
	reg := New()
	if err := reg.Register(Spec{ID: "fields", Extends: IDGLM, Thinking: "hidden", Recipe: "r", InheritClaudeConfig: true}); err != nil {
		t.Fatal(err)
	}
	s, ok := reg.Get("fields")
	if !ok {
		t.Fatal("instance not registered")
	}
	if s.Thinking != "hidden" || s.Recipe != "r" || !s.InheritClaudeConfig {
		t.Fatalf("instance fields = %q/%q/%v, want hidden/r/true", s.Thinking, s.Recipe, s.InheritClaudeConfig)
	}
}
