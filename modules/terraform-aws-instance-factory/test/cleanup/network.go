package cleanup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
)

// CleanupSubnet deletes a subnet and its associated resources from AWS.
// It first detaches and removes all network interfaces in the subnet,
// then attempts to delete the subnet itself with retries.
func CleanupSubnet(t *testing.T, ec2Client *ec2.Client, subnetID string) {
	ctx := context.Background()

	enis, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("subnet-id"),
				Values: []string{subnetID},
			},
		},
	})
	if err != nil {
		t.Logf("Warning: Error describing network interfaces: %v", err)
		return
	}

	for _, eni := range enis.NetworkInterfaces {
		if eni.Attachment != nil && eni.Attachment.DeleteOnTermination != nil && !*eni.Attachment.DeleteOnTermination {
			if eni.Status != ec2types.NetworkInterfaceStatusAvailable {
				_, err = ec2Client.DetachNetworkInterface(ctx, &ec2.DetachNetworkInterfaceInput{
					AttachmentId: eni.Attachment.AttachmentId,
					Force:        aws.Bool(true),
				})
				if err != nil {
					if eni.NetworkInterfaceId != nil {
						t.Logf("Warning: Error detaching ENI %s: %v", *eni.NetworkInterfaceId, err)
					} else {
						t.Logf("Warning: Error detaching ENI (no id): %v", err)
					}
					continue
				}
				if eni.NetworkInterfaceId != nil {
					err = waitForENIDetachment(ec2Client, *eni.NetworkInterfaceId)
					if err != nil {
						t.Logf("Warning: Error waiting for ENI detachment: %v", err)
						continue
					}
				}
			}
		}

		if eni.NetworkInterfaceId != nil {
			_, err = ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
				NetworkInterfaceId: eni.NetworkInterfaceId,
			})
			if err != nil {
				t.Logf("Warning: Error deleting ENI %s: %v", aws.ToString(eni.NetworkInterfaceId), err)
			}
		}
	}

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		_, err = ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
			SubnetId: aws.String(subnetID),
		})
		if err == nil {
			t.Logf("Successfully deleted subnet %s", subnetID)
			return
		}
		if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "DependencyViolation" {
			t.Logf("Subnet %s still has dependencies, retrying in 10 seconds...", subnetID)
			time.Sleep(10 * time.Second)
			continue
		}
		t.Logf("Error deleting subnet %s: %v", subnetID, err)
		break
	}
}

// NetworkResources handles the cleanup of all network resources associated
// with an EKS cluster. This includes VPC endpoints, NAT gateways, subnet
// tags, and the subnets themselves.
func NetworkResources(t *testing.T, ec2Client *ec2.Client, networkConfig types.NetworkConfig) {
	ctx := context.Background()
	clusterName := fmt.Sprintf("%s-%s", types.EnvName, "eks")

	// First clean up VPC endpoints
	cleanupVPCEndpoints(t, ec2Client, networkConfig.VPCID)

	// Clean up NAT Gateways
	natGateways, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{networkConfig.VPCID},
			},
		},
	})
	if err != nil {
		t.Logf("Warning: Failed to describe NAT Gateways during cleanup: %v", err)
		return
	}

	for _, natGw := range natGateways.NatGateways {
		if natGw.State != ec2types.NatGatewayStateDeleted {
			_, err = ec2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
				NatGatewayId: natGw.NatGatewayId,
			})
			if err != nil {
				t.Logf("Warning: Failed to delete NAT Gateway %s: %v", aws.ToString(natGw.NatGatewayId), err)
				continue
			}

			if natGw.NatGatewayId != nil {
				if err := waitForNatGatewayDeletion(ec2Client, *natGw.NatGatewayId); err != nil {
					t.Logf("Warning: Error waiting for NAT Gateway deletion: %v", err)
				}
			}

			if len(natGw.NatGatewayAddresses) > 0 {
				allocationID := natGw.NatGatewayAddresses[0].AllocationId
				if allocationID != nil {
					_, err = ec2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
						AllocationId: allocationID,
					})
					if err != nil {
						t.Logf("Warning: Failed to release Elastic IP: %v", err)
					}
				}
			}
		}
	}

	// Clean up subnet tags
	for _, subnetID := range append(networkConfig.PrivateSubnetIDs, networkConfig.PublicSubnetIDs...) {
		_, err = ec2Client.DeleteTags(ctx, &ec2.DeleteTagsInput{
			Resources: []string{subnetID},
			Tags: []ec2types.Tag{
				{Key: aws.String("kubernetes.io/cluster/" + clusterName)},
				{Key: aws.String("kubernetes.io/role/elb")},
				{Key: aws.String("kubernetes.io/role/internal-elb")},
			},
		})
		if err != nil {
			t.Logf("Warning: Failed to clean up subnet tags: %v", err)
		}
	}

	for _, subnetID := range append(networkConfig.PrivateSubnetIDs, networkConfig.PublicSubnetIDs...) {
		CleanupSubnet(t, ec2Client, subnetID)
	}
}

// CleanupRouteTableAssociations removes route table associations from AWS.
func CleanupRouteTableAssociations(t *testing.T, ec2Client *ec2.Client, associationIDs []string) {
	ctx := context.Background()

	for _, assocID := range associationIDs {
		t.Logf("Disassociating route table association %s...", assocID)
		_, err := ec2Client.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{
			AssociationId: aws.String(assocID),
		})
		if err != nil {
			t.Logf("Error disassociating route table association %s: %v", assocID, err)
		} else {
			t.Logf("Successfully disassociated route table association %s", assocID)
		}
		time.Sleep(5 * time.Second)
	}
}

// CleanupRouteTable removes a route table and its associations from AWS.
func CleanupRouteTable(t *testing.T, ec2Client *ec2.Client, rtID string) {
	ctx := context.Background()

	rtAssocs, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{rtID},
	})
	if err == nil && len(rtAssocs.RouteTables) > 0 {
		var associations []string
		for _, rt := range rtAssocs.RouteTables[0].Associations {
			if rt.RouteTableAssociationId != nil {
				associations = append(associations, *rt.RouteTableAssociationId)
			}
		}
		CleanupRouteTableAssociations(t, ec2Client, associations)
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		t.Logf("Attempting to delete route table %s (attempt %d/%d)...", rtID, i+1, maxRetries)
		_, err := ec2Client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
			RouteTableId: aws.String(rtID),
		})
		if err == nil {
			t.Logf("Successfully deleted route table %s", rtID)
			break
		}
		if i < maxRetries-1 {
			t.Logf("Error deleting route table %s: %v. Retrying in 10 seconds...", rtID, err)
			time.Sleep(10 * time.Second)
		} else {
			t.Logf("Final error deleting route table %s: %v", rtID, err)
		}
	}
}

// WaitForSubnetDeletion polls until a subnet is confirmed deleted.
func WaitForSubnetDeletion(t *testing.T, ec2Client *ec2.Client, subnetID string) {
	ctx := context.Background()

	for {
		_, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		})
		if err != nil {
			if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "InvalidSubnetID.NotFound" {
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
}

// CleanupSubnets removes multiple subnets from AWS.
func CleanupSubnets(t *testing.T, ec2Client *ec2.Client, subnetIDs []string) {
	ctx := context.Background()

	for _, subnetID := range subnetIDs {
		t.Logf("Deleting subnet %s...", subnetID)
		_, err := ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
			SubnetId: aws.String(subnetID),
		})
		if err != nil {
			t.Logf("Error deleting subnet %s: %v", subnetID, err)
		} else {
			t.Logf("Successfully deleted subnet %s", subnetID)
		}
		WaitForSubnetDeletion(t, ec2Client, subnetID)
	}
}

// CleanupInternetGateway detaches and deletes an Internet Gateway (terratest-tagged).
func CleanupInternetGateway(t *testing.T, ec2Client *ec2.Client, vpcID string) {
	ctx := context.Background()

	igwResult, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{"terratest-igw"},
			},
		},
	})
	if err == nil && len(igwResult.InternetGateways) > 0 {
		for _, igw := range igwResult.InternetGateways {
			// Detach first
			if len(igw.Attachments) > 0 && igw.InternetGatewayId != nil {
				_, err = ec2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
					InternetGatewayId: igw.InternetGatewayId,
					VpcId:             aws.String(vpcID),
				})
				if err != nil {
					t.Logf("Warning: Error detaching IGW %s: %v", aws.ToString(igw.InternetGatewayId), err)
				}
				time.Sleep(5 * time.Second)
			}

			// Delete IGW
			if igw.InternetGatewayId != nil {
				_, err = ec2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
					InternetGatewayId: igw.InternetGatewayId,
				})
				if err != nil {
					t.Logf("Warning: Error deleting IGW %s: %v", aws.ToString(igw.InternetGatewayId), err)
				}
			}
		}
	}
}

// CleanupPublicSubnetResources removes all resources associated with public subnets.
func CleanupPublicSubnetResources(t *testing.T, ec2Client *ec2.Client, vpcID string, createdSubnets []string, routeTableAssociations []string) {
	// First disassociate route table associations
	CleanupRouteTableAssociations(t, ec2Client, routeTableAssociations)

	// Then delete subnets
	CleanupSubnets(t, ec2Client, createdSubnets)

	// Clean up route tables with terratest-public tag
	ctx := context.Background()
	rtResult, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{"terratest-public"},
			},
		},
	})
	if err == nil {
		for _, rt := range rtResult.RouteTables {
			if rt.RouteTableId != nil {
				CleanupRouteTable(t, ec2Client, *rt.RouteTableId)
			}
		}
	}

	// Clean up IGW
	CleanupInternetGateway(t, ec2Client, vpcID)
}

// CleanupPrivateSubnetResources removes resources for private subnets.
func CleanupPrivateSubnetResources(t *testing.T, ec2Client *ec2.Client, createdSubnets []string, routeTableAssociations []string, rtID string) {
	// First disassociate route table associations
	CleanupRouteTableAssociations(t, ec2Client, routeTableAssociations)

	// Then delete subnets
	CleanupSubnets(t, ec2Client, createdSubnets)

	// Clean up the route table
	CleanupRouteTable(t, ec2Client, rtID)
}

func waitForVPCEndpointDeletion(ec2Client *ec2.Client, vpceID string) error {
	ctx := context.Background()

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		result, err := ec2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
			VpcEndpointIds: []string{vpceID},
		})
		if err != nil {
			if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "InvalidVpcEndpointId.NotFound" {
				return nil
			}
			return err
		}
		if len(result.VpcEndpoints) == 0 {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return nil
}

func waitForENIDetachment(ec2Client *ec2.Client, eniID string) error {
	ctx := context.Background()

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		result, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			NetworkInterfaceIds: []string{eniID},
		})
		if err != nil {
			return err
		}
		if len(result.NetworkInterfaces) == 0 {
			return nil
		}
		if result.NetworkInterfaces[0].Status == ec2types.NetworkInterfaceStatusAvailable {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timeout waiting for ENI detachment")
}

func cleanupVPCEndpoints(t *testing.T, ec2Client *ec2.Client, vpcID string) {
	ctx := context.Background()

	// List all VPC endpoints in the VPC that were created by terratest
	result, err := ec2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   aws.String("tag:CreatedBy"),
				Values: []string{"terratest"},
			},
		},
	})
	if err != nil {
		t.Logf("Warning: Failed to describe VPC endpoints: %v", err)
		return
	}

	var vpceIDs []string
	for _, vpce := range result.VpcEndpoints {
		if vpce.VpcEndpointId != nil {
			vpceIDs = append(vpceIDs, *vpce.VpcEndpointId)
			t.Logf("Found VPC endpoint to delete: %s", *vpce.VpcEndpointId)
		}
	}

	if len(vpceIDs) > 0 {
		t.Logf("Attempting to delete %d VPC endpoints", len(vpceIDs))
		_, err = ec2Client.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
			VpcEndpointIds: vpceIDs,
		})
		if err != nil {
			t.Logf("Warning: Failed to delete VPC endpoints: %v", err)
			return
		}

		for _, id := range vpceIDs {
			t.Logf("Waiting for VPC endpoint %s to be deleted...", id)
			if err := waitForVPCEndpointDeletion(ec2Client, id); err != nil {
				t.Logf("Warning: Error waiting for VPC endpoint %s deletion: %v", id, err)
			} else {
				t.Logf("Successfully deleted VPC endpoint %s", id)
			}
		}
	} else {
		t.Log("No VPC endpoints found to clean up")
	}
}

func waitForNatGatewayDeletion(ec2Client *ec2.Client, natGatewayID string) error {
	ctx := context.Background()

	maxRetries := 60
	for i := 0; i < maxRetries; i++ {
		out, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
			NatGatewayIds: []string{natGatewayID},
		})
		if err != nil {
			// If the NAT GW no longer exists
			if apiErr, ok := err.(smithy.APIError); ok && apiErr.ErrorCode() == "NatGatewayNotFound" {
				return nil
			}
			return err
		}
		if len(out.NatGateways) == 0 || out.NatGateways[0].State == ec2types.NatGatewayStateDeleted {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timeout waiting for NAT Gateway %s deletion", natGatewayID)
}
