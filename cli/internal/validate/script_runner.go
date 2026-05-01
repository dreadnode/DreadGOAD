package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// jsonBegin/jsonEnd bracket the JSON payload emitted by every embedded
// PowerShell script. Markers tolerate WinRM banners, Write-Warning text,
// and progress streams arriving alongside the payload — text scraping
// without an envelope is fragile in the face of locale and PS-version
// differences.
const (
	jsonBegin = "===BEGIN_JSON==="
	jsonEnd   = "===END_JSON==="
)

// scriptFuncs are the template helpers available to embedded PowerShell
// scripts via text/template.
var scriptFuncs = template.FuncMap{
	// psq renders a Go string as a single-quoted PowerShell literal,
	// escaping embedded single quotes by doubling them. Use {{psq .Var}}
	// in templates to interpolate untrusted values safely.
	"psq": func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	},
	// psarr renders a []string as a PowerShell array literal of psq'd
	// elements: ["a", "b'c"] -> @('a', 'b''c'). An empty slice renders
	// as @() so iteration is well-defined.
	"psarr": func(items []string) string {
		if len(items) == 0 {
			return "@()"
		}
		quoted := make([]string, len(items))
		for i, s := range items {
			quoted[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
		return "@(" + strings.Join(quoted, ", ") + ")"
	},
}

// renderScript expands {{.Var}} placeholders in a PowerShell template.
// The "psq" helper is the canonical way to interpolate string values.
func renderScript(tmpl string, vars map[string]any) (string, error) {
	t, err := template.New("ps").Funcs(scriptFuncs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse script template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute script template: %w", err)
	}
	return buf.String(), nil
}

// extractJSON pulls the JSON payload between BEGIN_JSON/END_JSON markers
// out of raw PowerShell output.
func extractJSON(raw string) ([]byte, error) {
	i := strings.Index(raw, jsonBegin)
	j := strings.LastIndex(raw, jsonEnd)
	if i < 0 || j < 0 || j <= i {
		return nil, errors.New("no JSON envelope in output")
	}
	payload := strings.TrimSpace(raw[i+len(jsonBegin) : j])
	if payload == "" {
		return nil, errors.New("empty JSON payload")
	}
	return []byte(payload), nil
}

// runScriptText renders a templated PowerShell command with vars and
// executes it on host. It returns the trimmed raw output. Errors surface
// both template-rendering bugs and transport failures (host marked dead,
// retries exhausted, ctx canceled) — caller's `if err != nil` branch
// should emit WARN instead of letting empty output mascarade as a real
// "thing not found" result. The trimmed stdout is returned even when
// err != nil so callers can include any partial output in WARN text.
func runScriptText(ctx context.Context, v *Validator, host, tmpl string, vars map[string]any) (string, error) {
	script, err := renderScript(tmpl, vars)
	if err != nil {
		return "", err
	}
	out, runErr := v.runPSErr(ctx, host, script)
	return strings.TrimSpace(out), runErr
}

// runScriptTextErr is the diagnostic variant of runScriptText: it bubbles
// runPS transport errors (host dead, retries exhausted, ctx cancelled) so
// catch-all branches in probes can surface a real cause instead of an
// opaque "could not read" message. The trimmed stdout is returned even
// when err != nil so callers can include any partial output in WARN text.
func runScriptTextErr(ctx context.Context, v *Validator, host, tmpl string, vars map[string]any) (string, error) {
	script, err := renderScript(tmpl, vars)
	if err != nil {
		return "", err
	}
	out, runErr := v.runPSErr(ctx, host, script)
	return strings.TrimSpace(out), runErr
}

// runScriptJSON renders a templated PowerShell script with vars, executes
// it on host via the validator's provider, and unmarshals the JSON
// envelope into a value of type T.
//
// Go does not allow generic methods on a struct, so this is a free
// function over *Validator.
func runScriptJSON[T any](ctx context.Context, v *Validator, host, tmpl string, vars map[string]any) (T, error) {
	var zero T
	script, err := renderScript(tmpl, vars)
	if err != nil {
		return zero, err
	}
	raw := v.runPS(ctx, host, script)
	if raw == "" {
		return zero, errors.New("empty output (host unreachable or marked dead)")
	}
	payload, err := extractJSON(raw)
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		return zero, fmt.Errorf("unmarshal payload: %w", err)
	}
	return out, nil
}
