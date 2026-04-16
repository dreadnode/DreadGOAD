package cleanup

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	autoscaling "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// CleanupLaunchTemplate deletes a launch template and waits for deletion to complete
func CleanupLaunchTemplate(t *testing.T, ec2Client *ec2.Client, ltID string) error {
	if ltID == "" {
		return nil
	}

	ctx := context.Background()

	// First check if the launch template exists
	_, err := ec2Client.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateIds: []string{ltID},
	})
	if err != nil {
		// If it's already gone, we're done
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return fmt.Errorf("error checking launch template existence: %v", err)
	}

	// Delete the launch template
	_, err = ec2Client.DeleteLaunchTemplate(ctx, &ec2.DeleteLaunchTemplateInput{
		LaunchTemplateId: aws.String(ltID),
	})
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return fmt.Errorf("error deleting launch template %s: %v", ltID, err)
		}
		return nil
	}

	// Wait for the launch template to be deleted
	maxRetries := 10
	retryInterval := 5 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := ec2Client.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{
			LaunchTemplateIds: []string{ltID},
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				t.Logf("Successfully deleted launch template %s", ltID)
				return nil
			}
		}
		time.Sleep(retryInterval)
	}

	return fmt.Errorf("timed out waiting for launch template %s to be deleted", ltID)
}

// CleanupEC2Instance terminates an EC2 instance and waits for termination to complete
func CleanupEC2Instance(t *testing.T, ec2Client *ec2.Client, instanceID string) error {
	if instanceID == "" {
		t.Log("Empty instance ID provided, skipping cleanup")
		return nil
	}

	ctx := context.Background()
	t.Logf("Starting cleanup for instance %s", instanceID)

	// First check if instance exists and get its current state
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}
	result, err := ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "invalidinstanceid.notfound") || strings.Contains(lower, "not found") {
			t.Logf("Instance %s not found, assuming already terminated", instanceID)
			return nil
		}
		return fmt.Errorf("error checking instance %s status: %v", instanceID, err)
	}

	// Check if instance exists in the response
	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		t.Logf("Instance %s not found in response, assuming already terminated", instanceID)
		return nil
	}

	// Get current instance state
	stateName := "<unknown>"
	if result.Reservations[0].Instances[0].State != nil {
		stateName = string(result.Reservations[0].Instances[0].State.Name)
	}
	t.Logf("Instance %s current state: %s", instanceID, stateName)

	// If already terminated or shutting down, nothing to do
	if stateName == "terminated" || stateName == "shutting-down" {
		t.Logf("Instance %s is already %s, no action needed", instanceID, stateName)
		return nil
	}

	// Attempt to terminate the instance
	t.Logf("Initiating termination for instance %s", instanceID)
	_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "invalidinstanceid.notfound") || strings.Contains(lower, "not found") {
			t.Logf("Instance %s not found during termination, assuming already terminated", instanceID)
			return nil
		}
		return fmt.Errorf("failed to initiate termination for instance %s: %v", instanceID, err)
	}

	return waitForInstanceTermination(t, ec2Client, instanceID)
}

// CleanupInstancesByNamePrefix terminates all instances matching the given name prefix
func CleanupInstancesByNamePrefix(t *testing.T, ec2Client *ec2.Client, prefix string) {
	ctx := context.Background()

	// Use a broader filter to catch all instances with similar naming pattern
	filters := []ec2types.Filter{
		{
			Name:   aws.String("tag:Name"),
			Values: []string{"*" + prefix + "*"},
		},
		{
			Name:   aws.String("instance-state-name"),
			Values: []string{"pending", "running", "stopping", "stopped"},
		},
	}

	result, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: filters,
	})
	if err != nil {
		t.Logf("Error finding instances to terminate: %v", err)
		return
	}

	var instanceIds []string
	// Collect all instance IDs
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instanceName := "<unknown>"
			for _, tag := range instance.Tags {
				if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
					instanceName = *tag.Value
					break
				}
			}
			if instance.InstanceId != nil {
				t.Logf("Found instance to clean up: %s (Name: %s)", *instance.InstanceId, instanceName)
				instanceIds = append(instanceIds, *instance.InstanceId)
			}
		}
	}

	if len(instanceIds) == 0 {
		t.Logf("No instances found matching prefix: %s", prefix)
		return
	}

	// Terminate all instances in one call
	t.Logf("Force terminating %d instances...", len(instanceIds))
	_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instanceIds,
	})
	if err != nil {
		t.Logf("Error terminating instances: %v", err)
		return
	}
	t.Logf("Successfully initiated termination for %d instances", len(instanceIds))

	// Wait for complete termination of all instances
	waitForBulkInstanceTermination(t, ec2Client, instanceIds)
}

// waitForInstanceTermination waits for a single instance to terminate
func waitForInstanceTermination(t *testing.T, ec2Client *ec2.Client, instanceID string) error {
	ctx := context.Background()

	maxRetries := 30
	retryInterval := 10 * time.Second

	for i := 0; i < maxRetries; i++ {
		statusResult, err := ec2Client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
			InstanceIds:         []string{instanceID},
			IncludeAllInstances: aws.Bool(true),
		})
		if err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "invalidinstanceid.notfound") || strings.Contains(lower, "not found") {
				t.Logf("Instance %s confirmed terminated", instanceID)
				return nil
			}
			t.Logf("Warning: Error checking instance %s status: %v", instanceID, err)
			time.Sleep(retryInterval)
			continue
		}

		if len(statusResult.InstanceStatuses) == 0 {
			t.Logf("Instance %s confirmed terminated", instanceID)
			return nil
		}

		state := "<unknown>"
		if statusResult.InstanceStatuses[0].InstanceState != nil {
			state = string(statusResult.InstanceStatuses[0].InstanceState.Name)
		}
		if state == "terminated" {
			t.Logf("Instance %s confirmed terminated", instanceID)
			return nil
		}

		t.Logf("Waiting for instance %s to terminate (current state: %s), attempt %d/%d",
			instanceID, state, i+1, maxRetries)
		time.Sleep(retryInterval)
	}

	return fmt.Errorf("timeout waiting for instance %s to terminate", instanceID)
}

// waitForBulkInstanceTermination waits for multiple instances to terminate
func waitForBulkInstanceTermination(t *testing.T, ec2Client *ec2.Client, instanceIds []string) {
	ctx := context.Background()

	maxRetries := 60 // ~10 minutes total
	retryInterval := 10 * time.Second

	t.Logf("Waiting for instances to terminate...")
	for i := 0; i < maxRetries; i++ {
		stillRunning := false
		checkResult, err := ec2Client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
			InstanceIds:         instanceIds,
			IncludeAllInstances: aws.Bool(true),
		})

		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "invalidinstanceid.notfound") {
				// All instances are gone
				t.Logf("All instances confirmed terminated")
				return
			}
			t.Logf("Warning: Error checking instance status: %v", err)
		} else {
			for _, status := range checkResult.InstanceStatuses {
				if status.InstanceState == nil || status.InstanceState.Name != ec2types.InstanceStateNameTerminated {
					stillRunning = true
					break
				}
			}
			if !stillRunning && len(checkResult.InstanceStatuses) == 0 {
				t.Logf("All instances confirmed terminated")
				return
			}
		}

		if !stillRunning {
			t.Logf("All instances confirmed terminated")
			return
		}

		t.Logf("Waiting for instances to terminate... (attempt %d/%d)", i+1, maxRetries)
		time.Sleep(retryInterval)
	}

	t.Logf("WARNING: Timed out waiting for all instances to terminate")
}

// CleanupASG cleans up an Auto Scaling Group and its associated launch template
func CleanupASG(t *testing.T, asgClient *autoscaling.Client, ec2Client *ec2.Client, asgName, launchTemplateID string) {
	if asgName == "" {
		return
	}

	ctx := context.Background()
	t.Logf("Starting cleanup for ASG: %s", asgName)

	// First check if ASG exists
	result, err := asgClient.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	})
	if (err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")) || len(result.AutoScalingGroups) == 0 {
		t.Logf("ASG %s already deleted, proceeding with launch template cleanup", asgName)
		goto cleanup_launch_template
	}

	// Scale down if ASG exists
	_, err = asgClient.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int32(0),
		MinSize:              aws.Int32(0),
		MaxSize:              aws.Int32(0),
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("Warning: Failed to scale down ASG: %v", err)
	}

	// Delete ASG
	_, err = asgClient.DeleteAutoScalingGroup(ctx, &autoscaling.DeleteAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		ForceDelete:          aws.Bool(true),
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("Warning: Failed to delete ASG: %v", err)
	}

cleanup_launch_template:
	// Clean up launch template if provided
	if launchTemplateID != "" {
		if err := CleanupLaunchTemplate(t, ec2Client, launchTemplateID); err != nil {
			t.Logf("Warning: Failed to clean up launch template: %v", err)
		}
	}
}
