// Package rangeconfig loads declarative metadata owned by each range.
package rangeconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

var pathComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	ManifestName        = "range.yml"
	KindActiveDirectory = "active-directory"
	KindServiceRange    = "service-range"
	ProfileActiveDir    = "active-directory"
	ProfileGOAT         = "goat"
	ProfileTemplate     = "template"
)

// ActionSpec names one allowlisted lifecycle action. The consumer, not the
// manifest, owns the implementation and allowlist for the action.
type ActionSpec struct {
	Action string `yaml:"action" json:"action"`
}

// VariantSpec describes whether the range can be randomized. A pointer keeps
// an omitted value distinguishable from an explicit false so older AD
// manifests retain their historical variant support.
type VariantSpec struct {
	Supported *bool `yaml:"supported,omitempty" json:"supported,omitempty"`
}

// NetworkSpec describes the environment CIDR exposed by a scaffold profile.
// Editable defaults to true for legacy AD ranges and false when explicitly
// declared by a range such as GOAT.
type NetworkSpec struct {
	CIDR     string `yaml:"cidr,omitempty" json:"cidr,omitempty"`
	Editable *bool  `yaml:"editable,omitempty" json:"editable,omitempty"`
}

// ProviderSpec tells env create how to scaffold one supported provider.
type ProviderSpec struct {
	Deployment          string      `yaml:"deployment" json:"deployment"`
	ScaffoldProfile     string      `yaml:"scaffold_profile" json:"scaffold_profile"`
	TemplateEnvironment string      `yaml:"template_environment" json:"template_environment"`
	DefaultRegion       string      `yaml:"default_region,omitempty" json:"default_region,omitempty"`
	Network             NetworkSpec `yaml:"network,omitempty" json:"network,omitempty"`
}

// InspectionSpec selects an allowlisted health/validation implementation.
// The manifest names the profile; the CLI owns the executable registry.
type InspectionSpec struct {
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

// DiscoverySpec declares the stable provider-side tag identifying a range.
// Cloud providers scope instances by Range=<range_tag> when it is present.
type DiscoverySpec struct {
	RangeTag string `yaml:"range_tag,omitempty" json:"range_tag,omitempty"`
}

// OperationsSpec selects an allowlisted implementation for infrastructure and
// provisioning behavior that cannot be represented by provider paths alone.
// The manifest names the profile; the CLI owns its implementation.
type OperationsSpec struct {
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

// Manifest is the shared, strict range.yml schema used by discovery,
// scaffolding, and session lifecycle handling.
type Manifest struct {
	SchemaVersion  int                     `yaml:"schema_version" json:"schema_version"`
	DisplayName    string                  `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Kind           string                  `yaml:"kind" json:"kind"`
	Variants       VariantSpec             `yaml:"variants,omitempty" json:"variants"`
	Infrastructure map[string]ProviderSpec `yaml:"infrastructure,omitempty" json:"infrastructure"`
	Inspection     InspectionSpec          `yaml:"inspection,omitempty" json:"inspection,omitempty"`
	Discovery      DiscoverySpec           `yaml:"discovery,omitempty" json:"discovery,omitempty"`
	Operations     OperationsSpec          `yaml:"operations,omitempty" json:"operations,omitempty"`
	Lifecycle      struct {
		SessionInit []ActionSpec `yaml:"session_init" json:"session_init"`
	} `yaml:"lifecycle" json:"lifecycle"`
}

// Decode parses one strict, single-document manifest.
func Decode(raw []byte) (*Manifest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, err
	}
	if manifest.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported schema_version %d (expected 1)", manifest.SchemaVersion)
	}
	manifest.Kind = strings.TrimSpace(manifest.Kind)
	if manifest.Kind != KindActiveDirectory && manifest.Kind != KindServiceRange {
		return nil, fmt.Errorf(
			"kind %q is unsupported (expected %q or %q)",
			manifest.Kind, KindActiveDirectory, KindServiceRange,
		)
	}
	for provider, spec := range manifest.Infrastructure {
		if err := validateProviderSpec(provider, spec); err != nil {
			return nil, err
		}
	}
	if manifest.Inspection.Profile != "" && !pathComponent.MatchString(manifest.Inspection.Profile) {
		return nil, fmt.Errorf("inspection.profile %q is not a safe identifier", manifest.Inspection.Profile)
	}
	if manifest.Operations.Profile != "" && !pathComponent.MatchString(manifest.Operations.Profile) {
		return nil, fmt.Errorf("operations.profile %q is not a safe identifier", manifest.Operations.Profile)
	}
	manifest.Discovery.RangeTag = strings.TrimSpace(manifest.Discovery.RangeTag)
	if manifest.Discovery.RangeTag != "" && !pathComponent.MatchString(manifest.Discovery.RangeTag) {
		return nil, fmt.Errorf("discovery.range_tag %q is not a safe tag value", manifest.Discovery.RangeTag)
	}
	return &manifest, nil
}

// Load reads range.yml below a lab directory. Missing manifests are reported
// separately so callers can retain compatibility with older AD ranges.
func Load(labDir string) (*Manifest, bool, error) {
	path := filepath.Join(labDir, ManifestName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read range manifest %s: %w", path, err)
	}
	manifest, err := Decode(raw)
	if err != nil {
		return nil, false, fmt.Errorf("parse range manifest %s: %w", path, err)
	}
	return manifest, true, nil
}

// EffectiveDisplayName returns the authored label or the directory name.
func (m *Manifest) EffectiveDisplayName(fallback string) string {
	if strings.TrimSpace(m.DisplayName) != "" {
		return strings.TrimSpace(m.DisplayName)
	}
	return fallback
}

// SupportsVariants returns the explicit policy, falling back to the historical
// rule that only Active Directory ranges are variant-compatible.
func (m *Manifest) SupportsVariants() bool {
	if m.Variants.Supported != nil {
		return *m.Variants.Supported
	}
	return m.Kind == KindActiveDirectory
}

// Provider returns explicit infrastructure metadata or the compatible legacy
// AD defaults. The provider directory remains the authority on whether a lab
// actually ships support; discovery intersects this result with that directory.
func (m *Manifest) Provider(provider string) (ProviderSpec, bool) {
	if spec, ok := m.Infrastructure[provider]; ok {
		return withDefaults(spec), true
	}
	if m.Kind != KindActiveDirectory {
		return ProviderSpec{}, false
	}
	switch provider {
	case "aws":
		return ProviderSpec{
			Deployment:          "goad-deployment",
			ScaffoldProfile:     ProfileActiveDir,
			TemplateEnvironment: "staging",
			DefaultRegion:       "us-west-1",
			Network:             editableNetwork(),
		}, true
	case "azure":
		return ProviderSpec{
			Deployment:          "goad-deployment",
			ScaffoldProfile:     ProfileActiveDir,
			TemplateEnvironment: "test",
			DefaultRegion:       "centralus",
			Network:             editableNetwork(),
		}, true
	default:
		return ProviderSpec{}, false
	}
}

// NetworkEditable resolves the optional boolean with an explicit fallback.
func (p ProviderSpec) NetworkEditable(fallback bool) bool {
	if p.Network.Editable != nil {
		return *p.Network.Editable
	}
	return fallback
}

func editableNetwork() NetworkSpec {
	value := true
	return NetworkSpec{Editable: &value}
}

func withDefaults(spec ProviderSpec) ProviderSpec {
	if spec.ScaffoldProfile == "" {
		spec.ScaffoldProfile = ProfileTemplate
	}
	return spec
}

func validateProviderSpec(provider string, spec ProviderSpec) error {
	if !pathComponent.MatchString(provider) {
		return fmt.Errorf("infrastructure provider name %q is not a safe path component", provider)
	}
	if !pathComponent.MatchString(spec.Deployment) {
		return fmt.Errorf("infrastructure.%s.deployment %q is not a safe path component", provider, spec.Deployment)
	}
	profile := spec.ScaffoldProfile
	if profile == "" {
		profile = ProfileTemplate
	}
	if profile != ProfileActiveDir && profile != ProfileTemplate {
		return fmt.Errorf("infrastructure.%s.scaffold_profile %q is unsupported", provider, profile)
	}
	if !pathComponent.MatchString(spec.TemplateEnvironment) {
		return fmt.Errorf("infrastructure.%s.template_environment %q is not a safe path component", provider, spec.TemplateEnvironment)
	}
	if spec.DefaultRegion != "" && !pathComponent.MatchString(spec.DefaultRegion) {
		return fmt.Errorf("infrastructure.%s.default_region %q is not a safe path component", provider, spec.DefaultRegion)
	}
	if spec.Network.CIDR != "" {
		ip, network, err := net.ParseCIDR(spec.Network.CIDR)
		if err != nil || ip.To4() == nil {
			return fmt.Errorf("infrastructure.%s.network.cidr %q is not an IPv4 CIDR", provider, spec.Network.CIDR)
		}
		ones, _ := network.Mask.Size()
		if ones != 16 || !ip.Equal(network.IP) {
			return fmt.Errorf("infrastructure.%s.network.cidr %q must be a canonical /16 network", provider, spec.Network.CIDR)
		}
		if !ip.IsPrivate() {
			return fmt.Errorf("infrastructure.%s.network.cidr %q must use private address space", provider, spec.Network.CIDR)
		}
	}
	if profile == ProfileTemplate {
		if spec.Network.CIDR == "" {
			return fmt.Errorf("infrastructure.%s.network.cidr is required for template profiles", provider)
		}
		if spec.Network.Editable != nil && *spec.Network.Editable {
			return fmt.Errorf("infrastructure.%s.network.editable cannot be true for template profiles", provider)
		}
	}
	return nil
}
