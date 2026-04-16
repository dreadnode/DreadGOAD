package helpers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/stretchr/testify/assert"
)

var sgNameSanitizer = regexp.MustCompile(`[^A-Za-z0-9._\-]+`)

func sanitizePrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "test"
	}
	p = sgNameSanitizer.ReplaceAllString(p, "-")
	// keep well under AWS’s 255 limit after we add suffixes
	if len(p) > 200 {
		p = p[:200]
	}
	return p
}

// CreateSecurityGroup creates a temporary security group for testing (AWS SDK v2)
func CreateSecurityGroup(t *testing.T, vpcID string, namePrefix string) string {
	t.Helper()

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(DefaultRegion))
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}
	ec2Client := ec2.NewFromConfig(cfg)

	uniqueID := random.UniqueId()
	base := sanitizePrefix(namePrefix)
	sgName := fmt.Sprintf("%s-sg-%s", base, uniqueID)

	out, err := ec2Client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String("Temporary security group for testing"),
		VpcId:       aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSecurityGroup,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String(sgName)},
					{Key: aws.String("CreatedBy"), Value: aws.String("terratest")},
					{Key: aws.String("TestPrefix"), Value: aws.String(namePrefix)},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create security group in VPC %s: %v", vpcID, err)
	}
	sgID := aws.ToString(out.GroupId)
	t.Logf("Created temporary security group: %s (%s) in %s", sgName, sgID, vpcID)

	// Wait until the SG is consistently visible
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		_, derr := ec2Client.DescribeSecurityGroups(waitCtx, &ec2.DescribeSecurityGroupsInput{
			GroupIds: []string{sgID},
		})
		if derr == nil {
			break
		}
		// brief backoff
		select {
		case <-time.After(1 * time.Second):
		case <-waitCtx.Done():
			t.Fatalf("Security group %s not consistently visible after create: %v", sgID, waitCtx.Err())
		}
	}

	return sgID
}

// VerifySecurityGroupRules verifies that a security group has the expected ingress and egress rules (AWS SDK v2)
func VerifySecurityGroupRules(t *testing.T, sgID string, ingressRules []types.SecurityGroupRule, egressRules []types.SecurityGroupRule) {
	t.Helper()

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(DefaultRegion),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Get security group details
	describeOutput, err := ec2Client.DescribeSecurityGroups(context.Background(), &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgID},
	})
	if err != nil {
		t.Fatalf("Failed to describe security group %s: %v", sgID, err)
	}

	if len(describeOutput.SecurityGroups) == 0 {
		t.Fatalf("Security group %s not found", sgID)
	}

	sg := describeOutput.SecurityGroups[0]

	// Verify ingress rules
	t.Logf("Verifying %d ingress rules", len(ingressRules))
	assert.Equal(t, len(ingressRules), len(sg.IpPermissions), "Number of ingress rules doesn't match expected")
	for _, expectedRule := range ingressRules {
		found := false
		for _, actualRule := range sg.IpPermissions {
			if matchSecurityGroupRule(expectedRule, actualRule) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected ingress rule not found: %+v", expectedRule)
	}

	// Verify egress rules
	t.Logf("Verifying %d egress rules", len(egressRules))
	assert.Equal(t, len(egressRules), len(sg.IpPermissionsEgress), "Number of egress rules doesn't match expected")
	for _, expectedRule := range egressRules {
		found := false
		for _, actualRule := range sg.IpPermissionsEgress {
			if matchSecurityGroupRule(expectedRule, actualRule) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected egress rule not found: %+v", expectedRule)
	}
}

// matchSecurityGroupRule checks if an actual security group rule matches the expected rule (AWS SDK v2 types)
func matchSecurityGroupRule(expected types.SecurityGroupRule, actual ec2types.IpPermission) bool {
	if aws.ToInt32(actual.FromPort) != int32(expected.FromPort) ||
		aws.ToInt32(actual.ToPort) != int32(expected.ToPort) ||
		aws.ToString(actual.IpProtocol) != expected.Protocol {
		return false
	}

	if len(actual.IpRanges) != len(expected.CIDRBlocks) {
		return false
	}

	for _, expectedCIDR := range expected.CIDRBlocks {
		cidrFound := false
		for _, ipRange := range actual.IpRanges {
			if aws.ToString(ipRange.CidrIp) == expectedCIDR {
				if expected.Description != "" && ipRange.Description != nil {
					if aws.ToString(ipRange.Description) == expected.Description {
						cidrFound = true
						break
					}
				} else {
					cidrFound = true
					break
				}
			}
		}
		if !cidrFound {
			return false
		}
	}

	return true
}
