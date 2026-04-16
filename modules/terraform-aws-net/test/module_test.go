package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	terratestaws "github.com/gruntwork-io/terratest/modules/aws"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/require"
)

const (
	awsRegion          = "us-east-2"
	envName            = "tt"
	testDeploymentName = "network"
)

// TestTerraformAwsNet tests the Terraform module that creates a VPC with public and private subnets
//
// **Parameters:**
//
// t: The testing.T object
func TestTerraformAwsNet(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(awsRegion))
	require.NoError(t, err)

	ec2Client := ec2v2.NewFromConfig(cfg)

	// Define base terraform options
	terraformOptions := &terraform.Options{
		TerraformDir: "../",
		Vars: map[string]interface{}{
			"env":             envName,
			"deployment_name": testDeploymentName,
			"vpc_endpoints": map[string]interface{}{
				"s3": map[string]interface{}{
					"service":            "s3",
					"type":               "Gateway",
					"security_group_ids": []string{},
				},
				"secretsmanager": map[string]interface{}{
					"service":            "secretsmanager",
					"type":               "Interface",
					"private_dns":        true,
					"security_group_ids": []string{},
				},
			},
		},
		EnvVars: map[string]string{
			"AWS_DEFAULT_REGION": awsRegion,
		},
		NoColor: true,
		Lock:    false,
	}

	// Setup cleanup for end of test
	defer func() {
		if os.Getenv("TERRATEST_DESTROY") == "true" {
			destroyOptions := &terraform.Options{
				TerraformDir: terraformOptions.TerraformDir,
				Vars: map[string]interface{}{
					"deployment_name": testDeploymentName,
					"env":             envName,
				},
				EnvVars: terraformOptions.EnvVars,
				NoColor: true,
				Lock:    false,
			}
			// Run Terraform init before destroy
			_, err := terraform.InitE(t, destroyOptions)
			require.NoError(t, err, "Failed to init before destroy")
			_, err = terraform.DestroyE(t, destroyOptions)
			require.NoError(t, err, "Failed to destroy Terraform")
		}
	}()

	// Initialize Terraform
	_, err = terraform.InitE(t, terraformOptions)
	require.NoError(t, err, "Failed to init Terraform")

	// Create a plan file
	planFilePath := filepath.Join(os.TempDir(), "network-plan.tfplan")
	_, err = terraform.PlanE(t, &terraform.Options{
		TerraformDir: terraformOptions.TerraformDir,
		Vars:         terraformOptions.Vars,
		EnvVars:      terraformOptions.EnvVars,
		PlanFilePath: planFilePath,
	})
	require.NoError(t, err, "Failed to create plan")

	// Apply the saved plan
	_, err = terraform.ApplyE(t, &terraform.Options{
		TerraformDir: terraformOptions.TerraformDir,
		PlanFilePath: planFilePath,
	})
	require.NoError(t, err, "Failed to apply plan")

	// Get outputs
	natEIP := terraform.Output(t, terraformOptions, "nat_eip")
	privateSubnetIDs := terraform.OutputList(t, terraformOptions, "private_subnet_ids")
	publicSubnetIDs := terraform.OutputList(t, terraformOptions, "public_subnet_ids")
	vpcID := terraform.Output(t, terraformOptions, "vpc_id")
	privateRouteTableID := terraform.Output(t, terraformOptions, "private_route_table_id")

	// Run validations
	validateSubnetDistribution(t, context.TODO(), ec2Client, publicSubnetIDs)
	validateVPCExists(t, vpcID)
	validateSubnetsExist(t, vpcID, publicSubnetIDs, awsRegion)
	validateSubnetsExist(t, vpcID, privateSubnetIDs, awsRegion)
	validateNATEIP(t, context.TODO(), ec2Client, natEIP)
	validateVPCEndpoints(t, context.TODO(), ec2Client, vpcID)
	validatePrivateRouteTable(t, context.TODO(), ec2Client, privateRouteTableID)
}

func validateVPCExists(t *testing.T, vpcID string) {
	result, err := terratestaws.GetVpcByIdE(t, vpcID, awsRegion)
	if err != nil {
		t.Fatalf("Failed to describe VPCs: %v", err)
	}

	if result.Name == "" {
		t.Fatalf("VPC with ID %s does not exist", vpcID)
	}
}

func validateSubnetsExist(t *testing.T, vpcID string, expectedSubnetIDs []string, region string) {
	filters := []types.Filter{
		{
			Name:   aws.String("vpc-id"),
			Values: []string{vpcID},
		},
	}

	subnets, err := terratestaws.GetSubnetsForVpcE(t, region, filters)
	if err != nil {
		t.Fatalf("Failed to get subnets for VPC %s: %v", vpcID, err)
	}

	foundSubnetsSet := make(map[string]struct{})
	for _, subnet := range subnets {
		foundSubnetsSet[subnet.Id] = struct{}{}
	}

	for _, id := range expectedSubnetIDs {
		if _, found := foundSubnetsSet[id]; !found {
			t.Errorf("Expected subnet with ID %s not found in VPC %s", id, vpcID)
		}
	}
}

func validateNATEIP(t *testing.T, ctx context.Context, ec2Client *ec2v2.Client, natEIP string) {
	if natEIP == "" {
		t.Fatalf("NAT EIP output is empty")
	}

	input := &ec2v2.DescribeAddressesInput{
		PublicIps: []string{natEIP},
	}

	result, err := ec2Client.DescribeAddresses(ctx, input)
	if err != nil {
		t.Fatalf("Failed to describe Elastic IP %s: %v", natEIP, err)
	}

	if len(result.Addresses) == 0 {
		t.Fatalf("NAT EIP %s does not exist", natEIP)
	}
	t.Logf("NAT EIP %s found", natEIP)
}

func validateSubnetDistribution(t *testing.T, ctx context.Context, ec2Client *ec2v2.Client, subnetIDs []string) {
	azMap := make(map[string]bool)

	for _, subnetID := range subnetIDs {
		result, err := ec2Client.DescribeSubnets(ctx, &ec2v2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(result.Subnets))

		az := *result.Subnets[0].AvailabilityZone
		require.False(t, azMap[az], "Multiple subnets found in the same AZ: %s", az)
		azMap[az] = true
	}

	require.GreaterOrEqual(t, len(azMap), 2, "Need at least 2 different AZs for public subnets")
	t.Logf("Validated public subnets are in different AZs: %v", azMap)
}

func validateVPCEndpoints(t *testing.T, ctx context.Context, ec2Client *ec2v2.Client, vpcID string) {
	input := &ec2v2.DescribeVpcEndpointsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	result, err := ec2Client.DescribeVpcEndpoints(ctx, input)
	if err != nil {
		t.Fatalf("Failed to describe VPC endpoints: %v", err)
	}

	foundS3Endpoint := false
	foundSecretsEndpoint := false

	for _, endpoint := range result.VpcEndpoints {
		switch *endpoint.ServiceName {
		case "com.amazonaws." + awsRegion + ".s3":
			foundS3Endpoint = true
			if endpoint.VpcEndpointType != types.VpcEndpointTypeGateway {
				t.Errorf("S3 endpoint is not a Gateway type")
			}
		case "com.amazonaws." + awsRegion + ".secretsmanager":
			foundSecretsEndpoint = true
			if endpoint.VpcEndpointType != types.VpcEndpointTypeInterface {
				t.Errorf("Secrets Manager endpoint is not an Interface type")
			}
			if len(endpoint.Groups) == 0 {
				t.Errorf("Secrets Manager endpoint has no security groups attached")
			}
		}
	}

	if !foundS3Endpoint {
		t.Error("S3 Gateway endpoint not found")
	}
	if !foundSecretsEndpoint {
		t.Error("Secrets Manager Interface endpoint not found")
	}
}

func validatePrivateRouteTable(t *testing.T, ctx context.Context, ec2Client *ec2v2.Client, routeTableID string) {
	// Get the private route table by ID
	input := &ec2v2.DescribeRouteTablesInput{
		RouteTableIds: []string{routeTableID},
	}

	result, err := ec2Client.DescribeRouteTables(ctx, input)
	if err != nil {
		t.Fatalf("Failed to describe route table %s: %v", routeTableID, err)
	}

	if len(result.RouteTables) == 0 {
		t.Fatalf("Private route table %s not found", routeTableID)
	}

	routeTable := result.RouteTables[0]
	t.Logf("Found private route table: %s", *routeTable.RouteTableId)

	// Validate that there's a route to 0.0.0.0/0 via NAT gateway
	foundNATRoute := false
	var natGatewayID string

	for _, route := range routeTable.Routes {
		if route.DestinationCidrBlock != nil && *route.DestinationCidrBlock == "0.0.0.0/0" {
			if route.NatGatewayId != nil {
				foundNATRoute = true
				natGatewayID = *route.NatGatewayId
				t.Logf("Found NAT gateway route: destination=%s, nat_gateway=%s", *route.DestinationCidrBlock, natGatewayID)
				break
			}
		}
	}

	if !foundNATRoute {
		t.Error("NAT gateway route (0.0.0.0/0) not found in private route table")
	}

	// Verify the NAT gateway exists
	if natGatewayID != "" {
		natInput := &ec2v2.DescribeNatGatewaysInput{
			NatGatewayIds: []string{natGatewayID},
		}
		natResult, err := ec2Client.DescribeNatGateways(ctx, natInput)
		if err != nil {
			t.Fatalf("Failed to describe NAT gateway %s: %v", natGatewayID, err)
		}
		if len(natResult.NatGateways) == 0 {
			t.Errorf("NAT gateway %s referenced in route does not exist", natGatewayID)
		} else {
			t.Logf("Verified NAT gateway %s exists with state: %s", natGatewayID, natResult.NatGateways[0].State)
		}
	}
}
