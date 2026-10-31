package providers

// Vendor profiles: which backend's rate-limit wording a provider speaks
// (consumed by internal/runs/failure via Input.Vendor).
const (
	VendorZAI       = "zai"
	VendorAnthropic = "anthropic"
	VendorOpenAI    = "openai"
	VendorGeneric   = "generic"
)

// ValidVendor reports whether v names a vendor profile ("" is valid: the
// generic profile).
func ValidVendor(v string) bool {
	switch v {
	case "", VendorZAI, VendorAnthropic, VendorOpenAI, VendorGeneric:
		return true
	}
	return false
}

// builtinVendors maps a built-in id to its vendor profile.
var builtinVendors = map[string]string{
	IDClaudeNative: VendorAnthropic,
	IDCodexNative:  VendorOpenAI,
	IDGLM:          VendorZAI,
	IDKimi:         VendorGeneric,
}

// builtinsWithVendors is builtins with Vendor filled from builtinVendors
// where the table row leaves it empty.
func builtinsWithVendors() []Spec {
	specs := builtins()
	for i := range specs {
		if specs[i].Vendor == "" {
			specs[i].Vendor = builtinVendors[specs[i].ID]
		}
	}
	return specs
}

// VendorOf is the vendor profile of provider id (VendorGeneric for an
// unknown id or an empty profile).
func VendorOf(id string) string {
	s, ok := Default().Get(id)
	if !ok || s.Vendor == "" {
		return VendorGeneric
	}
	return s.Vendor
}
