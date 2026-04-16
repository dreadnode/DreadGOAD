package helpers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	asg "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

// ConfigureTerraformOptions creates terraform options object
func ConfigureTerraformOptions(
	subnetIDs []string,
	config types.EC2InstanceConfig,
	securityConfig types.SecurityConfig,
	instanceProfile string,
	instanceName string,
	vpcID string,
) *terraform.Options {
	// Add timestamp to instanceName to ensure uniqueness
	timestamp := time.Now().Format("20060102150405")
	uniqueinstanceName := fmt.Sprintf("%s-%s", instanceName, timestamp)

	vars := map[string]interface{}{
		"os_type":                       config.OSType,
		"instance_type":                 config.InstanceType,
		"enable_asg":                    config.EnableASG,
		"env":                           DefaultEnv,
		"instance_name":                 uniqueinstanceName,
		"vpc_id":                        vpcID,
		"subnet_id":                     subnetIDs[0],
		"instance_profile":              instanceProfile,
		"user_data":                     GenerateUserData(config.OSType),
		"root_volume_size":              config.VolumeSize,
		"volume_type":                   config.VolumeType,
		"additional_security_group_ids": securityConfig.AdditionalSecurityGroups,
		"ssh_public_key":                securityConfig.SSHPublicKey,
		"tags": map[string]string{
			"Environment": DefaultEnv,
			"Project":     "Terratest",
			"ManagedBy":   "Terratest",
		},
	}

	// Add ASG specific variables if enabled
	if config.EnableASG {
		vars["asg_subnet_ids"] = subnetIDs
		vars["asg_min_size"] = config.ASGMinSize
		vars["asg_max_size"] = config.ASGMaxSize
		vars["asg_desired_capacity"] = config.ASGDesiredCapacity
		vars["asg_health_check_type"] = "EC2"
		vars["asg_health_check_grace_period"] = 300
		vars["asg_force_delete"] = true
		vars["asg_termination_policies"] = []string{"Default"}
		vars["asg_tags"] = map[string]string{
			"Environment": DefaultEnv,
			"Project":     "Terratest",
			"ManagedBy":   "Terratest",
		}
	}

	// Add additional EBS volumes if specified
	if len(config.AdditionalVolumes) > 0 {
		additionalVolumes := make([]map[string]interface{}, len(config.AdditionalVolumes))
		for i, vol := range config.AdditionalVolumes {
			additionalVolumes[i] = map[string]interface{}{
				"device_name":           vol.DeviceName,
				"volume_size":           vol.VolumeSize,
				"volume_type":           vol.VolumeType,
				"delete_on_termination": vol.DeleteOnTermination,
			}
		}
		vars["additional_ebs_volumes"] = additionalVolumes
	}

	// Add ingress rules
	if len(securityConfig.IngressRules) > 0 {
		ingressRules := make([]map[string]interface{}, len(securityConfig.IngressRules))
		for i, rule := range securityConfig.IngressRules {
			ingressRules[i] = map[string]interface{}{
				"description": rule.Description,
				"from_port":   rule.FromPort,
				"to_port":     rule.ToPort,
				"protocol":    rule.Protocol,
				"cidr_blocks": rule.CIDRBlocks,
			}
		}
		vars["ingress_rules"] = ingressRules
	} else {
		// Default SSH rule
		publicIP, err := GetPublicIP(nil)
		if err != nil {
			panic(err)
		}
		vars["ingress_rules"] = []map[string]interface{}{
			{
				"description": "SSH access",
				"from_port":   22,
				"to_port":     22,
				"protocol":    "tcp",
				"cidr_blocks": []string{publicIP + "/32"},
			},
		}

		// Add RDP port for Windows
		if config.OSType == "windows" {
			vars["ingress_rules"] = append(vars["ingress_rules"].([]map[string]interface{}), map[string]interface{}{
				"description": "RDP access",
				"from_port":   3389,
				"to_port":     3389,
				"protocol":    "tcp",
				"cidr_blocks": []string{publicIP + "/32"},
			})
		}
	}

	// Configure egress rules if specified
	if len(securityConfig.EgressRules) > 0 {
		egressRules := make([]map[string]interface{}, len(securityConfig.EgressRules))
		for i, rule := range securityConfig.EgressRules {
			egressRules[i] = map[string]interface{}{
				"description": rule.Description,
				"from_port":   rule.FromPort,
				"to_port":     rule.ToPort,
				"protocol":    rule.Protocol,
				"cidr_blocks": rule.CIDRBlocks,
			}
		}
		vars["egress_rules"] = egressRules
	}

	return &terraform.Options{
		TerraformDir: "../",
		Vars:         vars,
		EnvVars: map[string]string{
			"AWS_DEFAULT_REGION": DefaultRegion,
		},
	}
}

func ExtractOutputs(t *testing.T, terraformOptions *terraform.Options, instanceConfig types.EC2InstanceConfig) *types.ValidationContext {
	context := &types.ValidationContext{
		T:                t,
		TerraformOptions: terraformOptions,
	}

	// Get security group ID
	if sgID, err := terraform.OutputE(t, terraformOptions, "security_group_id"); err == nil {
		context.SecurityGroupID = sgID
	}

	// Try to get instance IDs
	if instanceIds, err := terraform.OutputListE(t, terraformOptions, "instance_ids"); err == nil {
		context.InstanceIDs = instanceIds
	}

	// For ASG configurations, try different output possibilities
	if instanceConfig.EnableASG {
		// Try to get ASG name first
		if asgName, err := terraform.OutputE(t, terraformOptions, "asg_name"); err == nil {
			context.ASGName = asgName
		} else if asgID, err := terraform.OutputE(t, terraformOptions, "asg_id"); err == nil {
			// Fallback to ASG ID if name not available
			context.ASGName = asgID
		}

		// Try to get launch template ID - this might not be available immediately
		if launchTemplateID, err := terraform.OutputE(t, terraformOptions, "launch_template_id"); err == nil {
			context.LaunchTemplateID = launchTemplateID
		}
	}

	return context
}

func validateInstanceOutputs(ctx *types.ValidationContext) error {
	maxRetries := 12
	retryInterval := 15 * time.Second

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			ctx.T.Logf("Attempt %d: Waiting for instance details...", i+1)
		}

		// Try to get instance IDs
		if instanceIDs, err := terraform.OutputListE(ctx.T, ctx.TerraformOptions, "instance_ids"); err == nil {
			ctx.InstanceIDs = instanceIDs
		}

		// Try to get instance details (OutputMapE returns map[string]string)
		if rawDetails, err := terraform.OutputMapE(ctx.T, ctx.TerraformOptions, "instance_details"); err == nil {
			details := make(map[string]interface{}, len(rawDetails))
			for k, v := range rawDetails {
				details[k] = v
			}
			ctx.InstanceDetails = details

			// For non-ASG mode, we expect instance IDs and details
			if len(ctx.InstanceIDs) > 0 && len(details) > 0 {
				return nil
			}

			// For ASG mode, we just need the ASG name to be populated
			if ctx.ASGName != "" {
				return nil
			}
		} else {
			// Even if details aren't ready, ASG mode can pass once ASG name is set
			if ctx.ASGName != "" {
				return nil
			}
		}

		time.Sleep(retryInterval)
	}

	return fmt.Errorf("instance details not available after %d retries", maxRetries)
}

// ValidateResources validates the created AWS resources
func ValidateResources(ctx *types.ValidationContext, config types.EC2InstanceConfig) error {
	if ctx.SecurityGroupID == "" {
		return fmt.Errorf("security group ID is empty")
	}

	if config.EnableASG {
		return validateASGOutputs(ctx, config)
	}

	return validateInstanceOutputs(ctx)
}

func validateASGOutputs(ctx *types.ValidationContext, cfg types.EC2InstanceConfig) error {
	// First validate that we have an ASG name
	if ctx.ASGName == "" {
		return fmt.Errorf("ASG name is empty")
	}

	// v2: load config and create client (use awscfg alias to avoid shadowing)
	awsCfg, err := awscfg.LoadDefaultConfig(context.Background(), awscfg.WithRegion(DefaultRegion))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %v", err)
	}
	asgClient := asg.NewFromConfig(awsCfg)

	// Describe the ASG to get launch template information
	input := &asg.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{ctx.ASGName},
	}

	result, err := asgClient.DescribeAutoScalingGroups(context.Background(), input)
	if err != nil {
		return fmt.Errorf("failed to describe ASG: %v", err)
	}

	if len(result.AutoScalingGroups) == 0 {
		return fmt.Errorf("ASG %s not found", ctx.ASGName)
	}

	a := result.AutoScalingGroups[0]

	// Get launch template ID from ASG if not available in outputs
	if ctx.LaunchTemplateID == "" && a.LaunchTemplate != nil && a.LaunchTemplate.LaunchTemplateId != nil {
		ctx.LaunchTemplateID = aws.ToString(a.LaunchTemplate.LaunchTemplateId)
	}

	// Validate launch template exists
	if ctx.LaunchTemplateID == "" {
		return fmt.Errorf("launch template ID is empty")
	}

	// Validate ASG size constraints (v2 uses *int32)
	if aws.ToInt32(a.MinSize) != int32(cfg.ASGMinSize) {
		return fmt.Errorf("unexpected ASG min size: got %d, want %d", aws.ToInt32(a.MinSize), cfg.ASGMinSize)
	}

	if aws.ToInt32(a.MaxSize) != int32(cfg.ASGMaxSize) {
		return fmt.Errorf("unexpected ASG max size: got %d, want %d", aws.ToInt32(a.MaxSize), cfg.ASGMaxSize)
	}

	if aws.ToInt32(a.DesiredCapacity) != int32(cfg.ASGDesiredCapacity) {
		return fmt.Errorf("unexpected ASG desired capacity: got %d, want %d", aws.ToInt32(a.DesiredCapacity), cfg.ASGDesiredCapacity)
	}

	return nil
}
