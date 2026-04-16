package helpers

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
	"github.com/gruntwork-io/terratest/modules/aws"
)

// GetDefaultVPCID retrieves the default VPC ID
//
// **Parameters:**
//
// t: The testing object
//
// **Returns:**
//
// string: The default VPC ID
func GetDefaultVPCID(t *testing.T) string {
	vpc, err := aws.GetDefaultVpcE(t, types.AwsRegion)
	if err != nil {
		t.Fatalf("Failed to get default VPC: %v", err)
	}
	if vpc == nil || vpc.Id == "" {
		t.Fatal("No default VPC found in region")
	}
	t.Logf("Found default VPC: %s", vpc.Id)
	return vpc.Id
}

// GetPublicIP retrieves the public IP address of the current machine
//
// **Parameters:**
//
// t: The testing object (can be nil if not needed for logging)
//
// **Returns:**
//
// string: The public IP address
// error: Any error that occurred
func GetPublicIP(t *testing.T) (string, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		if t != nil {
			t.Logf("Error getting public IP: %v", err)
		}
		return "", fmt.Errorf("error getting public IP: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if t != nil {
			t.Logf("Error reading response: %v", err)
		}
		return "", fmt.Errorf("error reading response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}
