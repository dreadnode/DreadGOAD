package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	asg "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/cleanup"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/helpers"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/require"
)

// TestTerraformAwsInstanceFactoryModule tests creating instances with different OS types
func TestTerraformAwsInstanceFactoryModule(t *testing.T) {
	ctx := context.Background()

	// Get the test name prefix to use for all cleanup operations
	testPrefix := "test-tt"

	// Register cleanup to run at the end of the test
	defer cleanup.ForceCleanup(t, testPrefix)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)

	ec2Client := ec2.NewFromConfig(cfg)

	// Clean up only test-prefixed resources from previous failed runs
	cleanup.SecurityGroupsByNamePrefix(t, ec2Client, testPrefix)

	t.Parallel()

	osTypes := []string{"linux", "windows"}
	for _, osType := range osTypes {
		osType := osType
		t.Run(fmt.Sprintf("OS_%s", osType), func(t *testing.T) {
			t.Parallel()
			// Create a unique workload name that includes the test name
			instanceName := fmt.Sprintf("%s-%s-%s-%s", testPrefix, t.Name(), osType, random.UniqueId())
			// Register specific cleanup for this test run
			defer cleanup.CleanupInstancesByNamePrefix(t, ec2Client, instanceName)
			defer cleanup.SecurityGroupsByNamePrefix(t, ec2Client, instanceName)
			runTerraformTest(t, false, osType, instanceName)
		})
	}
}

// TestTerraformASGConfigurations tests different ASG configurations
func TestTerraformASGConfigurations(t *testing.T) {
	ctx := context.Background()

	// Create shorter but still unique prefix
	testPrefix := fmt.Sprintf("tt-%s", random.UniqueId())
	defer cleanup.ForceCleanup(t, testPrefix)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)

	ec2Client := ec2.NewFromConfig(cfg)
	cleanup.SecurityGroupsByNamePrefix(t, ec2Client, testPrefix)

	t.Parallel()

	configs := []struct {
		name        string
		minSize     int
		maxSize     int
		desiredSize int
	}{
		{"SmallASG", 1, 3, 2},
		{"LargeASG", 3, 10, 5},
	}

	for _, configCase := range configs {
		configCase := configCase
		t.Run(configCase.name, func(t *testing.T) {
			t.Parallel()
			// Create shorter workload name by using minimal identifiers
			instanceName := fmt.Sprintf("%s-%s-%s",
				testPrefix,
				configCase.name,
				random.UniqueId()[:6],
			)

			// Register cleanup specific to this test instance
			defer func() {
				cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
				if err == nil {
					ec2Client := ec2.NewFromConfig(cfg)
					cleanup.CleanupInstancesByNamePrefix(t, ec2Client, instanceName)
					cleanup.SecurityGroupsByNamePrefix(t, ec2Client, instanceName)
				}
			}()

			runTerraformTestWithASGConfig(t, configCase.minSize, configCase.maxSize, configCase.desiredSize, instanceName)
		})
	}
}

// TestTerraformStorageConfigurations tests different storage configurations
func TestTerraformStorageConfigurations(t *testing.T) {
	ctx := context.Background()

	testPrefix := "test-tt"
	defer cleanup.ForceCleanup(t, testPrefix)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)

	ec2Client := ec2.NewFromConfig(cfg)
	cleanup.SecurityGroupsByNamePrefix(t, ec2Client, testPrefix)

	t.Parallel()
	instanceName := fmt.Sprintf("tt-%s-%s", t.Name(), random.UniqueId())
	runTerraformTestWithStorageConfig(t, 100, "gp3", false, instanceName)
}

func runTerraformTest(t *testing.T, enableASG bool, osType string, instanceName string) {
	ctx := context.Background()

	// Create cleanup tracker
	cleanupTracker := &types.CleanupTracker{}

	// Copy module to temp directory
	tempDir, err := helpers.CopyModuleToTempDir(t, ".")
	require.NoError(t, err)

	if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
		defer func() {
			t.Log("Cleaning up temporary directory...")
			if err := os.RemoveAll(tempDir); err != nil {
				t.Logf("Warning: Error cleaning up temporary directory: %v", err)
			}
		}()
	}

	defaultVPCID := helpers.GetDefaultVPCID(t)
	roleName := helpers.CreateIAMRole(t)
	instanceProfile := helpers.CreateInstanceProfile(t, roleName)
	securityGroupID := helpers.CreateSecurityGroup(t, defaultVPCID, instanceName)
	subnetIDs := helpers.GetPublicSubnetsInDifferentAZs(t, defaultVPCID, 2)

	instanceConfig := types.EC2InstanceConfig{
		InstanceType:       helpers.DefaultInstanceType,
		OSType:             osType,
		VolumeSize:         100,
		VolumeType:         "gp3",
		EnableASG:          enableASG,
		ASGMinSize:         1,
		ASGMaxSize:         3,
		ASGDesiredCapacity: 2,
		UniqueIdentifier:   instanceName,
	}

	securityConfig := types.SecurityConfig{
		AdditionalSecurityGroups: []string{securityGroupID},
	}

	terraformOptions := helpers.ConfigureTerraformOptions(
		subnetIDs,
		instanceConfig,
		securityConfig,
		instanceProfile,
		instanceName,
		defaultVPCID,
	)

	// Add longer timeout for Windows instances
	if osType == "windows" {
		terraformOptions.MaxRetries = 3
		terraformOptions.TimeBetweenRetries = 30 * time.Second
	}

	// Update working directory to temp directory
	terraformOptions.TerraformDir = tempDir

	// Add retries for Windows instances
	maxRetries := 1
	if osType == "windows" {
		maxRetries = 3
	}

	var applyErr error
	for i := 0; i < maxRetries; i++ {
		_, applyErr = terraform.InitAndApplyE(t, terraformOptions)
		if applyErr == nil {
			break
		}

		if i < maxRetries-1 {
			t.Logf("Attempt %d for %s instance failed with error: %v. Retrying after cleanup...",
				i+1, osType, applyErr)

			// Clean up any partial resources before retrying
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
			if err == nil {
				ec2Client := ec2.NewFromConfig(cfg)
				cleanup.CleanupInstancesByNamePrefix(t, ec2Client, instanceName)
				time.Sleep(10 * time.Second)
			}

			time.Sleep(30 * time.Second)
		}
	}
	require.NoError(t, applyErr, "Failed to apply Terraform configuration after %d attempts", maxRetries)

	ctxVal := helpers.ExtractOutputs(t, terraformOptions, instanceConfig)

	// Set up deferred cleanup in reverse order of creation with sequencing protection
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	// Using a deferred function with the cleanup tracker to prevent race conditions
	defer func() {
		cleanupTracker.Mutex.Lock()
		defer cleanupTracker.Mutex.Unlock()

		if !cleanupTracker.Complete {
			t.Log("Running coordinated cleanup for test resources...")

			// Perform instance validation first if possible
			if ctxVal != nil && len(ctxVal.InstanceIDs) > 0 {
				// For Windows, add a longer wait time before validation to ensure the instance is stable
				if osType == "windows" {
					t.Log("Waiting for Windows instance to stabilize before validation...")
					time.Sleep(60 * time.Second)
				}

				// Validate resources with a timeout to prevent hanging
				validationErr := helpers.ValidateResources(ctxVal, instanceConfig)
				if validationErr != nil {
					t.Logf("Resource validation warning: %v", validationErr)
				}
			}

			// Explicitly destroy with Terraform if requested
			if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
				t.Log("Destroying infrastructure with Terraform...")
				terraform.Destroy(t, terraformOptions)
			}

			// Clean up IAM resources
			if err := cleanup.IAMRole(t, roleName); err != nil {
				t.Logf("Warning: Failed to clean up IAM role: %v", err)
			}
			if err := cleanup.InstanceProfile(t, instanceProfile); err != nil {
				t.Logf("Warning: Failed to clean up instance profile: %v", err)
			}

			// Clean up security group last
			cleanup.SecurityGroup(t, ec2Client, securityGroupID)

			cleanupTracker.Complete = true
		} else {
			t.Log("Cleanup already performed, skipping...")
		}
	}()

	// Run validation if not in cleanup mode
	if !cleanupTracker.Complete {
		// For Windows instances, add a special wait time before validation
		if osType == "windows" {
			t.Log("Waiting for Windows instance to fully initialize...")
			time.Sleep(30 * time.Second)
		}

		err = helpers.ValidateResources(ctxVal, instanceConfig)
		require.NoError(t, err, "Resource validation failed")
	}
}

func runTerraformTestWithASGConfig(t *testing.T, minSize, maxSize, desiredCapacity int, instanceName string) {
	ctx := context.Background()

	// Copy module to temp directory
	tempDir, err := helpers.CopyModuleToTempDir(t, ".")
	require.NoError(t, err)

	if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
		defer func() {
			t.Log("Cleaning up temporary directory...")
			if err := os.RemoveAll(tempDir); err != nil {
				t.Logf("Warning: Error cleaning up temporary directory: %v", err)
			}
		}()
	}

	defaultVPCID := helpers.GetDefaultVPCID(t)
	roleName := helpers.CreateIAMRole(t)
	instanceProfile := helpers.CreateInstanceProfile(t, roleName)
	securityGroupID := helpers.CreateSecurityGroup(t, defaultVPCID, instanceName)
	subnetIDs := helpers.GetPublicSubnetsInDifferentAZs(t, defaultVPCID, 2)

	// Get AWS clients
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)
	asgClient := asg.NewFromConfig(cfg)

	// Setup cleanup to run first
	defer func() {
		t.Log("Starting deferred cleanup...")

		// Clean up instances first
		cleanup.CleanupInstancesByNamePrefix(t, ec2Client, instanceName)
		time.Sleep(10 * time.Second) // Wait for instances to start terminating

		// Clean up ASG if it exists
		asgsOut, err := asgClient.DescribeAutoScalingGroups(ctx, &asg.DescribeAutoScalingGroupsInput{})
		if err == nil {
			for _, group := range asgsOut.AutoScalingGroups {
				if group.AutoScalingGroupName != nil && strings.Contains(*group.AutoScalingGroupName, instanceName) {
					t.Logf("Found ASG to clean up: %s", *group.AutoScalingGroupName)
					// Get launch template ID if it exists
					var ltID string
					if group.LaunchTemplate != nil && group.LaunchTemplate.LaunchTemplateId != nil {
						ltID = *group.LaunchTemplate.LaunchTemplateId
					}
					cleanup.CleanupASG(t, asgClient, ec2Client, *group.AutoScalingGroupName, ltID)
				}
			}
		}
		time.Sleep(30 * time.Second) // Wait for ASG cleanup

		// Clean up security groups
		cleanup.SecurityGroupsByNamePrefix(t, ec2Client, instanceName)
		cleanup.SecurityGroup(t, ec2Client, securityGroupID)

		// Finally clean up IAM resources
		if err := cleanup.InstanceProfile(t, instanceProfile); err != nil {
			t.Logf("Warning: Failed to clean up instance profile: %v", err)
		}
		if err := cleanup.IAMRole(t, roleName); err != nil {
			t.Logf("Warning: Failed to clean up IAM role: %v", err)
		}
	}()

	instanceConfig := types.EC2InstanceConfig{
		InstanceType:       helpers.DefaultInstanceType,
		OSType:             "linux",
		VolumeSize:         100,
		VolumeType:         "gp3",
		EnableASG:          true,
		ASGMinSize:         minSize,
		ASGMaxSize:         maxSize,
		ASGDesiredCapacity: desiredCapacity,
		UniqueIdentifier:   instanceName,
	}

	securityConfig := types.SecurityConfig{
		AdditionalSecurityGroups: []string{securityGroupID},
	}

	terraformOptions := helpers.ConfigureTerraformOptions(
		subnetIDs,
		instanceConfig,
		securityConfig,
		instanceProfile,
		instanceName,
		defaultVPCID,
	)

	// Update working directory to temp directory
	terraformOptions.TerraformDir = tempDir

	_, err = terraform.InitAndApplyE(t, terraformOptions)
	require.NoError(t, err, "Failed to apply Terraform configuration")

	ctxVal := helpers.ExtractOutputs(t, terraformOptions, instanceConfig)
	require.NotNil(t, ctxVal, "Failed to extract outputs")

	// Additional validation for ASG configuration
	asgsOut, err := asgClient.DescribeAutoScalingGroups(ctx, &asg.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{ctxVal.ASGName},
	})
	require.NoError(t, err, "Failed to describe ASG")
	require.Len(t, asgsOut.AutoScalingGroups, 1, "Expected exactly one ASG")

	asgGrp := asgsOut.AutoScalingGroups[0]
	require.Equal(t, int32(minSize), aws.ToInt32(asgGrp.MinSize), "Unexpected ASG min size")
	require.Equal(t, int32(maxSize), aws.ToInt32(asgGrp.MaxSize), "Unexpected ASG max size")
	require.Equal(t, int32(desiredCapacity), aws.ToInt32(asgGrp.DesiredCapacity), "Unexpected ASG desired capacity")

	// Validate resources
	err = helpers.ValidateResources(ctxVal, instanceConfig)
	require.NoError(t, err, "Resource validation failed")

	if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
		_, err = terraform.DestroyE(t, terraformOptions)
		require.NoError(t, err, "Failed to destroy Terraform configuration")
	}
}

func runTerraformTestWithStorageConfig(t *testing.T, volumeSize int, volumeType string, includeAdditionalVolumes bool, instanceName string) {
	ctx := context.Background()

	// Copy module to temp directory
	tempDir, err := helpers.CopyModuleToTempDir(t, ".")
	require.NoError(t, err)

	if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
		defer func() {
			t.Log("Cleaning up temporary directory...")
			if err := os.RemoveAll(tempDir); err != nil {
				t.Logf("Warning: Error cleaning up temporary directory: %v", err)
			}
		}()
	}

	defaultVPCID := helpers.GetDefaultVPCID(t)

	// Create AWS clients and clean up any existing resources with this prefix
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(helpers.DefaultRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)
	cleanup.SecurityGroupsByNamePrefix(t, ec2Client, instanceName)

	// Create AWS resources needed for test
	roleName := helpers.CreateIAMRole(t)
	instanceProfile := helpers.CreateInstanceProfile(t, roleName)
	securityGroupID := helpers.CreateSecurityGroup(t, defaultVPCID, instanceName)
	subnetIDs := helpers.GetPublicSubnetsInDifferentAZs(t, defaultVPCID, 2)

	// Setup test configuration
	instanceConfig := types.EC2InstanceConfig{
		InstanceType:     helpers.DefaultInstanceType,
		OSType:           "linux",
		VolumeSize:       volumeSize,
		VolumeType:       volumeType,
		EnableASG:        false,
		UniqueIdentifier: instanceName,
	}

	if includeAdditionalVolumes {
		instanceConfig.AdditionalVolumes = []types.EBSVolumeConfig{
			{
				DeviceName:          "/dev/sdb",
				VolumeSize:          100,
				VolumeType:          volumeType,
				DeleteOnTermination: true,
			},
			{
				DeviceName:          "/dev/sdc",
				VolumeSize:          25,
				VolumeType:          volumeType,
				DeleteOnTermination: true,
			},
		}
	}

	securityConfig := types.SecurityConfig{
		AdditionalSecurityGroups: []string{securityGroupID},
	}

	// Create Terraform options
	terraformOptions := helpers.ConfigureTerraformOptions(
		subnetIDs,
		instanceConfig,
		securityConfig,
		instanceProfile,
		instanceName,
		defaultVPCID,
	)

	// Update working directory to temp directory
	terraformOptions.TerraformDir = tempDir

	// Use a channel to track if Terraform destroy completed successfully
	destroyCompleted := make(chan bool, 1)
	defer close(destroyCompleted)

	// Register cleanup handlers to run at test completion
	t.Cleanup(func() {
		// Wait a brief moment to see if terraform destroy completes
		select {
		case <-destroyCompleted:
			// Terraform destroy completed, skip manual cleanup
			t.Log("Terraform destroy completed, skipping manual cleanup")
		case <-time.After(2 * time.Second):
			// Terraform destroy didn't complete, perform manual cleanup
			t.Log("Performing manual resource cleanup")
			cleanup.CleanupInstancesByNamePrefix(t, ec2Client, instanceName)
			cleanup.SecurityGroup(t, ec2Client, securityGroupID)
		}

		// Always clean up IAM resources as they might be outside terraform's scope
		if err := cleanup.IAMRole(t, roleName); err != nil {
			t.Logf("Warning: Failed to clean up IAM role: %v", err)
		}
		if err := cleanup.InstanceProfile(t, instanceProfile); err != nil {
			t.Logf("Warning: Failed to clean up instance profile: %v", err)
		}
	})

	_, err = terraform.InitAndApplyE(t, terraformOptions)
	require.NoError(t, err, "Failed to apply Terraform configuration")

	ctxVal := helpers.ExtractOutputs(t, terraformOptions, instanceConfig)
	err = helpers.ValidateResources(ctxVal, instanceConfig)
	require.NoError(t, err, "Resource validation failed")

	if ctxVal != nil {
		helpers.ValidateStorageConfig(t, ctxVal, volumeSize, volumeType, includeAdditionalVolumes)
	} else {
		t.Fatal("Validation context is nil, cannot validate storage configuration")
	}

	if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
		terraform.Destroy(t, terraformOptions)
		destroyCompleted <- true
	}
}
