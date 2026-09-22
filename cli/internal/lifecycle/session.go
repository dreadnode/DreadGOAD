// Package lifecycle runs declarative, range-owned lifecycle actions.
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangecommand"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
)

const (
	manifestName            = "range.yml"
	actionGenerateAnswerKey = "generate_answer_key"
)

type actionRunner func(*config.Config, string) (Result, error)

var sessionInitActions = map[string]actionRunner{
	actionGenerateAnswerKey: generateAnswerKey,
}

// Keep the lifecycle package's existing internal names while sharing the
// manifest schema with discovery and scaffolding.
type ActionSpec = rangeconfig.ActionSpec
type Manifest = rangeconfig.Manifest

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
	if !found {
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
		results := make([]Result, 0, len(manifest.Lifecycle.SessionInit)+1)
		for _, spec := range manifest.Lifecycle.SessionInit {
			results = append(results, Result{
				Action:  spec.Action,
				Status:  "pending",
				Message: "variant configuration is not available until scaffolding completes",
			})
		}
		if score := manifest.Commands["score"]; score.Initializer != nil {
			results = append(results, Result{
				Action: "initialize_score", Status: "pending",
				Message: "variant configuration is not available until scaffolding completes",
			})
		}
		return results, nil
	}

	results := make([]Result, 0, len(manifest.Lifecycle.SessionInit)+1)
	var actionErrors []error
	for _, spec := range manifest.Lifecycle.SessionInit {
		result, actionErr := runAction(cfg, outputDir, spec.Action)
		results = append(results, result)
		if actionErr != nil {
			actionErrors = append(actionErrors, actionErr)
		}
	}
	if score := manifest.Commands["score"]; score.Initializer != nil {
		result, actionErr := initializeScore(cfg, outputDir)
		results = append(results, result)
		if actionErr != nil {
			actionErrors = append(actionErrors, actionErr)
		}
	}
	return results, errors.Join(actionErrors...)
}

func initializeScore(cfg *config.Config, outputDir string) (Result, error) {
	result := Result{Action: "initialize_score"}
	capability, err := rangecommand.Require(cfg, "score")
	if err != nil {
		result.Status, result.Message = "failed", err.Error()
		return result, err
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := rangecommand.ExecuteScoreInitializer(
		ctx, cfg, capability, outputDir, &stdout, &stderr,
	); err != nil {
		result.Status, result.Message = "failed", strings.TrimSpace(stderr.String()+" "+err.Error())
		return result, err
	}
	for _, line := range reverseLines(stdout.Bytes()) {
		var payload struct {
			Schema    string   `json:"schema"`
			Artifacts []string `json:"artifacts"`
			Message   string   `json:"message"`
		}
		if json.Unmarshal(line, &payload) != nil || payload.Schema != "session-init/v1" {
			continue
		}
		artifacts, err := confinedArtifacts(outputDir, payload.Artifacts)
		if err != nil {
			result.Status, result.Message = "failed", err.Error()
			return result, err
		}
		result.Status, result.Artifacts, result.Message = "completed", artifacts, payload.Message
		return result, nil
	}
	err = fmt.Errorf("score initializer returned no session-init/v1 result")
	result.Status, result.Message = "failed", err.Error()
	return result, err
}

func reverseLines(output []byte) [][]byte {
	lines := bytes.Split(output, []byte{'\n'})
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return lines
}

func confinedArtifacts(outputDir string, artifacts []string) ([]string, error) {
	root, err := filepath.EvalSymlinks(outputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve score artifact directory: %w", err)
	}
	confined := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !filepath.IsAbs(artifact) {
			return nil, fmt.Errorf("score initializer artifact %q must be an absolute path", artifact)
		}
		path, err := filepath.EvalSymlinks(artifact)
		if err != nil {
			return nil, fmt.Errorf("resolve score artifact %q: %w", artifact, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("score initializer artifact %q is outside its private output directory", artifact)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("score initializer artifact %q is not a regular file", artifact)
		}
		confined = append(confined, path)
	}
	return confined, nil
}

func loadSelectedManifest(cfg *config.Config) (*Manifest, bool, error) {
	root, err := cfg.EffectiveRangeRoot()
	if err != nil {
		return nil, false, err
	}
	path := filepath.Join(root.Path, manifestName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
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

func decodeManifest(raw []byte) (*Manifest, error) {
	return rangeconfig.Decode(raw)
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
	if _, err := rangecommand.RequireBuiltinProfile(cfg, "score", rangecommand.ProfileActiveDir); err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, fmt.Errorf("%s: %w", actionGenerateAnswerKey, err)
	}
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
