package cleanup

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	smithy "github.com/aws/smithy-go"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
)

// IAMRole deletes an IAM role and its associated policies
func IAMRole(t *testing.T, roleName string) error {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(types.AwsRegion))
	if err != nil {
		return err
	}
	iamClient := iam.NewFromConfig(cfg)

	// List and detach attached policies
	policies, err := iamClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		if !containsNoSuchEntity(err) {
			t.Logf("Warning: Failed to list policies for role %s: %v", roleName, err)
		}
	} else {
		for _, policy := range policies.AttachedPolicies {
			_, derr := iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  aws.String(roleName),
				PolicyArn: policy.PolicyArn,
			})
			if derr != nil {
				t.Logf("Warning: Failed to detach policy from role %s: %v", roleName, derr)
			}
		}
	}

	// List and remove inline policies
	inlinePolicies, err := iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err == nil {
		for _, policyName := range inlinePolicies.PolicyNames {
			_, derr := iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
				RoleName:   aws.String(roleName),
				PolicyName: aws.String(policyName),
			})
			if derr != nil {
				t.Logf("Warning: Failed to delete inline policy %s from role %s: %v", policyName, roleName, derr)
			}
		}
	}

	// Delete instance profile associations
	profileList, err := iamClient.ListInstanceProfilesForRole(ctx, &iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String(roleName),
	})
	if err == nil {
		for _, profile := range profileList.InstanceProfiles {
			if profile.InstanceProfileName == nil {
				continue
			}
			if derr := removeRoleFromInstanceProfile(t, ctx, iamClient, aws.ToString(profile.InstanceProfileName), roleName); derr != nil {
				t.Logf("Warning: Failed to remove role from instance profile: %v", derr)
			}
		}
	}

	// Delete the role
	_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil && !containsNoSuchEntity(err) {
		return fmt.Errorf("failed to delete role %s: %v", roleName, err)
	}

	return nil
}

// InstanceProfile deletes an IAM instance profile and its role associations
func InstanceProfile(t *testing.T, profileName string) error {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(types.AwsRegion))
	if err != nil {
		return err
	}
	iamClient := iam.NewFromConfig(cfg)

	// Get the instance profile to check for role associations
	result, err := iamClient.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil {
		if !containsNoSuchEntity(err) {
			return fmt.Errorf("failed to get instance profile %s: %v", profileName, err)
		}
		return nil
	}

	// Remove roles from the instance profile
	if result.InstanceProfile != nil && len(result.InstanceProfile.Roles) > 0 {
		for _, role := range result.InstanceProfile.Roles {
			if role.RoleName == nil {
				continue
			}
			if derr := removeRoleFromInstanceProfile(t, ctx, iamClient, profileName, *role.RoleName); derr != nil {
				t.Logf("Warning: Failed to remove role from instance profile: %v", derr)
			}
		}
	}

	// Delete the instance profile
	_, err = iamClient.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil && !containsNoSuchEntity(err) {
		return fmt.Errorf("failed to delete instance profile %s: %v", profileName, err)
	}

	return nil
}

// removeRoleFromInstanceProfile removes a role from an instance profile
func removeRoleFromInstanceProfile(t *testing.T, ctx context.Context, iamClient *iam.Client, profileName, roleName string) error {
	_, err := iamClient.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
		RoleName:            aws.String(roleName),
	})
	if err != nil && !containsNoSuchEntity(err) {
		t.Logf("Warning: Failed to remove role %s from profile %s: %v", roleName, profileName, err)
		return fmt.Errorf("failed to remove role %s from instance profile %s: %v", roleName, profileName, err)
	}

	// Wait for role to be removed
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		result, gerr := iamClient.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
			InstanceProfileName: aws.String(profileName),
		})
		if gerr != nil {
			if containsNoSuchEntity(gerr) {
				return nil
			}
			break
		}

		if result.InstanceProfile == nil {
			return nil
		}

		hasRole := roleInProfile(result.InstanceProfile.Roles, roleName)
		if !hasRole {
			return nil
		}

		t.Logf("Waiting for role %s to be removed from profile %s...", roleName, profileName)
		time.Sleep(2 * time.Second)
	}

	return nil
}

func roleInProfile(roles []iamtypes.Role, roleName string) bool {
	for _, r := range roles {
		if r.RoleName != nil && *r.RoleName == roleName {
			return true
		}
	}
	return false
}

func containsNoSuchEntity(err error) bool {
	if err == nil {
		return false
	}
	// Prefer structured smithy errors
	if apiErr, ok := err.(smithy.APIError); ok {
		if strings.EqualFold(apiErr.ErrorCode(), "NoSuchEntity") {
			return true
		}
	}
	// Fallback to substring check
	return strings.Contains(strings.ToLower(err.Error()), "nosuchentity")
}
