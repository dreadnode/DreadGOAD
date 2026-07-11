package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// jsonBegin/jsonEnd bracket the JSON payload emitted by every embedded
// PowerShell script. Markers tolerate WinRM banners, Write-Warning text,
// and progress streams arriving alongside the payload — text scraping
// without an envelope is fragile in the face of locale and PS-version
// differences.
const (
	jsonBegin = "===BEGIN_JSON==="
	jsonEnd   = "===END_JSON==="

	// sqlProbeSentinel is appended to every MSSQL probe script and printed
	// only after the query completes. Its presence distinguishes a genuine
	// empty result set (sentinel present, zero rows) from a transient blip
	// (sentinel absent: a host still settling right after provisioning
	// returns truncated or empty stdout even though the transport reported
	// success). See [Validator.mssqlProbe].
	sqlProbeSentinel = "__SQLPROBE_OK__"

	// transientRetries is how many times a probe whose output is empty or
	// missing its expected JSON envelope / SQL sentinel is re-run before
	// giving up. Envelope and sentinel scripts never legitimately emit empty
	// output, so retrying here cannot mask a real "thing not found" result —
	// it only absorbs the post-provision settling window that would otherwise
	// turn a healthy lab into bogus failures.
	transientRetries = 3
)

var backoffBase = time.Second

// backoffSleep waits attempt*backoffBase or returns ctx.Err() if the context
// is cancelled first.
func backoffSleep(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(attempt) * backoffBase):
		return nil
	}
}

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
	// A non-empty response that lacks the JSON envelope happens transiently for
	// these scripts (a host still settling emits a banner or partial stream
	// before the real payload), never as a legitimate result. Retry that case.
	// The empty-output case is already handled one layer down by runPSErr, and
	// transport errors there feed the dead-host machinery, so neither is
	// retried again here.
	var lastErr error
	for attempt := 1; attempt <= transientRetries; attempt++ {
		raw, err := v.runPSErr(ctx, host, script)
		if err != nil {
			return zero, err
		}
		if raw == "" {
			return zero, errors.New("empty output despite successful transport (host settling?)")
		}
		payload, perr := extractJSON(raw)
		if perr != nil {
			lastErr = perr
			if attempt < transientRetries {
				if berr := backoffSleep(ctx, attempt); berr != nil {
					return zero, berr
				}
			}
			continue
		}
		var out T
		if uerr := json.Unmarshal(payload, &out); uerr != nil {
			// A malformed envelope is a script bug, not a transient blip —
			// fail fast rather than retrying pointlessly.
			return zero, fmt.Errorf("unmarshal payload: %w", uerr)
		}
		return out, nil
	}
	return zero, lastErr
}
