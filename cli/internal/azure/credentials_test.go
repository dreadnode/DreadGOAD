package azure

import (
	"context"
	"testing"
)

func TestVerifyCredentials_UsesEnvVar(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "env-sub-123")
	c := &Client{cred: noOpCred{}}
	info, err := c.VerifyCredentials(context.Background())
	if err != nil {
		t.Fatalf("VerifyCredentials: %v", err)
	}
	if info.ID != "env-sub-123" {
		t.Errorf("ID = %q, want env-sub-123", info.ID)
	}
	if c.SubscriptionID != "env-sub-123" {
		t.Errorf("client SubscriptionID = %q, want env-sub-123", c.SubscriptionID)
	}
	if info.Name != "" {
		t.Errorf("Name = %q, want empty (env path doesn't yield metadata)", info.Name)
	}
}

func TestVerifyCredentials_TrimsEnvVar(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "  padded-sub  ")
	c := &Client{cred: noOpCred{}}
	info, err := c.VerifyCredentials(context.Background())
	if err != nil {
		t.Fatalf("VerifyCredentials: %v", err)
	}
	if info.ID != "padded-sub" {
		t.Errorf("ID = %q, want padded-sub (whitespace stripped)", info.ID)
	}
}
