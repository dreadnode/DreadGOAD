package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Session represents an active SSM session.
type Session struct {
	SessionID  string
	InstanceID string
	StartDate  time.Time
	Status     string
}

// CommandResult holds the output of an SSM command.
type CommandResult struct {
	Status string
	Stdout string
	Stderr string
}

// DescribeActiveSessions returns active SSM sessions for an instance.
func (c *Client) DescribeActiveSessions(ctx context.Context, instanceID string) ([]Session, error) {
	out, err := c.SSM.DescribeSessions(ctx, &ssm.DescribeSessionsInput{
		State: ssmtypes.SessionStateActive,
		Filters: []ssmtypes.SessionFilter{
			{Key: ssmtypes.SessionFilterKeyTargetId, Value: Ptr(instanceID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe sessions for %s: %w", instanceID, err)
	}

	var sessions []Session
	for _, s := range out.Sessions {
		sess := Session{
			SessionID:  deref(s.SessionId),
			InstanceID: deref(s.Target),
			Status:     string(s.Status),
		}
		if s.StartDate != nil {
			sess.StartDate = *s.StartDate
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// CleanupStaleSessions terminates SSM sessions older than maxAge for the given instances.
func (c *Client) CleanupStaleSessions(ctx context.Context, instanceIDs []string, maxAge time.Duration, dryRun bool, log *slog.Logger) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	terminated := 0

	for _, instanceID := range instanceIDs {
		sessions, err := c.DescribeActiveSessions(ctx, instanceID)
		if err != nil {
			log.Warn("failed to describe sessions", "instance", instanceID, "error", err)
			continue
		}

		for _, s := range sessions {
			if s.StartDate.Before(cutoff) {
				if dryRun {
					log.Info("would terminate session", "session", s.SessionID, "instance", instanceID, "started", s.StartDate)
				} else {
					if err := c.TerminateSession(ctx, s.SessionID); err != nil {
						log.Warn("failed to terminate session", "session", s.SessionID, "error", err)
					} else {
						log.Info("terminated session", "session", s.SessionID, "instance", instanceID)
						terminated++
					}
				}
			}
		}
	}
	return terminated, nil
}

// TerminateSession ends an SSM session.
func (c *Client) TerminateSession(ctx context.Context, sessionID string) error {
	_, err := c.SSM.TerminateSession(ctx, &ssm.TerminateSessionInput{
		SessionId: Ptr(sessionID),
	})
	return err
}

// RunPowerShellCommand executes a PowerShell command on an instance via SSM and returns the result.
func (c *Client) RunPowerShellCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*CommandResult, error) {
	timeoutSecs := int32(timeout.Seconds())
	if timeoutSecs == 0 {
		timeoutSecs = 60
	}

	out, err := c.SSM.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:    []string{instanceID},
		DocumentName:   Ptr("AWS-RunPowerShellScript"),
		Parameters:     map[string][]string{"commands": {command}},
		TimeoutSeconds: aws.Int32(timeoutSecs),
	})
	if err != nil {
		return nil, fmt.Errorf("send command to %s: %w", instanceID, err)
	}

	commandID := deref(out.Command.CommandId)
	return c.waitForCommand(ctx, commandID, instanceID, timeout)
}

// RunPowerShellOnMultiple executes a PowerShell command on multiple instances.
func (c *Client) RunPowerShellOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*CommandResult, error) {
	timeoutSecs := int32(timeout.Seconds())
	if timeoutSecs == 0 {
		timeoutSecs = 60
	}

	out, err := c.SSM.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:    instanceIDs,
		DocumentName:   Ptr("AWS-RunPowerShellScript"),
		Parameters:     map[string][]string{"commands": {command}},
		TimeoutSeconds: aws.Int32(timeoutSecs),
	})
	if err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	commandID := deref(out.Command.CommandId)
	results := make(map[string]*CommandResult, len(instanceIDs))

	for _, id := range instanceIDs {
		result, err := c.waitForCommand(ctx, commandID, id, timeout)
		if err != nil {
			results[id] = &CommandResult{Status: "Error", Stderr: err.Error()}
		} else {
			results[id] = result
		}
	}
	return results, nil
}

// EnableSSMUserLocal re-enables the local ssm-user account.
func (c *Client) EnableSSMUserLocal(ctx context.Context, instanceID string) error {
	cmd := `try { Enable-LocalUser -Name ssm-user -ErrorAction Stop; Write-Output "ssm-user enabled" } catch { Write-Output "Failed: $_"; exit 1 }`
	result, err := c.RunPowerShellCommand(ctx, instanceID, cmd, 60*time.Second)
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		return fmt.Errorf("enable ssm-user failed: %s", result.Stderr)
	}
	return nil
}

// FixSSMUserViaDomainAccount creates ssm-user as a domain account on DCs.
func (c *Client) FixSSMUserViaDomainAccount(ctx context.Context, instanceID string) error {
	script := `$ErrorActionPreference = "Continue"
$maxWait = 30
$attempt = 0

$cs = Get-WmiObject Win32_ComputerSystem
if ($cs.DomainRole -lt 4) {
    Write-Output "Not a DC (role=$($cs.DomainRole)), skipping domain ssm-user creation"
    exit 0
}

Write-Output "Waiting for AD Web Services..."
while ($attempt -lt $maxWait) {
    $adws = Get-Service ADWS -ErrorAction SilentlyContinue
    if ($adws.Status -eq "Running") {
        Write-Output "ADWS is running"
        break
    }
    if ($adws.Status -eq "Stopped") {
        Start-Service ADWS -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 10
    $attempt++
}

try {
    Get-ADDomain -ErrorAction Stop | Out-Null
    Write-Output "AD is accessible"
} catch {
    Write-Output "ERROR: AD not accessible: $_"
    exit 1
}

try {
    $user = Get-ADUser -Identity ssm-user -ErrorAction Stop
    Write-Output "ssm-user exists, ensuring enabled..."
    Enable-ADAccount -Identity ssm-user
    Set-ADUser -Identity ssm-user -PasswordNeverExpires $true
} catch {
    Write-Output "Creating ssm-user domain account..."
    $pwd = ConvertTo-SecureString "TempP@ss$(Get-Random)!" -AsPlainText -Force
    New-ADUser -Name ssm-user -AccountPassword $pwd -Enabled $true -PasswordNeverExpires $true
}

try {
    Add-ADGroupMember -Identity "Domain Admins" -Members ssm-user -ErrorAction SilentlyContinue
    Write-Output "ssm-user added to Domain Admins"
} catch {
    Write-Output "ssm-user already in Domain Admins or error: $_"
}

Restart-Service AmazonSSMAgent -Force
Write-Output "SSM Agent restarted - ssm-user fix complete"`

	result, err := c.RunPowerShellCommand(ctx, instanceID, script, 10*time.Minute)
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		return fmt.Errorf("fix ssm-user failed: %s %s", result.Stdout, result.Stderr)
	}
	return nil
}

func (c *Client) waitForCommand(ctx context.Context, commandID, instanceID string, timeout time.Duration) (*CommandResult, error) {
	deadline := time.Now().Add(timeout + 30*time.Second)
	backoff := 2 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff = backoff * 3 / 2
		}

		out, err := c.SSM.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
			CommandId:  Ptr(commandID),
			InstanceId: Ptr(instanceID),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvocationDoesNotExist") {
				continue
			}
			return nil, fmt.Errorf("get command invocation: %w", err)
		}

		status := string(out.Status)
		switch out.Status {
		case ssmtypes.CommandInvocationStatusSuccess,
			ssmtypes.CommandInvocationStatusFailed,
			ssmtypes.CommandInvocationStatusTimedOut,
			ssmtypes.CommandInvocationStatusCancelled:
			return &CommandResult{
				Status: status,
				Stdout: deref(out.StandardOutputContent),
				Stderr: deref(out.StandardErrorContent),
			}, nil
		}
	}
	return nil, fmt.Errorf("command %s timed out waiting for result", commandID)
}
