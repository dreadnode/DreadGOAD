package helpers

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/cleanup"
	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
	"github.com/stretchr/testify/require"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// GetPublicSubnetsInDifferentAZs retrieves public subnets in different AZs
func GetPublicSubnetsInDifferentAZs(t *testing.T, vpcID string, count int) []string {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(types.AwsRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	// Get all available AZs and create a map for validation
	azResult, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("region-name"), Values: []string{types.AwsRegion}},
		},
	})
	require.NoError(t, err)

	// Create a map of valid AZs
	validAZs := make(map[string]bool)
	for _, az := range azResult.AvailabilityZones {
		if az.ZoneName != nil {
			validAZs[*az.ZoneName] = true
		}
	}

	// Create a map of AZ to subnets
	subnetsByAZ := make(map[string][]string)

	// Get all subnets in the VPC
	result, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	require.NoError(t, err)

	// Get the main route table
	mainRT, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("association.main"), Values: []string{"true"}},
		},
	})
	require.NoError(t, err)

	var mainRouteTableID string
	if len(mainRT.RouteTables) > 0 && mainRT.RouteTables[0].RouteTableId != nil {
		mainRouteTableID = *mainRT.RouteTables[0].RouteTableId
	}

	// Process each subnet
	for _, subnet := range result.Subnets {
		if subnet.AvailabilityZone == nil || subnet.SubnetId == nil {
			continue
		}

		// Skip subnets in non-available AZs
		if !validAZs[*subnet.AvailabilityZone] {
			t.Logf("Skipping subnet %s in non-available AZ %s", *subnet.SubnetId, *subnet.AvailabilityZone)
			continue
		}

		// Get the route table for this subnet
		var routeTable *ec2types.RouteTable

		rtResult, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("association.subnet-id"), Values: []string{aws.ToString(subnet.SubnetId)}},
			},
		})
		if err != nil {
			t.Logf("Warning: Error getting route tables for subnet %s: %v", aws.ToString(subnet.SubnetId), err)
			continue
		}

		if len(rtResult.RouteTables) > 0 {
			rt := rtResult.RouteTables[0]
			routeTable = &rt
		} else if mainRouteTableID != "" {
			// If no explicit association, check main route table
			mainRTResult, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
				RouteTableIds: []string{mainRouteTableID},
			})
			if err == nil && len(mainRTResult.RouteTables) > 0 {
				rt := mainRTResult.RouteTables[0]
				routeTable = &rt
			}
		}

		// Check if the subnet is public (has route to IGW)
		if routeTable != nil {
			isPublic := false
			for _, route := range routeTable.Routes {
				if route.GatewayId != nil && strings.HasPrefix(*route.GatewayId, "igw-") {
					isPublic = true
					break
				}
			}
			if isPublic {
				subnetsByAZ[*subnet.AvailabilityZone] = append(subnetsByAZ[*subnet.AvailabilityZone], *subnet.SubnetId)
				t.Logf("Found public subnet %s in AZ %s", *subnet.SubnetId, *subnet.AvailabilityZone)
			}
		}
	}

	// Select subnets from different AZs
	var selectedSubnets []string
	usedAZs := make(map[string]bool)

	// Use the sorted list of AZs from the describe call
	for _, az := range azResult.AvailabilityZones {
		if len(selectedSubnets) >= count {
			break
		}
		if az.ZoneName == nil {
			continue
		}
		azName := *az.ZoneName
		if subnets := subnetsByAZ[azName]; len(subnets) > 0 && !usedAZs[azName] {
			selectedSubnets = append(selectedSubnets, subnets[0])
			usedAZs[azName] = true
			t.Logf("Selected subnet %s from AZ %s", subnets[0], azName)
		}
	}

	// Log the selection process
	t.Logf("Found %d AZs with public subnets:", len(subnetsByAZ))
	for az, subnets := range subnetsByAZ {
		t.Logf("  AZ %s: %d subnets %v", az, len(subnets), subnets)
	}
	t.Logf("Selected %d subnets in different AZs: %v", len(selectedSubnets), selectedSubnets)

	// If we don't have enough subnets, create new ones with cleanup
	if len(selectedSubnets) < count {
		t.Logf("Insufficient public subnets, creating %d new ones", count)
		createdSubnets, resources := createPublicSubnets(t, vpcID, count)

		if strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true" {
			t.Cleanup(func() {
				cleanup.CleanupPublicSubnetResources(t, ec2Client, vpcID, resources.Subnets, resources.RouteTableAssociations)
			})
		}

		return createdSubnets
	}

	selectedAZs := make(map[string]bool)
	for _, subnetID := range selectedSubnets {
		subnet, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		})
		if err != nil {
			t.Fatalf("Failed to describe subnet %s: %v", subnetID, err)
		}

		az := aws.ToString(subnet.Subnets[0].AvailabilityZone)
		if selectedAZs[az] {
			t.Fatalf("Multiple subnets selected from the same AZ %s", az)
		}
		selectedAZs[az] = true
	}

	// Trim to exactly the number requested
	return selectedSubnets[:count]
}

// createPublicSubnets creates public subnets in the specified VPC
func createPublicSubnets(t *testing.T, vpcID string, count int) ([]string, types.VPCResources) {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(types.AwsRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	// Get VPC CIDR
	vpcResult, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, vpcResult.Vpcs)
	vpcCIDR := aws.ToString(vpcResult.Vpcs[0].CidrBlock)

	// Create or get Internet Gateway
	var igwID string
	igws, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}},
		},
	})
	require.NoError(t, err)

	if len(igws.InternetGateways) == 0 {
		// Create new IGW
		igwResult, err := ec2Client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeInternetGateway,
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String("terratest-igw")},
					},
				},
			},
		})
		require.NoError(t, err)
		igwID = aws.ToString(igwResult.InternetGateway.InternetGatewayId)

		// Attach IGW to VPC
		_, err = ec2Client.AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{
			InternetGatewayId: aws.String(igwID),
			VpcId:             aws.String(vpcID),
		})
		require.NoError(t, err)
	} else {
		igwID = aws.ToString(igws.InternetGateways[0].InternetGatewayId)
	}

	// Create public route table
	rtResult, err := ec2Client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeRouteTable,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String("terratest-public")},
				},
			},
		},
	})
	require.NoError(t, err)
	rtID := aws.ToString(rtResult.RouteTable.RouteTableId)

	// Add route to Internet Gateway
	_, err = ec2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
		RouteTableId:         aws.String(rtID),
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            aws.String(igwID),
	})
	require.NoError(t, err)

	// Get available AZs
	azResult, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("region-name"), Values: []string{types.AwsRegion}},
		},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(azResult.AvailabilityZones), count)

	var createdSubnets []string
	var routeTableAssociations []string
	var resources types.VPCResources

	// Create subnets across multiple AZs
	for i := 0; i < count; i++ {
		az := aws.ToString(azResult.AvailabilityZones[i].ZoneName)

		// Find next available CIDR block
		cidr, err := findNextAvailableCIDR(ctx, ec2Client, vpcID, vpcCIDR)
		require.NoError(t, err)

		result, err := ec2Client.CreateSubnet(ctx, &ec2.CreateSubnetInput{
			VpcId:            aws.String(vpcID),
			CidrBlock:        aws.String(cidr),
			AvailabilityZone: aws.String(az),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeSubnet,
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String(fmt.Sprintf("terratest-public-%d", i+1))},
					},
				},
			},
		})
		require.NoError(t, err)
		subnetID := aws.ToString(result.Subnet.SubnetId)

		// Enable auto-assign public IP
		_, err = ec2Client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
			SubnetId:            aws.String(subnetID),
			MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{Value: aws.Bool(true)},
		})
		require.NoError(t, err)

		// Associate with public route table
		assocResult, err := ec2Client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
			SubnetId:     aws.String(subnetID),
			RouteTableId: aws.String(rtID),
		})
		require.NoError(t, err)

		createdSubnets = append(createdSubnets, subnetID)
		routeTableAssociations = append(routeTableAssociations, aws.ToString(assocResult.AssociationId))

		// Wait for subnet to be available (v2 waiter)
		waiter := ec2.NewSubnetAvailableWaiter(ec2Client)
		err = waiter.Wait(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		}, 5*time.Minute)
		require.NoError(t, err)
	}

	resources.Subnets = createdSubnets
	resources.RouteTableAssociations = routeTableAssociations
	resources.RouteTableID = rtID

	return createdSubnets, resources
}

// findNextAvailableCIDR finds the next available CIDR block in the VPC that doesn't conflict with existing subnets
func findNextAvailableCIDR(ctx context.Context, ec2Client *ec2.Client, vpcID string, vpcCIDR string) (string, error) {
	// Get all existing subnets in the VPC
	result, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("error describing subnets: %v", err)
	}

	// Parse VPC CIDR
	_, vpcNet, err := net.ParseCIDR(vpcCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid VPC CIDR: %v", err)
	}

	// Create map of used CIDRs and their second/third octets
	usedCIDRs := make(map[string]bool)
	usedOctets := make(map[string]bool)
	for _, subnet := range result.Subnets {
		if subnet.CidrBlock == nil {
			continue
		}
		usedCIDRs[*subnet.CidrBlock] = true

		// Extract second and third octets from the CIDR
		parts := strings.Split(*subnet.CidrBlock, ".")
		if len(parts) >= 4 {
			octetKey := fmt.Sprintf("%s.%s", parts[1], parts[2])
			usedOctets[octetKey] = true
		}
	}

	// Try different combinations of second and third octets
	for secondOctet := 16; secondOctet < 32; secondOctet++ {
		for thirdOctet := 0; thirdOctet < 256; thirdOctet++ {
			octetKey := fmt.Sprintf("%d.%d", secondOctet, thirdOctet)
			if usedOctets[octetKey] {
				continue
			}

			candidateCIDR := fmt.Sprintf("172.%d.%d.0/24", secondOctet, thirdOctet)
			_, candidateNet, err := net.ParseCIDR(candidateCIDR)
			if err != nil {
				continue
			}

			// Check if candidate CIDR is within VPC CIDR
			if !vpcNet.Contains(candidateNet.IP) {
				continue
			}

			// Check if this CIDR is already in use
			if !usedCIDRs[candidateCIDR] {
				// Double check for overlaps
				hasOverlap := false
				for existingCIDR := range usedCIDRs {
					_, existingNet, err := net.ParseCIDR(existingCIDR)
					if err != nil {
						continue
					}
					if cidrsOverlap(candidateNet, existingNet) {
						hasOverlap = true
						break
					}
				}
				if !hasOverlap {
					usedOctets[octetKey] = true // Mark these octets as used
					return candidateCIDR, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no available /24 CIDR blocks found in VPC CIDR %s", vpcCIDR)
}

// cidrsOverlap checks if two CIDR ranges overlap
func cidrsOverlap(cidr1, cidr2 *net.IPNet) bool {
	return cidr1.Contains(cidr2.IP) || cidr2.Contains(cidr1.IP)
}

// EnsurePublicSubnetRoutes ensures that the public subnets have routes to the Internet Gateway
func EnsurePublicSubnetRoutes(t *testing.T, ec2Client *ec2.Client, vpcID string, subnetIDs []string, igwID string) {
	ctx := context.Background()

	routeTables, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	require.NoError(t, err)

	for _, subnetID := range subnetIDs {
		// ✅ Pass routeTables directly (it's already *ec2.DescribeRouteTablesOutput)
		routeTableID := getRouteTableIDForSubnet(routeTables, subnetID)
		if routeTableID == "" {
			createResult, err := ec2Client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
				VpcId: aws.String(vpcID),
			})
			require.NoError(t, err)
			routeTableID = aws.ToString(createResult.RouteTable.RouteTableId)

			_, err = ec2Client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
				RouteTableId: aws.String(routeTableID),
				SubnetId:     aws.String(subnetID),
			})
			require.NoError(t, err)
		}

		_, err = ec2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
			RouteTableId:         aws.String(routeTableID),
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			GatewayId:            aws.String(igwID),
		})
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "routealreadyexists") {
			require.NoError(t, err)
		}
	}
}

// getRouteTableIDForSubnet returns the route table ID associated with a subnet
func getRouteTableIDForSubnet(routeTables *ec2.DescribeRouteTablesOutput, subnetID string) string {
	for _, rt := range routeTables.RouteTables {
		for _, assoc := range rt.Associations {
			if assoc.SubnetId != nil && *assoc.SubnetId == subnetID {
				return aws.ToString(rt.RouteTableId)
			}
		}
	}
	return ""
}

// GetDefaultSubnets retrieves the default subnets for the VPC
func GetDefaultSubnets(t *testing.T, vpcID string, isPublic bool) []string {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(types.AwsRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	supportedAZs, err := getSupportedEKSAvailabilityZones(ec2Client)
	require.NoError(t, err)

	// Build the filter to get subnets by the MapPublicIpOnLaunch attribute.
	filters := []ec2types.Filter{
		{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		{Name: aws.String("map-public-ip-on-launch"), Values: []string{strconv.FormatBool(isPublic)}},
		{Name: aws.String("availability-zone"), Values: supportedAZs},
	}

	result, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: filters,
	})
	require.NoError(t, err)

	var subnetIDs []string
	for _, subnet := range result.Subnets {
		subnetIDs = append(subnetIDs, aws.ToString(subnet.SubnetId))
	}

	// If no subnets were found using the filter, fall back to all subnets in supported AZs.
	if len(subnetIDs) == 0 {
		var subnetType string
		if isPublic {
			subnetType = "public"
		} else {
			subnetType = "private"
		}
		t.Logf("No %s subnets found with filter; using all available subnets in supported AZs for testing", subnetType)
		filters = []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("availability-zone"), Values: supportedAZs},
		}
		allSubnets, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			Filters: filters,
		})
		require.NoError(t, err)
		for _, subnet := range allSubnets.Subnets {
			subnetIDs = append(subnetIDs, aws.ToString(subnet.SubnetId))
		}
	}

	if len(subnetIDs) < 2 {
		t.Logf("Warning: Found fewer than 2 subnets in supported AZs. EKS requires at least 2 subnets in different AZs.")
	}

	return subnetIDs
}

// GetPrivateSubnetsInDifferentAZs finds private subnets in different AZs for the given VPC
func GetPrivateSubnetsInDifferentAZs(t *testing.T, vpcID string, count int) []string {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(types.AwsRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	// Get all available AZs
	azResult, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("region-name"), Values: []string{types.AwsRegion}},
		},
	})
	require.NoError(t, err)

	// Map of valid AZs
	validAZs := make(map[string]bool)
	for _, az := range azResult.AvailabilityZones {
		validAZs[aws.ToString(az.ZoneName)] = true
	}

	// Get all subnets
	result, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	require.NoError(t, err)

	// Map to store private subnets by AZ
	subnetsByAZ := make(map[string][]string)

	// Process each subnet
	for _, subnet := range result.Subnets {
		if !validAZs[aws.ToString(subnet.AvailabilityZone)] {
			continue
		}

		// Check if subnet is private
		rtResult, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("association.subnet-id"), Values: []string{aws.ToString(subnet.SubnetId)}},
			},
		})
		if err != nil {
			t.Logf("Warning: Error getting route tables for subnet %s: %v", aws.ToString(subnet.SubnetId), err)
			continue
		}

		isPrivate := true
		if len(rtResult.RouteTables) > 0 {
			for _, rt := range rtResult.RouteTables {
				for _, route := range rt.Routes {
					if route.GatewayId != nil && strings.HasPrefix(*route.GatewayId, "igw-") {
						isPrivate = false
						break
					}
				}
			}
		}

		if isPrivate {
			subnetsByAZ[aws.ToString(subnet.AvailabilityZone)] = append(subnetsByAZ[aws.ToString(subnet.AvailabilityZone)], aws.ToString(subnet.SubnetId))
			t.Logf("Found private subnet %s in AZ %s", aws.ToString(subnet.SubnetId), aws.ToString(subnet.AvailabilityZone))
		}
	}

	// Select subnets from different AZs
	var selectedSubnets []string
	usedAZs := make(map[string]bool)

	// Use sorted list of AZs for consistent selection
	var azNames []string
	for _, az := range azResult.AvailabilityZones {
		azNames = append(azNames, aws.ToString(az.ZoneName))
	}
	sort.Strings(azNames)

	for _, azName := range azNames {
		if len(selectedSubnets) >= count {
			break
		}
		if subnets := subnetsByAZ[azName]; len(subnets) > 0 && !usedAZs[azName] {
			selectedSubnets = append(selectedSubnets, subnets[0])
			usedAZs[azName] = true
			t.Logf("Selected private subnet %s from AZ %s", subnets[0], azName)
		}
	}

	// Instead of failing, return the empty slice
	if len(selectedSubnets) < count {
		t.Logf("Not enough private subnets in different AZs. Need %d, found %d", count, len(selectedSubnets))
		return []string{}
	}

	t.Logf("Selected %d private subnets in different AZs: %v", len(selectedSubnets), selectedSubnets)
	return selectedSubnets
}

// CreatePrivateSubnets creates private subnets in the specified VPC
func CreatePrivateSubnets(t *testing.T, vpcID string, count int, clusterName string) []string {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(types.AwsRegion))
	require.NoError(t, err)
	ec2Client := ec2.NewFromConfig(cfg)

	// Get VPC CIDR
	vpcResult, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, vpcResult.Vpcs)
	vpcCIDR := aws.ToString(vpcResult.Vpcs[0].CidrBlock)

	// Create private route table
	rtResult, err := ec2Client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeRouteTable,
				Tags:         []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("terratest-private")}},
			},
		},
	})
	require.NoError(t, err)
	rtID := aws.ToString(rtResult.RouteTable.RouteTableId)
	t.Logf("Created private route table: %s", rtID)

	// Setup cleanup if needed
	shouldDestroy := strings.ToLower(os.Getenv("TERRATEST_DESTROY")) == "true"
	var createdSubnets []string
	var routeTableAssociations []string

	if shouldDestroy {
		t.Cleanup(func() {
			cleanup.CleanupPrivateSubnetResources(t, ec2Client, createdSubnets, routeTableAssociations, rtID)
		})
	}

	// Get available AZs
	azResult, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("region-name"), Values: []string{types.AwsRegion}},
		},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(azResult.AvailabilityZones), 2)
	t.Logf("Found %d AZs", len(azResult.AvailabilityZones))

	desiredSubnets := 2 // Always create at least 2 subnets for RDS
	if count > desiredSubnets {
		desiredSubnets = count
	}

	// Create subnets across multiple AZs
	for i := 0; i < desiredSubnets; i++ {
		az := aws.ToString(azResult.AvailabilityZones[i].ZoneName)

		// Find next available CIDR block
		cidr, err := findNextAvailableCIDR(ctx, ec2Client, vpcID, vpcCIDR)
		require.NoError(t, err)

		t.Logf("Creating subnet in AZ %s with CIDR %s", az, cidr)

		result, err := ec2Client.CreateSubnet(ctx, &ec2.CreateSubnetInput{
			VpcId:            aws.String(vpcID),
			CidrBlock:        aws.String(cidr),
			AvailabilityZone: aws.String(az),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeSubnet,
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String(fmt.Sprintf("terratest-private-%d", i+1))},
						{Key: aws.String("kubernetes.io/cluster/" + clusterName), Value: aws.String("owned")},
					},
				},
			},
		})
		require.NoError(t, err)

		subnetID := aws.ToString(result.Subnet.SubnetId)
		t.Logf("Created subnet %s", subnetID)

		// Associate with private route table
		assocResult, err := ec2Client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
			SubnetId:     aws.String(subnetID),
			RouteTableId: aws.String(rtID),
		})
		require.NoError(t, err)
		t.Logf("Associated subnet %s with route table %s", subnetID, rtID)

		// Store the association ID for cleanup
		routeTableAssociations = append(routeTableAssociations, aws.ToString(assocResult.AssociationId))

		// Wait for subnet to be available (v2 waiter)
		waiter := ec2.NewSubnetAvailableWaiter(ec2Client)
		err = waiter.Wait(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		}, 5*time.Minute)
		require.NoError(t, err)

		createdSubnets = append(createdSubnets, subnetID)
	}

	// Verify subnets are in different AZs
	azMap := make(map[string]bool)
	for _, subnetID := range createdSubnets {
		subnet, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		})
		require.NoError(t, err)
		azMap[aws.ToString(subnet.Subnets[0].AvailabilityZone)] = true
	}
	require.GreaterOrEqual(t, len(azMap), 2, "Must have subnets in at least 2 different AZs")

	t.Logf("Successfully created %d private subnets across %d AZs", len(createdSubnets), len(azMap))
	return createdSubnets
}

// getSupportedEKSAvailabilityZones returns the list of supported EKS availability zones
func getSupportedEKSAvailabilityZones(ec2Client *ec2.Client) ([]string, error) {
	ctx := context.Background()

	// EKS supported AZs in us-east-1 (kept per original logic)
	supportedAZs := []string{"us-east-1a", "us-east-1b", "us-east-1c", "us-east-1d", "us-east-1f"}

	result, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("zone-name"), Values: supportedAZs},
			{Name: aws.String("state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return nil, err
	}

	var availableAZs []string
	for _, az := range result.AvailabilityZones {
		availableAZs = append(availableAZs, aws.ToString(az.ZoneName))
	}
	return availableAZs, nil
}
