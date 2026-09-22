// Package rangecommand resolves and runs semantic commands owned by a range.
package rangecommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
)

const (
	HandlerBuiltin    = "builtin"
	HandlerExecutable = "executable"
	ProfileActiveDir  = "active-directory"
)

var Names = []string{"health", "validate", "score", "reset", "scrub"}

var protocols = map[string]string{
	"health": "health/v1", "validate": "validate/v1", "score": "score/v1",
	"reset": "operation/v1", "scrub": "operation/v1",
}

var defaultDescriptions = map[string]string{
	"health":   "Check each host and the range's core services",
	"validate": "Check the range against its complete expected state",
	"score":    "Score an engagement report using this range's objectives",
	"reset":    "Restore the range to its authored baseline",
	"scrub":    "Remove engagement artifacts between runs",
}

var defaultDetails = map[string]string{
	"health":   "read-only; runs the selected range's health implementation",
	"validate": "read-only; runs the selected range's expected-state implementation",
	"score":    "uses objectives and scoring logic owned by the selected range",
	"reset":    "mutates live state according to the selected range's reset contract",
	"scrub":    "deletes engagement artifacts according to the selected range's cleanup contract",
}

// Capability is the fully resolved behavior of one semantic command.
type Capability struct {
	Name            string `json:"name"`
	Supported       bool   `json:"supported"`
	Description     string `json:"description,omitempty"`
	Detail          string `json:"detail,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	HandlerType     string `json:"handler_type,omitempty"`
	Profile         string `json:"profile,omitempty"`
	Path            string `json:"-"`
	RangeRoot       string `json:"-"`
	Initializes     bool   `json:"initializes,omitempty"`
	InitializerPath string `json:"-"`
}

// Request is written to a range-owned executable on stdin. It contains range
// selectors and parsed command inputs, never cloud credentials or config-file
// contents. Trusted handlers may read their own range files from LabPath.
type Request struct {
	Schema      string         `json:"schema"`
	Command     string         `json:"command"`
	Protocol    string         `json:"protocol"`
	Environment string         `json:"environment"`
	ConfigPath  string         `json:"config_path,omitempty"`
	ProjectRoot string         `json:"project_root"`
	LabPath     string         `json:"lab_path"`
	Options     map[string]any `json:"options,omitempty"`
	Arguments   []string       `json:"arguments,omitempty"`
}

// UnsupportedError distinguishes an intentionally unavailable range command
// from a malformed manifest or handler installation.
type UnsupportedError struct {
	Range   string
	Command string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("range %s does not support %s", e.Range, e.Command)
}

// Resolve returns the selected range's command implementation. Active
// Directory ranges retain their historical built-ins when commands are absent;
// other range kinds fail closed until they declare each capability.
func Resolve(cfg *config.Config, name string) (Capability, error) {
	root, err := cfg.EffectiveRangeRoot()
	if err != nil {
		return Capability{}, err
	}
	return resolveAtRoot(cfg, root, name)
}

func resolveAtRoot(cfg *config.Config, root config.RangeRoot, name string) (Capability, error) {
	protocol, known := protocols[name]
	if !known {
		return Capability{}, fmt.Errorf("unknown range command %q", name)
	}
	manifest, found, err := rangeconfig.Load(root.Path)
	if err != nil {
		return Capability{}, err
	}
	if !found {
		return builtinCapability(name, protocol, root.Path), nil
	}

	if spec, declared := manifest.Commands[name]; declared {
		if spec.Enabled != nil && !*spec.Enabled {
			return Capability{Name: name, Description: description(name, spec.Description)}, nil
		}
		capability := Capability{
			Name: name, Supported: true, Description: description(name, spec.Description),
			Detail: detail(name, spec.Detail), Protocol: spec.Protocol, HandlerType: spec.Handler.Type, Profile: spec.Handler.Profile,
			RangeRoot: root.Path,
		}
		if spec.Protocol != protocol {
			return Capability{}, fmt.Errorf("range %s command %s uses protocol %q; expected %q", cfg.ResolvedLab(), name, spec.Protocol, protocol)
		}
		switch spec.Handler.Type {
		case HandlerBuiltin:
			if spec.Handler.Profile != ProfileActiveDir {
				return Capability{}, fmt.Errorf("range %s command %s uses unsupported builtin profile %q", cfg.ResolvedLab(), name, spec.Handler.Profile)
			}
		case HandlerExecutable:
			path, err := confinedExecutable(root.Path, spec.Handler.Path)
			if err != nil {
				return Capability{}, fmt.Errorf("range %s command %s: %w", cfg.ResolvedLab(), name, err)
			}
			capability.Path = path
		default:
			return Capability{}, fmt.Errorf("range %s command %s has unsupported handler type %q", cfg.ResolvedLab(), name, spec.Handler.Type)
		}
		if spec.Initializer != nil {
			path, err := confinedExecutable(root.Path, spec.Initializer.Path)
			if err != nil {
				return Capability{}, fmt.Errorf("range %s command %s initializer: %w", cfg.ResolvedLab(), name, err)
			}
			capability.Initializes = true
			capability.InitializerPath = path
		}
		return capability, nil
	}

	// inspection.profile was the original health/validate extension point.
	if (name == "health" || name == "validate") && manifest.Inspection.Profile != "" {
		if manifest.Inspection.Profile != ProfileActiveDir {
			return Capability{}, fmt.Errorf("range %s command %s uses unsupported legacy inspection profile %q", cfg.ResolvedLab(), name, manifest.Inspection.Profile)
		}
		return builtinCapability(name, protocol, root.Path), nil
	}
	if manifest.Kind == rangeconfig.KindActiveDirectory {
		return builtinCapability(name, protocol, root.Path), nil
	}
	return Capability{Name: name, Description: defaultDescriptions[name], Detail: defaultDetails[name], RangeRoot: root.Path}, nil
}

// ExecuteScoreInitializer prepares private per-session scoring data for a
// range-owned score handler. The handler owns artifact names and reports them
// through the session-init/v1 result.
func ExecuteScoreInitializer(ctx context.Context, cfg *config.Config, capability Capability, outputDir string, stdout, stderr io.Writer) error {
	if capability.Name != "score" || !capability.Initializes || capability.InitializerPath == "" {
		return errors.New("score capability has no initializer")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create score artifact directory: %w", err)
	}
	if err := os.Chmod(outputDir, 0o700); err != nil {
		return fmt.Errorf("secure score artifact directory: %w", err)
	}
	initializer := Capability{
		Name: "score:init", Supported: true, Protocol: "session-init/v1",
		HandlerType: HandlerExecutable, Path: capability.InitializerPath,
		RangeRoot: capability.RangeRoot,
	}
	return Execute(ctx, cfg, initializer, Request{
		Options: map[string]any{"output_dir": outputDir},
	}, stdout, stderr)
}

func builtinCapability(name, protocol, rangeRoot string) Capability {
	return Capability{Name: name, Supported: true, Description: defaultDescriptions[name], Detail: defaultDetails[name], Protocol: protocol, HandlerType: HandlerBuiltin, Profile: ProfileActiveDir, RangeRoot: rangeRoot}
}

func description(name, override string) string {
	if override != "" {
		return override
	}
	return defaultDescriptions[name]
}

func detail(name, override string) string {
	if override != "" {
		return override
	}
	return defaultDetails[name]
}

// Capabilities resolves every stable semantic command against one range root
// and returns that root so callers can load adjacent range-owned context from
// the same snapshot.
func Capabilities(cfg *config.Config) ([]Capability, config.RangeRoot, error) {
	root, err := cfg.EffectiveRangeRoot()
	if err != nil {
		return nil, config.RangeRoot{}, err
	}
	names := append([]string(nil), Names...)
	sort.Strings(names)
	result := make([]Capability, 0, len(names))
	for _, name := range names {
		capability, err := resolveAtRoot(cfg, root, name)
		if err != nil {
			return nil, config.RangeRoot{}, err
		}
		result = append(result, capability)
	}
	return result, root, nil
}

// Require returns a supported capability or a typed unavailable error.
func Require(cfg *config.Config, name string) (Capability, error) {
	capability, err := Resolve(cfg, name)
	if err != nil {
		return Capability{}, err
	}
	if !capability.Supported {
		return Capability{}, &UnsupportedError{Range: cfg.ResolvedLab(), Command: name}
	}
	return capability, nil
}

// RequireBuiltinProfile returns a supported command only when the CLI owns its
// implementation through the requested built-in profile. Legacy helpers must
// use this boundary instead of silently running for executable-owned ranges.
func RequireBuiltinProfile(cfg *config.Config, name, profile string) (Capability, error) {
	capability, err := Require(cfg, name)
	if err != nil {
		return Capability{}, err
	}
	if capability.HandlerType != HandlerBuiltin || capability.Profile != profile {
		return Capability{}, fmt.Errorf(
			"range %s command %s is owned by a %s handler; built-in profile %s is required",
			cfg.ResolvedLab(), name, capability.HandlerType, profile,
		)
	}
	return capability, nil
}

func confinedExecutable(labPath, declared string) (string, error) {
	root, err := filepath.EvalSymlinks(labPath)
	if err != nil {
		return "", fmt.Errorf("resolve range directory: %w", err)
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, declared))
	if err != nil {
		return "", fmt.Errorf("resolve executable %q: %w", declared, err)
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("executable %q escapes the range directory", declared)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect executable %q: %w", declared, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("executable %q is not a regular file", declared)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("executable %q is not executable", declared)
	}
	return candidate, nil
}

// Execute runs a resolved external handler with a versioned request on stdin.
// Stdout and stderr remain separate so existing machine-readable CLI contracts
// and console streaming continue to work.
func Execute(ctx context.Context, cfg *config.Config, capability Capability, request Request, stdout, stderr io.Writer) error {
	if capability.HandlerType != HandlerExecutable || capability.Path == "" || capability.RangeRoot == "" {
		return errors.New("range command is not an executable handler")
	}
	request.Schema = "dreadgoad/range-command-request/v1"
	request.Command = capability.Name
	request.Protocol = capability.Protocol
	request.Environment = cfg.Env
	request.ConfigPath = config.ConfigFileUsed()
	request.ProjectRoot = cfg.ProjectRoot
	request.LabPath = capability.RangeRoot
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode range command request: %w", err)
	}
	command := exec.CommandContext(ctx, capability.Path)
	command.Dir = capability.RangeRoot
	command.Env = handlerEnvironment(os.Environ())
	command.Stdin = strings.NewReader(string(payload))
	captured := &tailWriter{destination: stdout, limit: 1 << 20}
	command.Stdout = captured
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("range %s command %s failed: %w", cfg.ResolvedLab(), capability.Name, err)
	}
	if err := validateResult(capability.Protocol, captured.Bytes()); err != nil {
		return fmt.Errorf("range %s command %s returned an invalid %s result: %w", cfg.ResolvedLab(), capability.Name, capability.Protocol, err)
	}
	return nil
}

var handlerEnvironmentNames = map[string]struct{}{
	"HOME": {}, "PATH": {}, "SHELL": {}, "USER": {}, "LOGNAME": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {}, "TERM": {}, "LANG": {},
	"SSH_AUTH_SOCK": {}, "SSL_CERT_FILE": {}, "SSL_CERT_DIR": {},
	"REQUESTS_CA_BUNDLE": {}, "HTTP_PROXY": {}, "HTTPS_PROXY": {},
	"NO_PROXY": {}, "http_proxy": {}, "https_proxy": {}, "no_proxy": {},
	"GOOGLE_APPLICATION_CREDENTIALS": {},
	"DREADGOAD_ENV":                  {}, "DREADGOAD_REGION": {}, "DREADGOAD_DEBUG": {},
}

var handlerEnvironmentPrefixes = []string{
	"LC_", "AWS_", "AZURE_", "ARM_", "CLOUDSDK_", "OCI_", "TF_",
	"ANSIBLE_",
}

func handlerEnvironment(parent []string) []string {
	filtered := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		_, allowed := handlerEnvironmentNames[name]
		if !allowed {
			for _, prefix := range handlerEnvironmentPrefixes {
				if strings.HasPrefix(name, prefix) {
					allowed = true
					break
				}
			}
		}
		if allowed {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

type tailWriter struct {
	destination io.Writer
	buffer      []byte
	limit       int
}

func (w *tailWriter) Write(payload []byte) (int, error) {
	written, err := w.destination.Write(payload)
	w.buffer = append(w.buffer, payload...)
	if len(w.buffer) > w.limit {
		w.buffer = append([]byte(nil), w.buffer[len(w.buffer)-w.limit:]...)
	}
	return written, err
}

func (w *tailWriter) Bytes() []byte { return w.buffer }

func validateResult(protocol string, output []byte) error {
	lines := bytes.Split(output, []byte{'\n'})
	var final []byte
	for index := len(lines) - 1; index >= 0; index-- {
		line := bytes.TrimSpace(lines[index])
		if len(line) != 0 {
			final = line
			break
		}
	}
	if len(final) == 0 {
		return errors.New("stdout has no final JSON result object")
	}
	var result map[string]any
	if json.Unmarshal(final, &result) != nil {
		return errors.New("final stdout line is not a JSON result object")
	}
	if result["schema"] != protocol {
		return fmt.Errorf("schema is %q", result["schema"])
	}
	switch protocol {
	case "health/v1", "validate/v1":
		if err := requireObjectArray(result, "checks"); err != nil {
			return err
		}
	case "score/v1":
		if err := requireObjectArray(result, "objectives"); err != nil {
			return err
		}
		if _, ok := result["score"].(float64); !ok {
			return errors.New("score must be a number")
		}
		if _, ok := result["maximum"].(float64); !ok {
			return errors.New("maximum must be a number")
		}
	case "operation/v1":
		if _, ok := result["changed"].(bool); !ok {
			return errors.New("changed must be a boolean")
		}
		if _, ok := result["steps"].([]any); !ok {
			return errors.New("steps must be an array")
		}
	case "session-init/v1":
		artifacts, ok := result["artifacts"].([]any)
		if !ok {
			return errors.New("artifacts must be an array")
		}
		for _, artifact := range artifacts {
			if _, ok := artifact.(string); !ok {
				return errors.New("artifacts entries must be strings")
			}
		}
	default:
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
	return nil
}

func requireObjectArray(result map[string]any, field string) error {
	values, ok := result[field].([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	for _, value := range values {
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s entries must be objects", field)
		}
	}
	return nil
}
