package variant

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecryptSecureString(t *testing.T) {
	// Real values from GOAD secret.ps1
	key := []byte{177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91}
	blob := "76492d1116743f0423413b16050a5345MgB8AGkAcwBDACsAUwArADIAcABRAEcARABnAGYAMwA3AEEAcgBFAEIAYQB2AEEAPQA9AHwAZQAwADgANAA2ADQAMABiADYANAAwADYANgA1ADcANgAxAGIAMQBhAGQANQBlAGYAYQBiADQAYQA2ADkAZgBlAGQAMQAzADAANQAyADUAMgAyADYANAA3ADAAZABiAGEAOAA0AGUAOQBkAGMAZABmAGEANAAyADkAZgAyADIAMwA="

	got, err := decryptSecureString(blob, key)
	if err != nil {
		t.Fatalf("decryptSecureString: %v", err)
	}
	if got != "powerkingftw135" {
		t.Errorf("got %q, want %q", got, "powerkingftw135")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := []byte{177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91}
	password := "f5ql8xzwbco69kd"

	blob, err := encryptSecureString(password, key)
	if err != nil {
		t.Fatalf("encryptSecureString: %v", err)
	}

	if !strings.HasPrefix(blob, secureStringMagic) {
		t.Error("missing magic prefix")
	}

	got, err := decryptSecureString(blob, key)
	if err != nil {
		t.Fatalf("decryptSecureString roundtrip: %v", err)
	}
	if got != password {
		t.Errorf("roundtrip got %q, want %q", got, password)
	}
}

// TestDecryptSecureStringErrors verifies that decryptSecureString returns
// clear errors for malformed blobs rather than panicking.
func TestDecryptSecureStringErrors(t *testing.T) {
	key := []byte{177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91}

	tests := []struct {
		name    string
		blob    string
		wantErr string
	}{
		{"wrong magic prefix", "00000000000000000000000000000000AAAA", "magic"},
		{"truncated base64", secureStringMagic + "not-valid-base64!!!", "base64"},
		{"bad IV length", secureStringMagic + buildBlobWithBadIV(), "iv length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decryptSecureString(tt.blob, key)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// buildBlobWithBadIV creates a SecureString blob whose inner "2|iv|ct" has a
// 4-byte IV instead of the required 16-byte IV.
func buildBlobWithBadIV() string {
	inner := "2|AAAA|00112233" // AAAA decodes to 3 bytes, not 16
	u16 := encodeUTF16LE(inner)
	return base64.StdEncoding.EncodeToString(u16)
}

// TestEncryptDecryptRoundtripEdgeCases covers empty and unicode passwords.
func TestEncryptDecryptRoundtripEdgeCases(t *testing.T) {
	key := []byte{177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91}

	tests := []struct {
		name     string
		password string
	}{
		{"empty password", ""},
		{"unicode emoji", "p@ss🔐word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob, err := encryptSecureString(tt.password, key)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			got, err := decryptSecureString(blob, key)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}
			if got != tt.password {
				t.Errorf("roundtrip got %q, want %q", got, tt.password)
			}
		})
	}
}

func TestFixSecureStrings(t *testing.T) {
	g := &Generator{
		mappings: Mappings{
			Passwords: map[string]string{
				"powerkingftw135": "newpassword123",
			},
		},
	}

	input := `# secret stored :
$keyData = 177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91
$secret="76492d1116743f0423413b16050a5345MgB8AGkAcwBDACsAUwArADIAcABRAEcARABnAGYAMwA3AEEAcgBFAEIAYQB2AEEAPQA9AHwAZQAwADgANAA2ADQAMABiADYANAAwADYANgA1ADcANgAxAGIAMQBhAGQANQBlAGYAYQBiADQAYQA2ADkAZgBlAGQAMQAzADAANQAyADUAMgAyADYANAA3ADAAZABiAGEAOAA0AGUAOQBkAGMAZABmAGEANAAyADkAZgAyADIAMwA="
`

	output := g.fixSecureStrings(input)

	// The $secret line should have changed.
	if output == input {
		t.Fatal("fixSecureStrings did not modify content")
	}

	// Verify the new blob decrypts to the mapped password.
	key := []byte{177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91}
	parts := reSecret.FindStringSubmatch(output)
	if len(parts) < 3 {
		t.Fatal("could not find $secret in output")
	}
	got, err := decryptSecureString(parts[2], key)
	if err != nil {
		t.Fatalf("decrypt new blob: %v", err)
	}
	if got != "newpassword123" {
		t.Errorf("new blob decrypts to %q, want %q", got, "newpassword123")
	}
}
