package cmd

import "testing"

func TestDeriveAzureSubnets(t *testing.T) {
	tests := []struct {
		name     string
		vnetCIDR string
		wantBast string
		wantCtrl string
		wantKali string
		wantErr  bool
	}{
		{"standard", "10.8.0.0/16", "10.8.2.0/26", "10.8.3.0/28", "10.8.4.0/28", false},
		{"different octet", "10.1.0.0/16", "10.1.2.0/26", "10.1.3.0/28", "10.1.4.0/28", false},
		{"high octet", "10.200.0.0/16", "10.200.2.0/26", "10.200.3.0/28", "10.200.4.0/28", false},
		{"not /16", "10.8.0.0/24", "", "", "", true},
		{"invalid CIDR", "not-a-cidr", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnets, err := deriveAzureSubnets(tt.vnetCIDR)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if subnets.Bastion != tt.wantBast {
				t.Errorf("bastion = %q, want %q", subnets.Bastion, tt.wantBast)
			}
			if subnets.Controller != tt.wantCtrl {
				t.Errorf("controller = %q, want %q", subnets.Controller, tt.wantCtrl)
			}
			if subnets.Kali != tt.wantKali {
				t.Errorf("kali = %q, want %q", subnets.Kali, tt.wantKali)
			}
		})
	}
}
