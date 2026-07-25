package variant

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// secureStringMagic is the fixed 16-byte prefix PowerShell prepends to
// ConvertFrom-SecureString output (hex-encoded = 32 chars).
const secureStringMagic = "76492d1116743f0423413b16050a5345"

// reKeyData matches  $keyData = 177, 252, 228, ...
var reKeyData = regexp.MustCompile(`(?m)^\s*\$keyData\s*=\s*(.+)$`)

// reSecret matches  $secret="76492d..."
var reSecret = regexp.MustCompile(`(?m)(\$secret\s*=\s*")([^"]+)(")`)

// fixSecureStrings finds PowerShell SecureString patterns in content,
// decrypts the embedded password, maps it via g.mappings.Passwords, and
// re-encrypts with the new password. Returns the (possibly modified) content.
func (g *Generator) fixSecureStrings(content string) string {
	keyMatch := reKeyData.FindStringSubmatch(content)
	if keyMatch == nil {
		return content
	}
	key, err := parseKeyBytes(keyMatch[1])
	if err != nil {
		fmt.Printf("  Warning: could not parse $keyData: %v\n", err)
		return content
	}

	return reSecret.ReplaceAllStringFunc(content, func(match string) string {
		parts := reSecret.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		blob := parts[2]

		plaintext, err := decryptSecureString(blob, key)
		if err != nil {
			fmt.Printf("  Warning: could not decrypt SecureString: %v\n", err)
			return match
		}

		newPassword, ok := g.mappings.Passwords[plaintext]
		if !ok {
			fmt.Printf("  Warning: decrypted password %q not in mappings\n", plaintext)
			return match
		}

		newBlob, err := encryptSecureString(newPassword, key)
		if err != nil {
			fmt.Printf("  Warning: could not re-encrypt SecureString: %v\n", err)
			return match
		}

		fmt.Printf("  Fixed SecureString: re-encrypted with mapped password\n")
		return parts[1] + newBlob + parts[3]
	})
}

// parseKeyBytes parses "177, 252, 228, 64, ..." into a byte slice.
func parseKeyBytes(s string) ([]byte, error) {
	parts := strings.Split(s, ",")
	key := make([]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid key byte %q: %w", p, err)
		}
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("key byte %d out of range [0,255]", v)
		}
		key = append(key, byte(v))
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("expected 32-byte key, got %d bytes", len(key))
	}
	return key, nil
}

// decryptSecureString decrypts a PowerShell SecureString blob.
// Format: magic_hex + base64(utf16le("iv_base64|ct_hex"))
func decryptSecureString(blob string, key []byte) (string, error) {
	if !strings.HasPrefix(blob, secureStringMagic) {
		return "", fmt.Errorf("missing SecureString magic prefix")
	}
	encoded := blob[len(secureStringMagic):]

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	inner := decodeUTF16LE(decoded)

	// Format: "2|iv_base64|ct_hex" (version 2 with AES key).
	parts := strings.SplitN(inner, "|", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("expected version|iv|ct format, got %d parts", len(parts))
	}

	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("iv base64 decode: %w", err)
	}

	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("iv length %d, expected %d", len(iv), aes.BlockSize)
	}

	ct, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("ct hex decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	if len(ct)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d not a multiple of block size", len(ct))
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	pt := make([]byte, len(ct))
	mode.CryptBlocks(pt, ct)

	// Remove PKCS7 padding — validate all padding bytes match.
	if len(pt) > 0 {
		padLen := int(pt[len(pt)-1])
		if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(pt) {
			valid := true
			for i := 0; i < padLen; i++ {
				if pt[len(pt)-1-i] != byte(padLen) {
					valid = false
					break
				}
			}
			if valid {
				pt = pt[:len(pt)-padLen]
			}
		}
	}

	return decodeUTF16LE(pt), nil
}

// encryptSecureString encrypts a plaintext password into PowerShell
// SecureString format using the given AES-256 key.
func encryptSecureString(plaintext string, key []byte) (string, error) {
	ptBytes := encodeUTF16LE(plaintext)

	// PKCS7 pad to AES block size.
	padLen := aes.BlockSize - (len(ptBytes) % aes.BlockSize)
	for i := 0; i < padLen; i++ {
		ptBytes = append(ptBytes, byte(padLen))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}

	ct := make([]byte, len(ptBytes))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct, ptBytes)

	// Build inner string: "2|iv_base64|ct_hex"
	inner := "2|" + base64.StdEncoding.EncodeToString(iv) + "|" + hex.EncodeToString(ct)

	// Encode as UTF-16LE then base64.
	innerBytes := encodeUTF16LE(inner)
	encoded := base64.StdEncoding.EncodeToString(innerBytes)

	return secureStringMagic + encoded, nil
}

// decodeUTF16LE decodes a UTF-16LE byte slice to a Go string.
func decodeUTF16LE(b []byte) string {
	if len(b)%2 != 0 {
		// Odd length means corrupted data — drop the trailing byte but warn.
		fmt.Printf("  Warning: odd-length UTF-16LE data (%d bytes), truncating\n", len(b))
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u16))
}

// encodeUTF16LE encodes a Go string to UTF-16LE bytes.
func encodeUTF16LE(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return b
}
