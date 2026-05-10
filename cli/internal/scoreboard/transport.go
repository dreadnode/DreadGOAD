package scoreboard

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/dreadnode/dreadgoad/internal/aws"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Transport fetches the agent's report file from wherever it's written.
type Transport interface {
	FetchReport(ctx context.Context) (string, error)
	DeleteReport(ctx context.Context) (bool, error)
}

// ErrNoReport is returned when the report file doesn't exist yet.
var ErrNoReport = errors.New("report file not found")

// LocalTransport reads/deletes a report from a local filesystem path.
type LocalTransport struct {
	Path string
}

// FetchReport reads the local report file. Returns ErrNoReport if missing.
func (t *LocalTransport) FetchReport(_ context.Context) (string, error) {
	data, err := os.ReadFile(t.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoReport
		}
		return "", err
	}
	return string(data), nil
}

// DeleteReport removes the local report file. Returns false if it didn't exist.
func (t *LocalTransport) DeleteReport(_ context.Context) (bool, error) {
	if err := os.Remove(t.Path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SSMTransport reads/deletes the report from an EC2 instance via SSM RunCommand.
type SSMTransport struct {
	InstanceID string
	ReportPath string
	Region     string
	Client     *awsclient.Client
}

// NewSSMTransport builds an SSM transport. Region defaults to the SDK's
// default if empty.
func NewSSMTransport(ctx context.Context, instanceID, reportPath, region string) (*SSMTransport, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instance ID is required")
	}
	c, err := awsclient.NewClient(ctx, region)
	if err != nil {
		return nil, err
	}
	return &SSMTransport{
		InstanceID: instanceID,
		ReportPath: reportPath,
		Region:     region,
		Client:     c,
	}, nil
}

// FetchReport runs `gzip -c <report> | base64 -w0` on the remote instance and
// inflates the result locally. SSM's GetCommandInvocation truncates plain stdout
// at 24KB; gzip+base64 sidesteps that for reports up to ~hundreds of KB before
// re-encoded base64 hits the same wall. Returns ErrNoReport if the file
// doesn't exist.
func (t *SSMTransport) FetchReport(ctx context.Context) (string, error) {
	cmd := fmt.Sprintf("test -s %[1]s && gzip -c %[1]s | base64 -w0", shellQuote(t.ReportPath))
	out, status, stderr, err := runSSMShell(ctx, t.Client, t.InstanceID, cmd)
	if err != nil {
		return "", err
	}
	if status == ssmtypes.CommandInvocationStatusSuccess {
		out = strings.TrimSpace(out)
		if out == "" {
			return "", ErrNoReport
		}
		return decodeGzipBase64Report(out)
	}
	if strings.Contains(stderr, "No such file") || status == ssmtypes.CommandInvocationStatusFailed {
		return "", ErrNoReport
	}
	return "", fmt.Errorf("ssm fetch %s: %s: %s", t.ReportPath, status, stderr)
}

func decodeGzipBase64Report(s string) (string, error) {
	gz, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("decode report base64: %w", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return "", fmt.Errorf("gunzip report: %w", err)
	}
	body, readErr := io.ReadAll(gr)
	closeErr := gr.Close()
	if readErr != nil {
		return "", fmt.Errorf("read report: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close gzip reader: %w", closeErr)
	}
	return string(body), nil
}

// DeleteReport removes the report file on the remote instance.
func (t *SSMTransport) DeleteReport(ctx context.Context) (bool, error) {
	_, status, stderr, err := runSSMShell(ctx, t.Client, t.InstanceID, fmt.Sprintf("rm -f %s", shellQuote(t.ReportPath)))
	if err != nil {
		return false, err
	}
	if status != ssmtypes.CommandInvocationStatusSuccess {
		return false, fmt.Errorf("ssm rm %s: %s: %s", t.ReportPath, status, stderr)
	}
	return true, nil
}

func runSSMShell(ctx context.Context, client *awsclient.Client, instanceID, cmd string) (string, ssmtypes.CommandInvocationStatus, string, error) {
	send, err := client.SSM.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:    []string{instanceID},
		DocumentName:   aws.String("AWS-RunShellScript"),
		Parameters:     map[string][]string{"commands": {cmd}},
		TimeoutSeconds: aws.Int32(30),
	})
	if err != nil {
		return "", "", "", fmt.Errorf("ssm send-command: %w", err)
	}
	commandID := aws.ToString(send.Command.CommandId)

	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			return "", "", "", fmt.Errorf("ssm command poll timed out")
		}
		time.Sleep(500 * time.Millisecond)
		inv, err := client.SSM.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(instanceID),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvocationDoesNotExist") {
				continue
			}
			return "", "", "", fmt.Errorf("ssm get-command-invocation: %w", err)
		}
		switch inv.Status {
		case ssmtypes.CommandInvocationStatusSuccess,
			ssmtypes.CommandInvocationStatusFailed,
			ssmtypes.CommandInvocationStatusCancelled,
			ssmtypes.CommandInvocationStatusTimedOut:
			return aws.ToString(inv.StandardOutputContent), inv.Status, aws.ToString(inv.StandardErrorContent), nil
		}
	}
}

// shellQuote single-quotes a string for safe inclusion in a /bin/sh command,
// escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
