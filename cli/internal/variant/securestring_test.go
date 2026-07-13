package variant

import (
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
