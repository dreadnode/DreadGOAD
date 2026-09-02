package aws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// Client wraps AWS SDK clients for EC2, SSM, STS, and S3.
type Client struct {
	EC2    *ec2.Client
	SSM    *ssm.Client
	STS    *sts.Client
	S3     *s3.Client
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

	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
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
		S3:     s3.NewFromConfig(cfg),
		Region: region,
	}
	clients[key] = c
	return c, nil
}

// EnsureSSMBucket creates the S3 bucket the Ansible SSM connection plugin
// needs to transfer files, if it does not already exist. Idempotent.
func (c *Client) EnsureSSMBucket(ctx context.Context, bucket string) error {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFound" {
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}

	input := &s3.CreateBucketInput{Bucket: &bucket}
	// us-east-1 is the S3 default; specifying it as a LocationConstraint is an error.
	if c.Region != "" && c.Region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(c.Region),
		}
	}
	if _, err := c.S3.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("create bucket %s: %w", bucket, err)
	}
	slog.Info("created SSM transfer bucket", "bucket", bucket, "region", c.Region)
	return nil
}

// DeleteSSMBucket empties and deletes the S3 bucket used by the Ansible SSM
// connection plugin. Returns nil if the bucket does not exist.
func (c *Client) DeleteSSMBucket(ctx context.Context, bucket string) error {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return nil
		}
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}

	// S3 requires a bucket to be empty before deletion.
	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{Bucket: &bucket})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects in %s: %w", bucket, err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]s3types.ObjectIdentifier, len(page.Contents))
		for i, obj := range page.Contents {
			objects[i] = s3types.ObjectIdentifier{Key: obj.Key}
		}
		_, err = c.S3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &bucket,
			Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("delete objects in %s: %w", bucket, err)
		}
	}

	if _, err := c.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket}); err != nil {
		return fmt.Errorf("delete bucket %s: %w", bucket, err)
	}
	slog.Info("deleted SSM transfer bucket", "bucket", bucket)
	return nil
}

// Ptr returns a pointer to the given string (helper for AWS SDK).
func Ptr(s string) *string {
	return aws.String(s)
}
