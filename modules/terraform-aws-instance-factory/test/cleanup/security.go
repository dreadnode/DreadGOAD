package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
)

// SecurityGroup attempts to clean up a security group with proper handling of dependencies
func SecurityGroup(t *testing.T, ec2Client *ec2.Client, sgID string) {
	if sgID == "" {
		t.Log("Empty security group ID provided, skipping cleanup")
		return
	}
	ctx := context.Background()

	// Get the security group details
	sg, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgID},
	})
	if err != nil {
		if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "InvalidGroup.NotFound" {
			t.Logf("Security group %s not found, already deleted", sgID)
			return
		}
		t.Logf("Warning: Error describing security group %s: %v", sgID, err)
		return
	}

	if len(sg.SecurityGroups) == 0 {
		t.Logf("Security group %s not found", sgID)
		return
	}

	// Check for dependent ENIs first
	checkForDependentENIs(t, ec2Client, sgID)

	// Remove all ingress rules
	if len(sg.SecurityGroups[0].IpPermissions) > 0 {
		_, err = ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: sg.SecurityGroups[0].IpPermissions,
		})
		if err != nil {
			t.Logf("Warning: Error revoking ingress rules for security group %s: %v", sgID, err)
		} else {
			t.Logf("Successfully revoked all ingress rules for security group %s", sgID)
		}
	}

	// Remove all egress rules
	if len(sg.SecurityGroups[0].IpPermissionsEgress) > 0 {
		_, err = ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: sg.SecurityGroups[0].IpPermissionsEgress,
		})
		if err != nil {
			t.Logf("Warning: Error revoking egress rules for security group %s: %v", sgID, err)
		} else {
			t.Logf("Successfully revoked all egress rules for security group %s", sgID)
		}
	}

	// Wait for rules to be removed
	time.Sleep(5 * time.Second)

	// Try to delete the security group with retries
	maxRetries := 30
	retryInterval := 10 * time.Second
	for i := 0; i < maxRetries; i++ {
		_, err = ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(sgID),
		})
		if err == nil {
			t.Logf("Successfully deleted security group %s", sgID)
			return
		}

		if apiErr, ok := err.(smithy.APIError); ok {
			// If the security group is not found, it's already deleted
			if apiErr.ErrorCode() == "InvalidGroup.NotFound" {
				t.Logf("Security group %s not found, already deleted", sgID)
				return
			}

			if apiErr.ErrorCode() == "DependencyViolation" {
				// Check for dependent ENIs before retrying
				remaining := checkForDependentENIs(t, ec2Client, sgID)
				if remaining == 0 {
					t.Logf("No more ENIs found, but still got dependency violation. Waiting additional time...")
				} else {
					t.Logf("Security group %s still has %d dependent ENIs", sgID, remaining)
				}

				// Wait longer for complex dependencies to resolve
				t.Logf("Security group %s still has dependencies, retrying in %v... (attempt %d/%d)",
					sgID, retryInterval, i+1, maxRetries)
				time.Sleep(retryInterval)
				continue
			}
		}

		t.Logf("Error deleting security group %s: %v (attempt %d/%d)",
			sgID, err, i+1, maxRetries)
		time.Sleep(retryInterval)
	}

	t.Logf("WARNING: Failed to delete security group %s after %d attempts", sgID, maxRetries)
}

// SecurityGroupsForVPC cleans up all non-default security groups in a VPC with optional tag filtering
func SecurityGroupsForVPC(t *testing.T, ec2Client *ec2.Client, vpcID string, tagFilters ...ec2types.Filter) {
	ctx := context.Background()

	filters := []ec2types.Filter{
		{
			Name:   aws.String("vpc-id"),
			Values: []string{vpcID},
		},
	}

	// Add any additional tag filters
	filters = append(filters, tagFilters...)

	sgs, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	})
	if err != nil {
		t.Logf("Warning: Failed to describe security groups for VPC %s: %v", vpcID, err)
		return
	}

	for _, sg := range sgs.SecurityGroups {
		// Skip the default security group
		if sg.GroupName != nil && *sg.GroupName != "default" && sg.GroupId != nil {
			t.Logf("Cleaning up security group: %s (%s)", *sg.GroupId, aws.ToString(sg.GroupName))
			SecurityGroup(t, ec2Client, *sg.GroupId)
		}
	}
}

// SecurityGroupsByNamePrefix cleans up security groups matching a name prefix
func SecurityGroupsByNamePrefix(t *testing.T, ec2Client *ec2.Client, prefix string) {
	ctx := context.Background()

	// Find all security groups with the given prefix
	nameTagFilter := ec2types.Filter{
		Name:   aws.String("tag:Name"),
		Values: []string{"*" + prefix + "*"},
	}

	groupNameFilter := ec2types.Filter{
		Name:   aws.String("group-name"),
		Values: []string{"*" + prefix + "*"},
	}

	// Get SGs by name tag
	result, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{nameTagFilter},
	})
	if err != nil {
		t.Logf("Error describing security groups by tag: %v", err)
	}

	// Get SGs by group name
	nameResult, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{groupNameFilter},
	})
	if err != nil {
		t.Logf("Error describing security groups by name: %v", err)
	}

	// Combine results (avoiding duplicates)
	sgMap := make(map[string]ec2types.SecurityGroup)
	for _, sg := range result.SecurityGroups {
		if sg.GroupId != nil {
			sgMap[*sg.GroupId] = sg
		}
	}
	for _, sg := range nameResult.SecurityGroups {
		if sg.GroupId != nil {
			if _, exists := sgMap[*sg.GroupId]; !exists {
				sgMap[*sg.GroupId] = sg
			}
		}
	}

	t.Logf("Found %d security groups to clean up for prefix: %s", len(sgMap), prefix)

	for _, sg := range sgMap {
		// Skip the default security group
		if sg.GroupName != nil && *sg.GroupName == "default" {
			continue
		}
		if sg.GroupId != nil {
			t.Logf("Cleaning up security group: %s (%s)", *sg.GroupId, aws.ToString(sg.GroupName))
			SecurityGroup(t, ec2Client, *sg.GroupId)
		}
	}
}

// CreateEC2ClientWithRegion creates an EC2 client for the specified region
func CreateEC2ClientWithRegion(t *testing.T, region string) *ec2.Client {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}
	return ec2.NewFromConfig(cfg)
}

// CleanupSecurityGroupWithRegion deletes a security group in the specified region
func CleanupSecurityGroupWithRegion(t *testing.T, region, sgID string) {
	t.Helper()
	if sgID == "" {
		return
	}
	ec2Client := CreateEC2ClientWithRegion(t, region)
	SecurityGroup(t, ec2Client, sgID)
}

// checkForDependentENIs checks for and logs any network interfaces using the security group
// Returns the number of dependent ENIs found
func checkForDependentENIs(t *testing.T, ec2Client *ec2.Client, sgID string) int {
	ctx := context.Background()

	enis, eniErr := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("group-id"),
				Values: []string{sgID},
			},
		},
	})
	if eniErr != nil {
		t.Logf("Error checking dependent ENIs: %v", eniErr)
		return 0
	}

	count := len(enis.NetworkInterfaces)
	if count > 0 {
		t.Logf("Security group %s has %d dependent ENIs", sgID, count)
		for _, eni := range enis.NetworkInterfaces {
			status := "unknown"
			if eni.Status != "" {
				status = string(eni.Status)
			}

			attachmentState := "not attached"
			if eni.Attachment != nil && eni.Attachment.Status != "" {
				attachmentState = string(eni.Attachment.Status)
			}

			instanceID := "none"
			if eni.Attachment != nil && eni.Attachment.InstanceId != nil {
				instanceID = *eni.Attachment.InstanceId
			}

			if eni.NetworkInterfaceId != nil {
				t.Logf("  ENI: %s, Status: %s, Attachment: %s, Instance: %s",
					*eni.NetworkInterfaceId, status, attachmentState, instanceID)
			}
		}
	}

	return count
}
