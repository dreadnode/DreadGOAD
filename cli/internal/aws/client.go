package aws

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Client wraps AWS SDK clients for EC2, SSM, and STS.
type Client struct {
	EC2    *ec2.Client
	SSM    *ssm.Client
	STS    *sts.Client
	Region string
}

var (
	clients = make(map[string]*Client)
	mu      sync.Mutex
)

// NewClient creates or returns a cached AWS client for the given region and
// optional profile. Pass an empty profile to use the SDK default chain.
func NewClient(ctx context.Context, region, profile string) (*Client, error) {
	mu.Lock()
	defer mu.Unlock()

	key := region + "\x00" + profile
	if c, ok := clients[key]; ok {
		return c, nil
	}

	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		if profile != "" {
			return nil, fmt.Errorf("load AWS config for region=%s profile=%s: %w", region, profile, err)
		}
		return nil, fmt.Errorf("load AWS config for region=%s: %w", region, err)
	}

	c := &Client{
		EC2:    ec2.NewFromConfig(cfg),
		SSM:    ssm.NewFromConfig(cfg),
		STS:    sts.NewFromConfig(cfg),
		Region: region,
	}
	clients[key] = c
	return c, nil
}

// Ptr returns a pointer to the given string (helper for AWS SDK).
func Ptr(s string) *string {
	return aws.String(s)
}
