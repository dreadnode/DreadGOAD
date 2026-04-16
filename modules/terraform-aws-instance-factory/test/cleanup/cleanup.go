package cleanup

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func ForceCleanup(t *testing.T, testPrefix string) {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-2"))
	if err != nil {
		t.Logf("Failed to load AWS config: %v", err)
		return
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Find and terminate all instances with the test prefix
	instances, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{"*" + testPrefix + "*"},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"pending", "running", "stopping", "stopped"},
			},
		},
	})
	if err != nil {
		t.Logf("Error describing instances: %v", err)
		return
	}

	var instanceIds []string
	for _, reservation := range instances.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil {
				instanceIds = append(instanceIds, *instance.InstanceId)
			}
		}
	}

	if len(instanceIds) > 0 {
		_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: instanceIds,
		})
		if err != nil {
			t.Logf("Error terminating instances: %v", err)
		}
	}

	// Find and delete all security groups with the test prefix
	sgs, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("group-name"),
				Values: []string{"*" + testPrefix + "*", "test-sg-*"},
			},
		},
	})
	if err != nil {
		t.Logf("Error describing security groups: %v", err)
		return
	}

	for _, sg := range sgs.SecurityGroups {
		// Skip the default security group
		if sg.GroupName != nil && *sg.GroupName == "default" {
			continue
		}

		// First remove all ingress rules
		if len(sg.IpPermissions) > 0 && sg.GroupId != nil {
			_, err = ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissions,
			})
			if err != nil {
				t.Logf("Error revoking ingress rules for SG %s: %v", aws.ToString(sg.GroupId), err)
			}
		}

		// Then remove all egress rules
		if len(sg.IpPermissionsEgress) > 0 && sg.GroupId != nil {
			_, err = ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissionsEgress,
			})
			if err != nil {
				t.Logf("Error revoking egress rules for SG %s: %v", aws.ToString(sg.GroupId), err)
			}
		}

		// Finally delete the security group
		if sg.GroupId != nil {
			_, err = ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			})
			if err != nil {
				t.Logf("Error deleting security group %s: %v", aws.ToString(sg.GroupId), err)
			}
		}
	}
}
