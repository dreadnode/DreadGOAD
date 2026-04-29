package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommandResult holds the output of a Run Command invocation.
type CommandResult struct {
	Status string
	Stdout string
	Stderr string
}

// runCommandResponse is the relevant slice of `az vm run-command invoke`'s
// response.
type runCommandResponse struct {
	Value []struct {
		Code          string `json:"code"`
		DisplayStatus string `json:"displayStatus"`
		Level         string `json:"level"`
		Message       string `json:"message"`
	} `json:"value"`
}

// RunPowerShellCommand executes a PowerShell script on a VM via Azure
// Run Command and returns the parsed result. Equivalent to AWS
// SSM SendCommand → AWS-RunPowerShellScript.
//
// timeout is honored both by the parent context and by the `az` invocation
// (which has its own internal long-poll timeout).
func (c *Client) RunPowerShellCommand(ctx context.Context, vmID, script string, timeout time.Duration) (*CommandResult, error) {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// `az vm run-command invoke` reads --scripts as a literal string. To avoid
	// argv-length and quoting issues with multi-line PowerShell, write the
	// script to a temp file and pass via @file syntax.
	tmp, err := os.CreateTemp("", "dreadgoad-azrc-*.ps1")
	if err != nil {
		return nil, fmt.Errorf("create temp script: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write temp script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp script: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx,
		"az", "vm", "run-command", "invoke",
		"--ids", vmID,
		"--command-id", "RunPowerShellScript",
		"--scripts", "@"+tmp.Name(),
		"-o", "json",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run-command invoke: %w (stderr: %s)",
			err, strings.TrimSpace(stderr.String()))
	}

	var resp runCommandResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("decode run-command response: %w", err)
	}

	result := &CommandResult{Status: "Success"}
	for _, v := range resp.Value {
		switch v.Code {
		case "ComponentStatus/StdOut/succeeded":
			result.Stdout = v.Message
		case "ComponentStatus/StdErr/succeeded":
			result.Stderr = v.Message
			if strings.TrimSpace(v.Message) != "" {
				result.Status = "Failed"
			}
		}
		if v.Level == "Error" {
			result.Status = "Failed"
		}
	}
	return result, nil
}

// RunPowerShellOnMultiple runs the same script across multiple VMs.
// Azure has no native batch endpoint; we issue invocations sequentially
// because Run Command serializes per-VM anyway.
func (c *Client) RunPowerShellOnMultiple(ctx context.Context, vmIDs []string, script string, timeout time.Duration) (map[string]*CommandResult, error) {
	results := make(map[string]*CommandResult, len(vmIDs))
	for _, id := range vmIDs {
		res, err := c.RunPowerShellCommand(ctx, id, script, timeout)
		if err != nil {
			results[id] = &CommandResult{Status: "Error", Stderr: err.Error()}
		} else {
			results[id] = res
		}
	}
	return results, nil
}
