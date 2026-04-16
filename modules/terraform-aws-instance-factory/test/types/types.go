package types

import (
	"sync"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
)

const (
	// AwsRegion is the default AWS region for running terratests
	AwsRegion = "us-east-2"
	// EnvName is the environment name used for testing
	EnvName = "tt"
)

// CleanupTracker is used to track cleanup operations
type CleanupTracker struct {
	Mutex    sync.Mutex
	Complete bool
}

// NetworkConfig holds VPC and subnet information
type NetworkConfig struct {
	VPCID            string
	PrivateSubnetIDs []string
	PublicSubnetIDs  []string
	VPCCIDR          string
}

// EC2InstanceConfig holds EC2 instance configuration details
type EC2InstanceConfig struct {
	InstanceType       string
	OSType             string
	VolumeSize         int
	VolumeType         string
	EnableASG          bool
	ASGMinSize         int
	ASGMaxSize         int
	ASGDesiredCapacity int
	AdditionalVolumes  []EBSVolumeConfig
	UniqueIdentifier   string
}

// EBSVolumeConfig holds EBS volume configuration
type EBSVolumeConfig struct {
	DeviceName          string
	VolumeSize          int
	VolumeType          string
	DeleteOnTermination bool
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	IngressRules             []SecurityGroupRule
	EgressRules              []SecurityGroupRule
	AdditionalSecurityGroups []string
	SSHPublicKey             string
}

// SecurityGroupRule represents a security group rule
type SecurityGroupRule struct {
	Description string
	FromPort    int
	ToPort      int
	Protocol    string
	CIDRBlocks  []string
}

// RetryConfig holds configuration information used for retry attempts
type RetryConfig struct {
	Delay       time.Duration
	Description string
	MaxRetries  int
}

// ValidationContext holds common validation context for all tests
type ValidationContext struct {
	T                *testing.T
	TerraformOptions *terraform.Options
	SecurityGroupID  string
	InstanceIDs      []string
	InstanceDetails  map[string]interface{} // was map[string]string; switched to interface{} to match terraform.OutputMapE
	ASGName          string
	StorageConfig    *StorageValidationConfig
	LaunchTemplateID string
	ASGDetails       map[string]interface{}
}

// StorageValidationConfig contains storage validation expectations
type StorageValidationConfig struct {
	VolumeSize               int
	VolumeType               string
	IncludeAdditionalVolumes bool
}

// ResourceValidation defines an interface for resource validation
type ResourceValidation interface {
	Validate(ctx *ValidationContext) error
}

// RetryFunc returns an error if the operation should be retried
type RetryFunc func() error

// VPCResources holds VPC resource information
type VPCResources struct {
	Subnets                []string
	RouteTableAssociations []string
	RouteTableID           string
}
