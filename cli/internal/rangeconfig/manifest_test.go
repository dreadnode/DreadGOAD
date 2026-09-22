package rangeconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func boolPtr(value bool) *bool { return &value }

func TestDecodeServiceRangeCreationMetadata(t *testing.T) {
	manifest, err := Decode([]byte(`schema_version: 1
display_name: Example Service Range
kind: service-range
agent:
  prompt: prompts/agent.md
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
variants:
  supported: false
inspection:
  profile: service-test
operations:
  profile: service-test
discovery:
  range_tag: SERVICE
infrastructure:
  azure:
    deployment: service-deployment
    scaffold_profile: template
    template_environment: service-seed
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
	if manifest.EffectiveDisplayName("fallback") != "Example Service Range" || manifest.SupportsVariants() {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if manifest.Inspection.Profile != "service-test" {
		t.Fatalf("inspection profile = %q, want service-test", manifest.Inspection.Profile)
	}
	if manifest.Operations.Profile != "service-test" {
		t.Fatalf("operations profile = %q, want service-test", manifest.Operations.Profile)
	}
	if manifest.Discovery.RangeTag != "SERVICE" {
		t.Fatalf("discovery range tag = %q, want SERVICE", manifest.Discovery.RangeTag)
	}
	if manifest.Agent.Prompt != "prompts/agent.md" {
		t.Fatalf("agent prompt = %q", manifest.Agent.Prompt)
	}
	spec, ok := manifest.Provider("azure")
	if !ok || spec.Deployment != "service-deployment" || spec.NetworkEditable(true) {
		t.Fatalf("unexpected provider spec: %#v, %v", spec, ok)
	}
}

func TestDecodeRejectsUnsafeAgentPromptPath(t *testing.T) {
	_, err := Decode([]byte("schema_version: 1\nkind: active-directory\nagent:\n  prompt: ../outside.md\n"))
	if err == nil || !strings.Contains(err.Error(), "agent.prompt") {
		t.Fatalf("Decode() error = %v, want confined agent prompt error", err)
	}
}

func TestLoadAgentPromptValidatesRangeOwnedText(t *testing.T) {
	writeRange := func(t *testing.T, content []byte) (string, string) {
		t.Helper()
		labDir := t.TempDir()
		manifest := "schema_version: 1\nkind: active-directory\nagent:\n  prompt: prompts/agent.md\n"
		if err := os.WriteFile(filepath.Join(labDir, ManifestName), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		prompt := filepath.Join(labDir, "prompts", "agent.md")
		if err := os.MkdirAll(filepath.Dir(prompt), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prompt, content, 0o644); err != nil {
			t.Fatal(err)
		}
		return labDir, prompt
	}

	labDir, _ := writeRange(t, []byte("  This range models a web service.  \n"))
	got, err := LoadAgentPrompt(labDir)
	if err != nil || got != "This range models a web service." {
		t.Fatalf("LoadAgentPrompt() = %q, %v", got, err)
	}

	for name, content := range map[string][]byte{
		"oversized":     bytes.Repeat([]byte("x"), MaxAgentPromptBytes+1),
		"invalid UTF-8": {0xff, 0xfe},
		"NUL byte":      []byte("before\x00after"),
	} {
		t.Run(name, func(t *testing.T) {
			badLab, _ := writeRange(t, content)
			if _, err := LoadAgentPrompt(badLab); err == nil {
				t.Fatal("LoadAgentPrompt() accepted invalid prompt")
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("symlink escape", func(t *testing.T) {
			symlinkLab, prompt := writeRange(t, []byte("placeholder"))
			outside := filepath.Join(t.TempDir(), "outside.md")
			if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(prompt); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, prompt); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAgentPrompt(symlinkLab); err == nil || !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("LoadAgentPrompt() error = %v, want escape rejection", err)
			}
		})
	}
}

func TestLoadAgentPromptIsOptional(t *testing.T) {
	labDir := t.TempDir()
	manifest := "schema_version: 1\nkind: active-directory\n"
	if err := os.WriteFile(filepath.Join(labDir, ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadAgentPrompt(labDir)
	if err != nil || got != "" {
		t.Fatalf("LoadAgentPrompt() = %q, %v; want empty optional prompt", got, err)
	}
}

func TestDecodeCommandHandlers(t *testing.T) {
	manifest, err := Decode([]byte(`schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
  validate:
    description: Validate service state
    detail: Uses the range-owned service probes
    protocol: validate/v1
    handler:
      type: executable
      path: commands/validate
  scrub:
    enabled: false
`))
	if err != nil {
		t.Fatal(err)
	}
	validate := manifest.Commands["validate"]
	if validate.Handler.Type != "executable" || validate.Handler.Path != "commands/validate" {
		t.Fatalf("validate command = %#v", validate)
	}
	if scrub := manifest.Commands["scrub"]; scrub.Enabled == nil || *scrub.Enabled {
		t.Fatalf("scrub command = %#v", scrub)
	}
}

func TestDecodeRequiresMandatoryHealth(t *testing.T) {
	for name, manifest := range map[string]string{
		"service missing":  "schema_version: 1\nkind: service-range\n",
		"service disabled": "schema_version: 1\nkind: service-range\ncommands:\n  health:\n    enabled: false\n",
		"AD disabled":      "schema_version: 1\nkind: active-directory\ncommands:\n  health:\n    enabled: false\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(manifest))
			if err == nil || !strings.Contains(err.Error(), "health") {
				t.Fatalf("Decode() error = %v, want mandatory health error", err)
			}
		})
	}
}

func TestDecodeKeepsImplicitActiveDirectoryHealth(t *testing.T) {
	if _, err := Decode([]byte("schema_version: 1\nkind: active-directory\n")); err != nil {
		t.Fatalf("legacy Active Directory manifest rejected: %v", err)
	}
}

func TestDecodeRejectsUnsafeCommandDeclarations(t *testing.T) {
	tests := []string{
		"commands:\n  surprise:\n    enabled: false\n",
		"commands:\n  validate:\n    protocol: validate/v1\n    handler:\n      type: executable\n      path: /tmp/validate\n",
		"commands:\n  validate:\n    protocol: validate/v1\n    handler:\n      type: executable\n      path: ../validate\n",
		"commands:\n  reset:\n    enabled: false\n    protocol: operation/v1\n    handler:\n      type: builtin\n      profile: active-directory\n",
		"commands:\n  validate:\n    protocol: validate/v1\n    handler:\n      type: builtin\n      profile: active-directory\n    initializer:\n      type: executable\n      path: init\n",
	}
	for _, fragment := range tests {
		_, err := Decode([]byte("schema_version: 1\nkind: service-range\n" + fragment))
		if err == nil {
			t.Fatalf("Decode() accepted:\n%s", fragment)
		}
	}
}

func TestDecodeRejectsLegacyAnswerKeyActionForNonBuiltinScorers(t *testing.T) {
	tests := []string{
		`schema_version: 1
kind: active-directory
commands:
  score:
    enabled: false
lifecycle:
  session_init:
    - action: generate_answer_key
`,
		`schema_version: 1
kind: active-directory
commands:
  score:
    protocol: score/v1
    handler: {type: executable, path: commands/score}
    initializer: {type: executable, path: commands/init-score}
lifecycle:
  session_init:
    - action: generate_answer_key
		`,
	}
	for _, manifest := range tests {
		_, err := Decode([]byte(strings.TrimSpace(manifest) + "\n"))
		if err == nil || !strings.Contains(err.Error(), "generate_answer_key") {
			t.Fatalf("Decode() error = %v, want incompatible generate_answer_key rejection", err)
		}
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
		"schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: ../../escape\n    template_environment: service-seed\n",
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
