package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client wraps the Proxmox VE REST API.
type Client struct {
	baseURL    string
	user       string
	node       string
	pool       string
	httpClient *http.Client

	mu     sync.Mutex
	ticket string
	csrf   string
}

// NewClient creates a new Proxmox API client and authenticates.
func NewClient(ctx context.Context, apiURL, user, password, node, pool string) (*Client, error) {
	// Strip trailing /api2/json if present (we add it per-request).
	apiURL = strings.TrimSuffix(apiURL, "/api2/json")
	apiURL = strings.TrimSuffix(apiURL, "/")

	c := &Client{
		baseURL: apiURL,
		user:    user,
		node:    node,
		pool:    pool,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Proxmox typically uses self-signed certs
			},
		},
	}

	if err := c.authenticate(ctx, password); err != nil {
		return nil, fmt.Errorf("proxmox authentication failed: %w", err)
	}
	return c, nil
}

func (c *Client) authenticate(ctx context.Context, password string) (err error) {
	data := url.Values{
		"username": {c.user},
		"password": {password},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api2/json/access/ticket",
		strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Proxmox at %s: %w", c.baseURL, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Ticket string `json:"ticket"`
			CSRF   string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode auth response: %w", err)
	}

	c.mu.Lock()
	c.ticket = result.Data.Ticket
	c.csrf = result.Data.CSRF
	c.mu.Unlock()

	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	c.mu.Lock()
	ticket := c.ticket
	csrf := c.csrf
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api2/json"+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cookie", "PVEAuthCookie="+ticket)
	if method != http.MethodGet {
		req.Header.Set("CSRFPreventionToken", csrf)
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	return c.httpClient.Do(req)
}

func (c *Client) get(ctx context.Context, path string) (_ json.RawMessage, err error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode response for %s: %w", path, err)
	}
	return envelope.Data, nil
}

func (c *Client) post(ctx context.Context, path string, data url.Values) (err error) {
	var body io.Reader
	if data != nil {
		body = strings.NewReader(data.Encode())
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, string(b))
	}
	return nil
}

// VM represents a Proxmox virtual machine or container.
type VM struct {
	VMID    int    `json:"vmid"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Type    string `json:"type"` // "qemu" or "lxc"
	Node    string `json:"node"`
	MaxMem  int64  `json:"maxmem"`
	MaxDisk int64  `json:"maxdisk"`
	CPU     int    `json:"cpus"`
}

// PoolMembers returns VMs/containers in the configured pool.
func (c *Client) PoolMembers(ctx context.Context) ([]VM, error) {
	data, err := c.get(ctx, "/pools/"+url.PathEscape(c.pool))
	if err != nil {
		return nil, fmt.Errorf("get pool %s: %w", c.pool, err)
	}

	var pool struct {
		Members []VM `json:"members"`
	}
	if err := json.Unmarshal(data, &pool); err != nil {
		return nil, fmt.Errorf("unmarshal pool: %w", err)
	}

	// Populate the node from the pool response if not set on members.
	for i := range pool.Members {
		if pool.Members[i].Node == "" {
			pool.Members[i].Node = c.node
		}
	}

	return pool.Members, nil
}

// VMStatus returns the current status of a VM/container.
func (c *Client) VMStatus(ctx context.Context, vmType string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/current", url.PathEscape(c.node), vmType, vmid)
	data, err := c.get(ctx, path)
	if err != nil {
		return "", err
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return "", err
	}
	return status.Status, nil
}

// VMConfig returns the config of a VM/container (to get the name).
func (c *Client) VMConfig(ctx context.Context, vmType string, vmid int) (map[string]interface{}, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/config", url.PathEscape(c.node), vmType, vmid)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// StartVM starts a VM or container.
func (c *Client) StartVM(ctx context.Context, vmType string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/start", url.PathEscape(c.node), vmType, vmid)
	return c.post(ctx, path, nil)
}

// StopVM stops a VM or container.
func (c *Client) StopVM(ctx context.Context, vmType string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/stop", url.PathEscape(c.node), vmType, vmid)
	return c.post(ctx, path, nil)
}

// DestroyVM destroys a VM or container.
func (c *Client) DestroyVM(ctx context.Context, vmType string, vmid int) (err error) {
	resp, err := c.doRequest(ctx, http.MethodDelete,
		fmt.Sprintf("/nodes/%s/%s/%d", url.PathEscape(c.node), vmType, vmid), nil)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("destroy VM %d: HTTP %d: %s", vmid, resp.StatusCode, string(b))
	}
	return nil
}

// QEMUAgentExec runs a command via the QEMU guest agent.
func (c *Client) QEMUAgentExec(ctx context.Context, vmid int, command string, timeout time.Duration) (stdout, stderr string, err error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", url.PathEscape(c.node), vmid)
	data := url.Values{
		"command":    {"powershell.exe"},
		"input-data": {command},
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("agent exec on VM %d: %w", vmid, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("agent exec HTTP %d: %s", resp.StatusCode, string(b))
	}

	var execResult struct {
		Data struct {
			PID int `json:"pid"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&execResult); err != nil {
		return "", "", fmt.Errorf("decode exec result: %w", err)
	}

	// Poll for exec status.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(2 * time.Second):
		}

		statusPath := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec-status?pid=%d",
			url.PathEscape(c.node), vmid, execResult.Data.PID)
		statusData, err := c.get(ctx, statusPath)
		if err != nil {
			continue
		}

		var status struct {
			Exited  int    `json:"exited"`
			OutData string `json:"out-data"`
			ErrData string `json:"err-data"`
		}
		if err := json.Unmarshal(statusData, &status); err != nil {
			continue
		}

		if status.Exited != 0 {
			return status.OutData, status.ErrData, nil
		}
	}

	return "", "", fmt.Errorf("command on VM %d timed out after %s", vmid, timeout)
}

// QEMUAgentGetInterfaces gets network interfaces from the QEMU guest agent.
func (c *Client) QEMUAgentGetInterfaces(ctx context.Context, vmid int) ([]AgentNetworkInterface, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", url.PathEscape(c.node), vmid)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result []AgentNetworkInterface `json:"result"`
	}
	// The response may be wrapped in a "result" key or directly an array.
	if err := json.Unmarshal(data, &result); err != nil {
		// Try direct array.
		var ifaces []AgentNetworkInterface
		if err2 := json.Unmarshal(data, &ifaces); err2 != nil {
			return nil, fmt.Errorf("decode interfaces: %w", err)
		}
		return ifaces, nil
	}
	return result.Result, nil
}

// AgentNetworkInterface represents a network interface from the guest agent.
type AgentNetworkInterface struct {
	Name        string `json:"name"`
	IPAddresses []struct {
		IPAddress string `json:"ip-address"`
		IPType    string `json:"ip-address-type"`
	} `json:"ip-addresses"`
}

// QEMUAgentGetHostname gets the hostname via the QEMU guest agent.
func (c *Client) QEMUAgentGetHostname(ctx context.Context, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-host-name", url.PathEscape(c.node), vmid)
	data, err := c.get(ctx, path)
	if err != nil {
		return "", err
	}

	var result struct {
		HostName string `json:"host-name"`
		Result   struct {
			HostName string `json:"host-name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	if result.HostName != "" {
		return result.HostName, nil
	}
	return result.Result.HostName, nil
}

// Node returns the configured Proxmox node name.
func (c *Client) Node() string { return c.node }

// User returns the authenticated Proxmox user.
func (c *Client) User() string { return c.user }
