package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Identity holds the result of an STS GetCallerIdentity call.
type Identity struct {
	Account string
	ARN     string
}

// VerifyCredentials validates that AWS credentials are configured and working.
func (c *Client) VerifyCredentials(ctx context.Context) (*Identity, error) {
	out, err := c.STS.GetCallerIdentity(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("AWS credentials invalid or not configured: %w", err)
	}
	return &Identity{
		Account: deref(out.Account),
		ARN:     deref(out.Arn),
	}, nil
}

// SSMStatus holds the SSM agent ping status for an instance.
type SSMStatus struct {
	InstanceID string
	PingStatus string // "Online", "ConnectionLost", etc.
}

// CheckSSMStatus queries SSM DescribeInstanceInformation for the given instance IDs
// and returns their ping status. Instances not managed by SSM are reported as "NotManaged".
func (c *Client) CheckSSMStatus(ctx context.Context, instanceIDs []string) ([]SSMStatus, error) {
	if len(instanceIDs) == 0 {
		return nil, nil
	}

	out, err := c.SSM.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
		Filters: []ssmtypes.InstanceInformationStringFilter{
			{Key: Ptr("InstanceIds"), Values: instanceIDs},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instance information: %w", err)
	}

	managed := make(map[string]string, len(out.InstanceInformationList))
	for _, info := range out.InstanceInformationList {
		managed[deref(info.InstanceId)] = string(info.PingStatus)
	}

	statuses := make([]SSMStatus, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		status := "NotManaged"
		if s, ok := managed[id]; ok {
			status = s
		}
		statuses = append(statuses, SSMStatus{InstanceID: id, PingStatus: status})
	}
	return statuses, nil
}
