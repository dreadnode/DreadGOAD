package helpers

import (
	"context"
	"testing"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GenerateUserData creates appropriate user data for the OS type
func GenerateUserData(osType string) string {
	switch osType {
	case "linux":
		return `#!/bin/bash
echo "Starting configuration at $(date)"
yum update -y
echo "Configuration complete at $(date)"
`
	case "windows":
		return `<powershell>
Write-Host "Starting configuration at $(Get-Date)"
Install-WindowsFeature -Name Web-Server
Write-Host "Configuration complete at $(Get-Date)"
</powershell>
`
	case "macos":
		return `#!/bin/bash
echo "Starting macOS configuration at $(date)"
softwareupdate --install --all
echo "Configuration complete at $(date)"
`
	default:
		return ""
	}
}

// ValidateStorageConfig validates storage configuration
func ValidateStorageConfig(t *testing.T, ctx *types.ValidationContext, volumeSize int, volumeType string, hasAdditionalVolumes bool) {
	require.NotEmpty(t, ctx.InstanceIDs, "Instance IDs should not be empty")

	// Load AWS SDK v2 config with region
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(DefaultRegion),
	)
	require.NoError(t, err)

	ec2Svc := ec2.NewFromConfig(cfg)

	// Describe volumes attached to the first instance
	describeResult, err := ec2Svc.DescribeVolumes(context.Background(), &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("attachment.instance-id"),
				Values: []string{ctx.InstanceIDs[0]},
			},
		},
	})
	require.NoError(t, err)

	// Verify root volume configuration
	assert.GreaterOrEqual(t, len(describeResult.Volumes), 1, "Instance should have at least one volume")

	var rootVolume *ec2types.Volume
	for i := range describeResult.Volumes {
		vol := &describeResult.Volumes[i]
		for _, attachment := range vol.Attachments {
			if aws.ToString(attachment.Device) == "/dev/sda1" || aws.ToString(attachment.Device) == "/dev/xvda" {
				rootVolume = vol
				break
			}
		}
		if rootVolume != nil {
			break
		}
	}

	require.NotNil(t, rootVolume, "Root volume not found")

	// In v2, Size is *int32 and VolumeType is an enum (ec2types.VolumeType)
	assert.Equal(t, int32(volumeSize), aws.ToInt32(rootVolume.Size), "Root volume size doesn't match")
	assert.Equal(t, volumeType, string(rootVolume.VolumeType), "Root volume type doesn't match")

	// Verify additional volumes if applicable
	if hasAdditionalVolumes {
		expectedDevices := []string{"/dev/sdb", "/dev/xvdb", "/dev/sdc", "/dev/xvdc"}
		foundAdditionalVolumes := 0

		for _, volume := range describeResult.Volumes {
			for _, attachment := range volume.Attachments {
				deviceName := aws.ToString(attachment.Device)
				for _, expectedDevice := range expectedDevices {
					if deviceName == expectedDevice {
						foundAdditionalVolumes++
						assert.Equal(t, volumeType, string(volume.VolumeType),
							"Additional volume type doesn't match for device %s", deviceName)
					}
				}
			}
		}

		assert.GreaterOrEqual(t, foundAdditionalVolumes, 1,
			"Should find at least one additional volume")
	}
}
