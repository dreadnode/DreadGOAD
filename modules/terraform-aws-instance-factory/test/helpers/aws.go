package helpers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	smithy "github.com/aws/smithy-go"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/stretchr/testify/require"
)

const (
	// DefaultRegion is the default AWS region for running tests
	DefaultRegion = "us-east-2"
	// DefaultEnv is the environment name used for testing
	DefaultEnv = "test"
	// DefaultInstanceType is the default EC2 instance type to use in tests
	DefaultInstanceType = "t3.micro"
)

var (
	// SupportedAZs defines the supported availability zones
	SupportedAZs = []string{
		fmt.Sprintf("%sa", DefaultRegion),
		fmt.Sprintf("%sb", DefaultRegion),
		fmt.Sprintf("%sc", DefaultRegion),
		fmt.Sprintf("%sd", DefaultRegion),
		fmt.Sprintf("%sf", DefaultRegion),
	}
)

// CreateIAMRole creates a new IAM role for testing
func CreateIAMRole(t *testing.T) string {
	ctx := context.Background()

	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(DefaultRegion))
	require.NoError(t, err)
	iamSvc := iam.NewFromConfig(cfg)

	roleName := fmt.Sprintf("test-role-%s", random.UniqueId())

	_, err = iamSvc.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName: aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(`{
			"Version":"2012-10-17",
			"Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]
		}`),
	})
	if err != nil {
		// If it already exists, reuse it
		if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "EntityAlreadyExists" {
			t.Logf("Role %s already exists, using existing role", roleName)
			return roleName
		}
		// Some providers return plain text errors in local/test envs
		if strings.Contains(strings.ToLower(err.Error()), "entityalreadyexists") {
			t.Logf("Role %s already exists, using existing role", roleName)
			return roleName
		}
		require.NoError(t, err)
	}

	return roleName
}

// CreateInstanceProfile creates an EC2 instance profile
func CreateInstanceProfile(t *testing.T, roleName string) string {
	ctx := context.Background()

	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(DefaultRegion))
	require.NoError(t, err)
	iamSvc := iam.NewFromConfig(cfg)

	profileName := fmt.Sprintf("test-profile-%s", random.UniqueId())

	_, err = iamSvc.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	require.NoError(t, err)

	// Wait for instance profile to be ready
	time.Sleep(5 * time.Second)

	_, err = iamSvc.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
		RoleName:            aws.String(roleName),
	})
	require.NoError(t, err)

	// Wait for role association to propagate
	time.Sleep(10 * time.Second)

	return profileName
}

// ExecuteCommand executes a shell command and returns the output
func ExecuteCommand(command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
