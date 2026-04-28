package variant

import (
	"strings"
	"testing"
)

func TestGenerateDomainName(t *testing.T) {
	ng := NewNameGenerator()
	name := ng.GenerateDomainName()

	if name == "" {
		t.Fatal("expected non-empty domain name")
	}
	if len(name) > maxNetBIOSLength {
		t.Errorf("domain name %q exceeds NetBIOS limit of %d", name, maxNetBIOSLength)
	}
}

func TestGenerateDomainNameUniqueness(t *testing.T) {
	ng := NewNameGenerator()
	seen := make(map[string]bool)
	for range 10 {
		name := ng.GenerateDomainName()
		if seen[strings.ToLower(name)] {
			t.Errorf("duplicate domain name: %s", name)
		}
		seen[strings.ToLower(name)] = true
	}
}

func TestGenerateUsername(t *testing.T) {
	ng := NewNameGenerator()
	username := ng.GenerateUsername()

	if !strings.Contains(username, ".") {
		t.Errorf("expected firstname.lastname format, got %q", username)
	}

	parts := strings.SplitN(username, ".", 2)
	if parts[0] != strings.ToLower(parts[0]) || parts[1] != strings.ToLower(parts[1]) {
		t.Errorf("expected lowercase username, got %q", username)
	}
}

func TestGenerateUsernameUniqueness(t *testing.T) {
	ng := NewNameGenerator()
	seen := make(map[string]bool)
	for range 50 {
		name := ng.GenerateUsername()
		if seen[name] {
			t.Errorf("duplicate username: %s", name)
		}
		seen[name] = true
	}
}

func TestGenerateHostname(t *testing.T) {
	ng := NewNameGenerator()
	name := ng.GenerateHostname()
	if name == "" {
		t.Fatal("expected non-empty hostname")
	}
	if name != strings.ToLower(name) {
		t.Errorf("expected lowercase hostname, got %q", name)
	}
}

func TestGeneratePassword(t *testing.T) {
	ng := NewNameGenerator()

	tests := []struct {
		name     string
		original string
		wantLen  int
	}{
		{"lowercase", "password", 8},
		{"mixed", "Password1!", 10},
		{"long", "averylongpasswordthatiscomplex", 30},
		{"empty", "", 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw := ng.GeneratePassword(tt.original)
			if len(pw) != tt.wantLen {
				t.Errorf("expected length %d, got %d for %q", tt.wantLen, len(pw), pw)
			}
			if pw == tt.original {
				t.Errorf("password should differ from original")
			}
		})
	}
}

func TestGenerateCrackablePassword(t *testing.T) {
	ng := NewNameGenerator()

	// Should return a non-empty password from the wordlist
	pw := ng.GenerateCrackablePassword()
	if pw == "" {
		t.Error("crackable password should not be empty")
	}

	// Should be in the wordlist
	found := false
	for _, w := range ng.crackablePasswords {
		if w == pw {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("crackable password %q not found in wordlist", pw)
	}

	// Multiple calls should eventually produce different passwords
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		seen[ng.GenerateCrackablePassword()] = true
	}
	if len(seen) < 2 {
		t.Error("expected some variety in crackable passwords")
	}
}

func TestGenerateGroupName(t *testing.T) {
	ng := NewNameGenerator()
	name := ng.GenerateGroupName()
	if name == "" {
		t.Fatal("expected non-empty group name")
	}
}

func TestGenerateOUName(t *testing.T) {
	ng := NewNameGenerator()
	name := ng.GenerateOUName()
	if name == "" {
		t.Fatal("expected non-empty OU name")
	}
}

func TestGenerateGMSAName(t *testing.T) {
	ng := NewNameGenerator()
	name := ng.GenerateGMSAName()
	if !strings.HasPrefix(name, "gmsa") {
		t.Errorf("expected gmsa prefix, got %q", name)
	}
}

func TestEnsureUnique(t *testing.T) {
	ng := NewNameGenerator()
	ng.usedNames["test"] = true

	result := ng.ensureUnique("test")
	if result != "test2" {
		t.Errorf("expected test2, got %q", result)
	}

	result = ng.ensureUnique("test")
	if result != "test3" {
		t.Errorf("expected test3, got %q", result)
	}
}
