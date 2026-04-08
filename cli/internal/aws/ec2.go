package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// Instance represents a discovered EC2 instance.
type Instance struct {
	InstanceID string
	Name       string
	PrivateIP  string
	State      string
}

// DiscoverInstances finds GOAD instances by tag pattern.
// By default only running instances are returned. Pass additional states
// (e.g. "stopped") to include them.
func (c *Client) DiscoverInstances(ctx context.Context, env string, extraStates ...string) ([]Instance, error) {
	pattern := fmt.Sprintf("*%s*dreadgoad*", env)
	states := []string{"running"}
	states = append(states, extraStates...)
	out, err := c.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{Name: Ptr("tag:Name"), Values: []string{pattern}},
			{Name: Ptr("instance-state-name"), Values: states},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	var instances []Instance
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			inst := Instance{
				InstanceID: deref(i.InstanceId),
				PrivateIP:  deref(i.PrivateIpAddress),
				State:      string(i.State.Name),
			}
			for _, t := range i.Tags {
				if deref(t.Key) == "Name" {
					inst.Name = deref(t.Value)
				}
			}
			instances = append(instances, inst)
		}
	}
	return instances, nil
}

// GetInstancePrivateIPs queries EC2 for private IPs of the given instance IDs.
func (c *Client) GetInstancePrivateIPs(ctx context.Context, instanceIDs []string) (map[string]string, error) {
	out, err := c.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	mapping := make(map[string]string)
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			mapping[deref(i.InstanceId)] = deref(i.PrivateIpAddress)
		}
	}
	return mapping, nil
}

// StartInstances starts the given EC2 instances.
func (c *Client) StartInstances(ctx context.Context, instanceIDs []string) error {
	_, err := c.EC2.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: instanceIDs,
	})
	return err
}

// StopInstances stops the given EC2 instances.
func (c *Client) StopInstances(ctx context.Context, instanceIDs []string) error {
	_, err := c.EC2.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: instanceIDs,
	})
	return err
}

// DiscoverAllInstances finds GOAD instances in any state (including stopped).
func (c *Client) DiscoverAllInstances(ctx context.Context, env string) ([]Instance, error) {
	pattern := fmt.Sprintf("*%s*dreadgoad*", env)
	out, err := c.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{Name: Ptr("tag:Name"), Values: []string{pattern}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	var instances []Instance
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			if i.State.Name == types.InstanceStateNameTerminated {
				continue
			}
			inst := Instance{
				InstanceID: deref(i.InstanceId),
				PrivateIP:  deref(i.PrivateIpAddress),
				State:      string(i.State.Name),
			}
			for _, t := range i.Tags {
				if deref(t.Key) == "Name" {
					inst.Name = deref(t.Value)
				}
			}
			instances = append(instances, inst)
		}
	}
	return instances, nil
}

// FindInstanceByHostnameAll finds an instance (any state except terminated) whose Name tag contains the hostname.
func (c *Client) FindInstanceByHostnameAll(ctx context.Context, env, hostname string) (*Instance, error) {
	instances, err := c.DiscoverAllInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	hostname = strings.ToUpper(hostname)
	for _, inst := range instances {
		if strings.Contains(strings.ToUpper(inst.Name), hostname) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance not found for hostname %s", hostname)
}

// WaitForInstanceStopped polls until the given instance reaches the "stopped" state.
func (c *Client) WaitForInstanceStopped(ctx context.Context, instanceID string) error {
	waiter := ec2.NewInstanceStoppedWaiter(c.EC2)
	return waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 5*time.Minute)
}

// TerminateInstances terminates the given EC2 instances.
func (c *Client) TerminateInstances(ctx context.Context, instanceIDs []string) error {
	_, err := c.EC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	})
	return err
}

// FindInstanceByHostname finds an instance whose Name tag contains the hostname.
func (c *Client) FindInstanceByHostname(ctx context.Context, env, hostname string) (*Instance, error) {
	instances, err := c.DiscoverInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	hostname = strings.ToUpper(hostname)
	for _, inst := range instances {
		if strings.Contains(strings.ToUpper(inst.Name), hostname) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance not found for hostname %s", hostname)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
