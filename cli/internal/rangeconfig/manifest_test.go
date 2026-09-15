package rangeconfig

import (
	"strings"
	"testing"
)

func boolPtr(value bool) *bool { return &value }

func TestDecodeGOATCreationMetadata(t *testing.T) {
	manifest, err := Decode([]byte(`schema_version: 1
display_name: GOAT
kind: service-range
variants:
  supported: false
inspection:
  profile: goat
operations:
  profile: goat
discovery:
  range_tag: GOAT
infrastructure:
  azure:
    deployment: goat-deployment
    scaffold_profile: template
    template_environment: goat-dev
    default_region: centralus
    network:
      cidr: 10.50.0.0/16
      editable: false
lifecycle:
  session_init: []
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.EffectiveDisplayName("fallback") != "GOAT" || manifest.SupportsVariants() {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if manifest.Inspection.Profile != "goat" {
		t.Fatalf("inspection profile = %q, want goat", manifest.Inspection.Profile)
	}
	if manifest.Operations.Profile != ProfileGOAT {
		t.Fatalf("operations profile = %q, want %s", manifest.Operations.Profile, ProfileGOAT)
	}
	if manifest.Discovery.RangeTag != "GOAT" {
		t.Fatalf("discovery range tag = %q, want GOAT", manifest.Discovery.RangeTag)
	}
	spec, ok := manifest.Provider("azure")
	if !ok || spec.Deployment != "goat-deployment" || spec.NetworkEditable(true) {
		t.Fatalf("unexpected provider spec: %#v, %v", spec, ok)
	}
}

func TestLegacyActiveDirectoryDefaultsRemainAvailable(t *testing.T) {
	manifest := &Manifest{Kind: KindActiveDirectory}
	if !manifest.SupportsVariants() {
		t.Fatal("legacy Active Directory range should support variants")
	}
	azure, ok := manifest.Provider("azure")
	if !ok || azure.TemplateEnvironment != "test" || azure.Deployment != "goad-deployment" {
		t.Fatalf("unexpected Azure defaults: %#v", azure)
	}
}

func TestDecodeRejectsUnsafeAndUnknownMetadata(t *testing.T) {
	for _, body := range []string{
		"schema_version: 1\nkind: mystery\n",
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: ../../escape\n    template_environment: goat-dev\n",
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: x\n    template_environment: seed\n    default_region: ../../escape\n",
		"schema_version: 1\nkind: service-range\ncommand: ./run-me\n",
		"schema_version: 1\nkind: service-range\ninspection:\n  profile: ../../run-me\n",
		"schema_version: 1\nkind: service-range\noperations:\n  profile: ../../run-me\n",
		"schema_version: 1\nkind: service-range\ndiscovery:\n  range_tag: ../../escape\n",
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: x\n    scaffold_profile: template\n    template_environment: seed\n",
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: x\n    scaffold_profile: template\n    template_environment: seed\n    network:\n      cidr: 10.60.0.0/24\n",
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: x\n    scaffold_profile: template\n    template_environment: seed\n    network:\n      cidr: 10.60.0.0/16\n      editable: true\n",
	} {
		if _, err := Decode([]byte(body)); err == nil {
			t.Fatalf("Decode(%q) unexpectedly succeeded", body)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Fatal("error must be actionable")
		}
	}
}

func TestExplicitVariantPolicyOverridesKind(t *testing.T) {
	manifest := &Manifest{Kind: KindActiveDirectory, Variants: VariantSpec{Supported: boolPtr(false)}}
	if manifest.SupportsVariants() {
		t.Fatal("explicit false must override the kind default")
	}
}
