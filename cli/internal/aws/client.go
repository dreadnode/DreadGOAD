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

// NewClient creates or returns a cached AWS client for the given region.
func NewClient(ctx context.Context, region string) (*Client, error) {
	mu.Lock()
	defer mu.Unlock()

	if c, ok := clients[region]; ok {
		return c, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config for %s: %w", region, err)
	}

	c := &Client{
		EC2:    ec2.NewFromConfig(cfg),
		SSM:    ssm.NewFromConfig(cfg),
		STS:    sts.NewFromConfig(cfg),
		Region: region,
	}
	clients[region] = c
	return c, nil
}

// Ptr returns a pointer to the given string (helper for AWS SDK).
func Ptr(s string) *string {
	return aws.String(s)
}
