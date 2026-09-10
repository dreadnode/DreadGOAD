// Package lifecycle runs declarative, range-owned lifecycle actions.
package lifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
	"go.yaml.in/yaml/v3"
)

const (
	manifestName            = "range.yml"
	actionGenerateAnswerKey = "generate_answer_key"
)

type actionRunner func(*config.Config, string) (Result, error)

var sessionInitActions = map[string]actionRunner{
	actionGenerateAnswerKey: generateAnswerKey,
}

// ActionSpec names one allowlisted lifecycle action. Range manifests never
// contain commands or script paths: selecting a config must not execute code
// supplied by that config.
type ActionSpec struct {
	Action string `yaml:"action"`
}

// Manifest describes stable metadata and lifecycle behavior owned by a range.
type Manifest struct {
	SchemaVersion int    `yaml:"schema_version"`
	Kind          string `yaml:"kind"`
	Lifecycle     struct {
		SessionInit []ActionSpec `yaml:"session_init"`
	} `yaml:"lifecycle"`
}

// Result is the machine-readable outcome of one initialization action.
type Result struct {
	Action    string   `json:"action"`
	Status    string   `json:"status"`
	Artifacts []string `json:"artifacts,omitempty"`
	Message   string   `json:"message,omitempty"`
}

// RunSessionInit runs the selected range's declared session initialization
// actions. Missing manifests are a backward-compatible no-op. A variant whose
// target has not been generated yet reports its actions as pending; callers
// should invoke this again after scaffolding.
func RunSessionInit(cfg *config.Config, outputDir string) ([]Result, error) {
	manifest, found, err := loadSelectedManifest(cfg)
	if err != nil {
		return nil, err
	}
	if !found || len(manifest.Lifecycle.SessionInit) == 0 {
		return []Result{}, nil
	}
	if err := validateActions(manifest.Lifecycle.SessionInit); err != nil {
		return nil, err
	}

	pending, err := variantTargetPending(cfg)
	if err != nil {
		return nil, err
	}
	if pending {
		results := make([]Result, 0, len(manifest.Lifecycle.SessionInit))
		for _, spec := range manifest.Lifecycle.SessionInit {
			results = append(results, Result{
				Action:  spec.Action,
				Status:  "pending",
				Message: "variant configuration is not available until scaffolding completes",
			})
		}
		return results, nil
	}

	results := make([]Result, 0, len(manifest.Lifecycle.SessionInit))
	var actionErrors []error
	for _, spec := range manifest.Lifecycle.SessionInit {
		result, actionErr := runAction(cfg, outputDir, spec.Action)
		results = append(results, result)
		if actionErr != nil {
			actionErrors = append(actionErrors, actionErr)
		}
	}
	return results, errors.Join(actionErrors...)
}

func loadSelectedManifest(cfg *config.Config) (*Manifest, bool, error) {
	for _, path := range manifestCandidates(cfg) {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("read range manifest %s: %w", path, err)
		}
		manifest, err := decodeManifest(raw)
		if err != nil {
			return nil, false, fmt.Errorf("parse range manifest %s: %w", path, err)
		}
		return manifest, true, nil
	}
	return nil, false, nil
}

func manifestCandidates(cfg *config.Config) []string {
	if cfg.ActiveEnvironment().Variant {
		source, target := cfg.ResolvedVariantPaths()
		// Prefer the generated target's copy. Falling back to the source lets a
		// not-yet-scaffolded variant declare which actions will become runnable.
		return []string{
			filepath.Join(target, manifestName),
			filepath.Join(source, manifestName),
		}
	}
	return []string{filepath.Join(cfg.LabPath(), manifestName)}
}

func decodeManifest(raw []byte) (*Manifest, error) {
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
	if manifest.Kind == "" {
		return nil, fmt.Errorf("kind is required")
	}
	return &manifest, nil
}

func validateActions(actions []ActionSpec) error {
	seen := make(map[string]struct{}, len(actions))
	for _, spec := range actions {
		if _, supported := sessionInitActions[spec.Action]; !supported {
			return fmt.Errorf("unsupported session_init action %q", spec.Action)
		}
		if _, duplicate := seen[spec.Action]; duplicate {
			return fmt.Errorf("duplicate session_init action %q", spec.Action)
		}
		seen[spec.Action] = struct{}{}
	}
	return nil
}

func variantTargetPending(cfg *config.Config) (bool, error) {
	if !cfg.ActiveEnvironment().Variant {
		return false, nil
	}
	_, target := cfg.ResolvedVariantPaths()
	if target == "" {
		return true, nil
	}
	_, err := os.Stat(filepath.Join(target, "data", "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect variant configuration: %w", err)
	}
	return false, nil
}

func runAction(cfg *config.Config, outputDir, action string) (Result, error) {
	runner, ok := sessionInitActions[action]
	if !ok {
		// validateActions makes this unreachable. Keeping the guard next to the
		// dispatcher ensures a future caller cannot accidentally bypass it.
		return Result{Action: action, Status: "failed"}, fmt.Errorf("unsupported session_init action %q", action)
	}
	return runner(cfg, outputDir)
}

func generateAnswerKey(cfg *config.Config, outputDir string) (Result, error) {
	result := Result{Action: actionGenerateAnswerKey}
	configPath, err := cfg.ResolvedLabConfigPath()
	if errors.Is(err, config.ErrLabConfigNotFound) {
		result.Status = "pending"
		result.Message = "lab configuration is not available yet"
		return result, nil
	}
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("%s: %w", actionGenerateAnswerKey, err)
	}

	if outputDir == "" {
		err := fmt.Errorf("output directory is required")
		result.Status = "failed"
		result.Message = err.Error()
		return result, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("create session artifact directory: %w", err)
	}
	if err := os.Chmod(outputDir, 0o700); err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("secure session artifact directory: %w", err)
	}

	ak, err := scoreboard.GenerateAnswerKey(configPath)
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("%s: %w", actionGenerateAnswerKey, err)
	}
	outputPath := filepath.Join(outputDir, "answer_key.json")
	if err := scoreboard.WriteAnswerKey(ak, outputPath); err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("%s: %w", actionGenerateAnswerKey, err)
	}

	result.Status = "completed"
	result.Artifacts = []string{outputPath}
	result.Message = fmt.Sprintf("generated %d scoring objectives", ak.TotalObjectives)
	return result, nil
}
