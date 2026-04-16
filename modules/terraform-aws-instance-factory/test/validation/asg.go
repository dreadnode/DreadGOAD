package validation

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	asg "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
)

// ASGValidator handles ASG validation operations
type ASGValidator struct {
	client   *asg.Client
	retries  int
	waitTime time.Duration
}

// NewASGValidator creates a new ASG validator (AWS SDK v2)
// Pass in an aws.Config that already has region/creds set (e.g., from config.LoadDefaultConfig).
func NewASGValidator(cfg aws.Config, retries int, waitTime time.Duration) *ASGValidator {
	return &ASGValidator{
		client:   asg.NewFromConfig(cfg),
		retries:  retries,
		waitTime: waitTime,
	}
}

// ValidateASG validates ASG configuration
func (v *ASGValidator) ValidateASG(ctx *types.ValidationContext, expectedConfig types.EC2InstanceConfig) error {
	if ctx.ASGName == "" {
		return fmt.Errorf("ASG name is empty")
	}
	if ctx.LaunchTemplateID == "" {
		return fmt.Errorf("launch template ID is empty")
	}

	var group *asgtypes.AutoScalingGroup
	var err error

	bg := context.Background()
	for i := 0; i < v.retries; i++ {
		group, err = v.getASGDetails(bg, ctx.ASGName)
		if err == nil && group != nil {
			break
		}
		ctx.T.Logf("Attempt %d: Waiting for ASG configuration...", i+1)
		time.Sleep(v.waitTime)
	}
	if group == nil {
		return fmt.Errorf("failed to get ASG details after %d attempts", v.retries)
	}

	// Log configuration comparison
	ctx.T.Logf(
		"ASG %s configuration - Expected: min=%d, max=%d, desired=%d, Actual: min=%d, max=%d, desired=%d",
		ctx.ASGName,
		expectedConfig.ASGMinSize, expectedConfig.ASGMaxSize, expectedConfig.ASGDesiredCapacity,
		aws.ToInt32(group.MinSize), aws.ToInt32(group.MaxSize), aws.ToInt32(group.DesiredCapacity),
	)

	return v.validateASGConfiguration(group, expectedConfig)
}

func (v *ASGValidator) getASGDetails(ctx context.Context, asgName string) (*asgtypes.AutoScalingGroup, error) {
	res, err := v.client.DescribeAutoScalingGroups(ctx, &asg.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	})
	if err != nil {
		return nil, err
	}
	if len(res.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("ASG not found")
	}
	return &res.AutoScalingGroups[0], nil
}

func (v *ASGValidator) validateASGConfiguration(group *asgtypes.AutoScalingGroup, cfg types.EC2InstanceConfig) error {
	if aws.ToInt32(group.MinSize) != int32(cfg.ASGMinSize) {
		return fmt.Errorf("ASG min size mismatch: expected %d, got %d",
			cfg.ASGMinSize, aws.ToInt32(group.MinSize))
	}
	if aws.ToInt32(group.MaxSize) != int32(cfg.ASGMaxSize) {
		return fmt.Errorf("ASG max size mismatch: expected %d, got %d",
			cfg.ASGMaxSize, aws.ToInt32(group.MaxSize))
	}
	if aws.ToInt32(group.DesiredCapacity) != int32(cfg.ASGDesiredCapacity) {
		return fmt.Errorf("ASG desired capacity mismatch: expected %d, got %d",
			cfg.ASGDesiredCapacity, aws.ToInt32(group.DesiredCapacity))
	}
	return nil
}
