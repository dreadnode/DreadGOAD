// Package azure implements the dreadgoad provider interface for Microsoft Azure.
//
// All control-plane operations shell out to the `az` CLI rather than the Azure
// SDK for Go. This piggybacks on the user's `az login` session for authentication
// and avoids pulling the SDK into the build. Per-call overhead is tens of
// milliseconds, which is negligible for a 5-VM lab.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps `az` CLI invocations against a specific subscription/region.
type Client struct {
	Region         string
	SubscriptionID string // populated by VerifyCredentials
}

// NewClient constructs a Client. Region is required; subscription is read from
// the active `az` session at credential-verify time.
func NewClient(region string) *Client {
	return &Client{Region: region}
}

// run invokes `az` and returns stdout. Stderr is bundled into the returned
// error so callers can see CLI diagnostics without enabling verbose logging.
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az %s: %w (stderr: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// runJSON invokes `az` and unmarshals stdout into out.
func (c *Client) runJSON(ctx context.Context, out any, args ...string) error {
	data, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode az output: %w", err)
	}
	return nil
}

// AccountInfo is the relevant slice of `az account show`.
type AccountInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
	User     struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"user"`
}

// VerifyCredentials runs `az account show` and caches the subscription ID.
func (c *Client) VerifyCredentials(ctx context.Context) (*AccountInfo, error) {
	var info AccountInfo
	if err := c.runJSON(ctx, &info, "account", "show", "-o", "json"); err != nil {
		return nil, fmt.Errorf("azure credentials invalid or not configured (run `az login`): %w", err)
	}
	c.SubscriptionID = info.ID
	return &info, nil
}
