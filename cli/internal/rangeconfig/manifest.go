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
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

var pathComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	// ManifestName is the declarative metadata filename within a range directory.
	ManifestName = "range.yml"
	// KindActiveDirectory identifies a Windows Active Directory range.
	KindActiveDirectory = "active-directory"
	// KindServiceRange identifies a service-oriented range.
	KindServiceRange = "service-range"
	// ProfileActiveDir selects the Active Directory scaffolding implementation.
	ProfileActiveDir = "active-directory"
	// ProfileTemplate selects the template-copy scaffolding implementation.
	ProfileTemplate = "template"
	// MaxAgentPromptBytes bounds range-authored model context before it reaches
	// the console or an API request.
	MaxAgentPromptBytes = 32 * 1024
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
// disabled by a range manifest.
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

// AgentSpec points at optional, range-owned model guidance. The path is
// resolved and confined beneath the selected range directory when loaded.
type AgentSpec struct {
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
}

// HandlerSpec selects either a CLI-owned implementation or a trusted
// executable shipped by the range. Executable paths are resolved and confined
// beneath the selected range directory by the rangecommand package.
type HandlerSpec struct {
	Type    string `yaml:"type" json:"type"`
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
}

// CommandSpec customizes one stable semantic command for a range. Safety
// classification deliberately does not live here: manifests may select an
// implementation or disable a capability, but cannot downgrade policy owned
// by the CLI and console.
type CommandSpec struct {
	Enabled     *bool        `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Detail      string       `yaml:"detail,omitempty" json:"detail,omitempty"`
	Protocol    string       `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Handler     HandlerSpec  `yaml:"handler,omitempty" json:"handler,omitempty"`
	Initializer *HandlerSpec `yaml:"initializer,omitempty" json:"initializer,omitempty"`
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
	Agent          AgentSpec               `yaml:"agent,omitempty" json:"agent,omitempty"`
	Commands       map[string]CommandSpec  `yaml:"commands,omitempty" json:"commands,omitempty"`
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
	if manifest.Agent.Prompt != "" {
		if err := validateRangeRelativePath(manifest.Agent.Prompt); err != nil {
			return nil, fmt.Errorf("agent.prompt %w", err)
		}
	}
	for name, command := range manifest.Commands {
		if err := validateCommandSpec(name, command); err != nil {
			return nil, err
		}
	}
	if err := validateLegacyScoreInitialization(&manifest); err != nil {
		return nil, err
	}
	health, declaresHealth := manifest.Commands["health"]
	if declaresHealth && health.Enabled != nil && !*health.Enabled {
		return nil, fmt.Errorf("commands.health is mandatory and cannot be disabled")
	}
	if manifest.Kind == KindServiceRange && !declaresHealth {
		return nil, fmt.Errorf("service-range manifests must declare commands.health")
	}
	manifest.Discovery.RangeTag = strings.TrimSpace(manifest.Discovery.RangeTag)
	if manifest.Discovery.RangeTag != "" && !pathComponent.MatchString(manifest.Discovery.RangeTag) {
		return nil, fmt.Errorf("discovery.range_tag %q is not a safe tag value", manifest.Discovery.RangeTag)
	}
	return &manifest, nil
}

func validateLegacyScoreInitialization(manifest *Manifest) error {
	for _, action := range manifest.Lifecycle.SessionInit {
		if action.Action != "generate_answer_key" {
			continue
		}
		score, declared := manifest.Commands["score"]
		builtinActiveDirectory := !declared && manifest.Kind == KindActiveDirectory
		if declared {
			builtinActiveDirectory = (score.Enabled == nil || *score.Enabled) &&
				score.Handler.Type == "builtin" && score.Handler.Profile == ProfileActiveDir
		}
		if !builtinActiveDirectory {
			return fmt.Errorf(
				"lifecycle.session_init action generate_answer_key requires the built-in active-directory score handler; executable scorers must use commands.score.initializer and disabled scorers must declare no scoring initialization",
			)
		}
	}
	return nil
}

var rangeCommandNames = map[string]struct{}{
	"health": {}, "validate": {}, "score": {}, "reset": {}, "scrub": {},
}

func validateCommandSpec(name string, command CommandSpec) error {
	if _, ok := rangeCommandNames[name]; !ok {
		return fmt.Errorf("commands.%s is unsupported", name)
	}
	if strings.TrimSpace(command.Description) != command.Description {
		return fmt.Errorf("commands.%s.description must not have surrounding whitespace", name)
	}
	if strings.TrimSpace(command.Detail) != command.Detail {
		return fmt.Errorf("commands.%s.detail must not have surrounding whitespace", name)
	}
	if command.Enabled != nil && !*command.Enabled {
		if command.Handler != (HandlerSpec{}) || command.Protocol != "" || command.Initializer != nil {
			return fmt.Errorf("commands.%s is disabled and must not declare a handler, initializer, or protocol", name)
		}
		return nil
	}
	switch command.Handler.Type {
	case "builtin":
		if !pathComponent.MatchString(command.Handler.Profile) {
			return fmt.Errorf("commands.%s.handler.profile must be a safe identifier", name)
		}
		if command.Handler.Path != "" {
			return fmt.Errorf("commands.%s builtin handler must not declare path", name)
		}
	case "executable":
		if err := validateRangeExecutablePath(command.Handler.Path); err != nil {
			return fmt.Errorf("commands.%s executable path %w", name, err)
		}
		if command.Handler.Profile != "" {
			return fmt.Errorf("commands.%s executable handler must not declare profile", name)
		}
	default:
		return fmt.Errorf("commands.%s handler type must be builtin or executable", name)
	}
	if command.Protocol == "" {
		return fmt.Errorf("commands.%s requires protocol", name)
	}
	if command.Initializer != nil {
		if name != "score" {
			return fmt.Errorf("commands.%s must not declare an initializer", name)
		}
		if command.Handler.Type != "executable" {
			return fmt.Errorf("commands.score initializer requires an executable score handler")
		}
		if command.Initializer.Type != "executable" {
			return fmt.Errorf("commands.score.initializer must be an executable handler with a path")
		}
		if err := validateRangeExecutablePath(command.Initializer.Path); err != nil {
			return fmt.Errorf("commands.score.initializer path %w", err)
		}
		if command.Initializer.Profile != "" {
			return fmt.Errorf("commands.score.initializer must not declare profile")
		}
	}
	return nil
}

func validateRangeExecutablePath(path string) error {
	return validateRangeRelativePath(path)
}

func validateRangeRelativePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("is required")
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay relative to the range")
	}
	return nil
}

// LoadAgentPrompt reads optional range-owned model guidance through the same
// strict manifest and path boundary used by the rest of range discovery.
func LoadAgentPrompt(labDir string) (string, error) {
	manifest, found, err := Load(labDir)
	if err != nil || !found || manifest.Agent.Prompt == "" {
		return "", err
	}
	root, err := filepath.EvalSymlinks(labDir)
	if err != nil {
		return "", fmt.Errorf("resolve range directory: %w", err)
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, manifest.Agent.Prompt))
	if err != nil {
		return "", fmt.Errorf("resolve agent prompt %q: %w", manifest.Agent.Prompt, err)
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("agent prompt %q escapes the range directory", manifest.Agent.Prompt)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect agent prompt %q: %w", manifest.Agent.Prompt, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("agent prompt %q is not a regular file", manifest.Agent.Prompt)
	}
	if info.Size() > MaxAgentPromptBytes {
		return "", fmt.Errorf("agent prompt %q exceeds %d bytes", manifest.Agent.Prompt, MaxAgentPromptBytes)
	}
	raw, err := os.ReadFile(candidate)
	if err != nil {
		return "", fmt.Errorf("read agent prompt %q: %w", manifest.Agent.Prompt, err)
	}
	if len(raw) > MaxAgentPromptBytes {
		return "", fmt.Errorf("agent prompt %q exceeds %d bytes", manifest.Agent.Prompt, MaxAgentPromptBytes)
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("agent prompt %q must be UTF-8 text", manifest.Agent.Prompt)
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("agent prompt %q must not contain NUL bytes", manifest.Agent.Prompt)
	}
	prompt := strings.TrimSpace(string(raw))
	if prompt == "" {
		return "", fmt.Errorf("agent prompt %q is empty", manifest.Agent.Prompt)
	}
	return prompt, nil
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
	if err := validateProviderPaths(provider, spec); err != nil {
		return err
	}
	profile := spec.ScaffoldProfile
	if profile == "" {
		profile = ProfileTemplate
	}
	if profile != ProfileActiveDir && profile != ProfileTemplate {
		return fmt.Errorf("infrastructure.%s.scaffold_profile %q is unsupported", provider, profile)
	}
	if err := validateProviderNetwork(provider, spec.Network); err != nil {
		return err
	}
	return validateProfileNetwork(provider, profile, spec.Network)
}

func validateProviderPaths(provider string, spec ProviderSpec) error {
	if !pathComponent.MatchString(provider) {
		return fmt.Errorf("infrastructure provider name %q is not a safe path component", provider)
	}
	if !pathComponent.MatchString(spec.Deployment) {
		return fmt.Errorf("infrastructure.%s.deployment %q is not a safe path component", provider, spec.Deployment)
	}
	if !pathComponent.MatchString(spec.TemplateEnvironment) {
		return fmt.Errorf("infrastructure.%s.template_environment %q is not a safe path component", provider, spec.TemplateEnvironment)
	}
	if spec.DefaultRegion != "" && !pathComponent.MatchString(spec.DefaultRegion) {
		return fmt.Errorf("infrastructure.%s.default_region %q is not a safe path component", provider, spec.DefaultRegion)
	}
	return nil
}

func validateProviderNetwork(provider string, networkSpec NetworkSpec) error {
	if networkSpec.CIDR != "" {
		ip, network, err := net.ParseCIDR(networkSpec.CIDR)
		if err != nil || ip.To4() == nil {
			return fmt.Errorf("infrastructure.%s.network.cidr %q is not an IPv4 CIDR", provider, networkSpec.CIDR)
		}
		ones, _ := network.Mask.Size()
		if ones != 16 || !ip.Equal(network.IP) {
			return fmt.Errorf("infrastructure.%s.network.cidr %q must be a canonical /16 network", provider, networkSpec.CIDR)
		}
		if !ip.IsPrivate() {
			return fmt.Errorf("infrastructure.%s.network.cidr %q must use private address space", provider, networkSpec.CIDR)
		}
	}
	return nil
}

func validateProfileNetwork(provider, profile string, network NetworkSpec) error {
	if profile == ProfileTemplate {
		if network.CIDR == "" {
			return fmt.Errorf("infrastructure.%s.network.cidr is required for template profiles", provider)
		}
		if network.Editable != nil && *network.Editable {
			return fmt.Errorf("infrastructure.%s.network.editable cannot be true for template profiles", provider)
		}
	}
	return nil
}
