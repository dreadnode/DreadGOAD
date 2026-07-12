package scoreboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/dreadnode/dreadgoad/internal/aws"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSMShellRunner executes shell commands on a Linux EC2 instance via AWS SSM
// (AWS-RunShellScript). Used for running nxc/secretsdump on the Kali attack
// box.
type SSMShellRunner struct {
	Client     *awsclient.Client
	InstanceID string
}

// NewSSMShellRunner constructs an SSMShellRunner.
func NewSSMShellRunner(ctx context.Context, instanceID, region, profile string) (*SSMShellRunner, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instance ID is required for SSM shell runner")
	}
	c, err := awsclient.NewClient(ctx, region, profile)
	if err != nil {
		return nil, err
	}
	return &SSMShellRunner{
		Client:     c,
		InstanceID: instanceID,
	}, nil
}

// RunShell executes a shell command on the instance via SSM and returns stdout.
func (r *SSMShellRunner) RunShell(ctx context.Context, command string, timeout time.Duration) (string, error) {
	send, err := r.Client.SSM.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:    []string{r.InstanceID},
		DocumentName:   aws.String("AWS-RunShellScript"),
		Parameters:     map[string][]string{"commands": {command}},
		TimeoutSeconds: aws.Int32(int32(timeout.Seconds())),
	})
	if err != nil {
		return "", fmt.Errorf("ssm send-command: %w", err)
	}
	commandID := aws.ToString(send.Command.CommandId)

	deadline := time.Now().Add(timeout + 5*time.Second)
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("ssm command poll timed out")
		}
		time.Sleep(500 * time.Millisecond)
		inv, err := r.Client.SSM.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(r.InstanceID),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvocationDoesNotExist") {
				continue
			}
			return "", fmt.Errorf("ssm get-command-invocation: %w", err)
		}
		switch inv.Status {
		case ssmtypes.CommandInvocationStatusSuccess:
			return aws.ToString(inv.StandardOutputContent), nil
		case ssmtypes.CommandInvocationStatusFailed:
			stderr := aws.ToString(inv.StandardErrorContent)
			stdout := aws.ToString(inv.StandardOutputContent)
			// nxc returns non-zero on auth failure but we still need
			// the stdout to check for [+] or (Pwn3d!).
			if stdout != "" {
				return stdout, nil
			}
			return "", fmt.Errorf("command failed: %s", stderr)
		case ssmtypes.CommandInvocationStatusCancelled,
			ssmtypes.CommandInvocationStatusTimedOut:
			return "", fmt.Errorf("command %s", inv.Status)
		}
	}
}
