package provider

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// DecodeWindowsText normalises text captured from a Windows host.
//
// Windows PowerShell writes its own fatal-error banner to stderr as UTF-16LE
// while the transports (WinRM, Azure Run Command, SSM) hand it back as opaque
// bytes. Read as UTF-8 that becomes NUL-interleaved mojibake — "W\x00i\x00n…"
// — which renders in a chat pane as "W i n d o w s", roughly doubles the token
// cost of every such message, and defeats any parsing done on it.
//
// The heuristic is narrow on purpose: only text that is *mostly* NUL bytes in
// alternating position is treated as UTF-16, so legitimate output containing an
// occasional NUL is left alone. Text that is already valid UTF-8 without NULs
// is returned unchanged, so this is safe to apply unconditionally.
func DecodeWindowsText(s string) string {
	if s == "" || !strings.Contains(s, "\x00") {
		return s
	}

	b := []byte(s)
	// A UTF-16LE run of ASCII has a NUL in every odd byte. Require most of the
	// odd positions to be NUL and most of the even ones not to be, which a
	// UTF-8 string carrying a stray NUL will not satisfy.
	var oddNUL, evenNUL, odd, even int
	for i, c := range b {
		if i%2 == 1 {
			odd++
			if c == 0 {
				oddNUL++
			}
		} else {
			even++
			if c == 0 {
				evenNUL++
			}
		}
	}
	if odd == 0 || oddNUL*4 < odd*3 || evenNUL*4 > even {
		return stripNULs(s)
	}

	// Drop a trailing odd byte so pairing can't run off the end.
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
	}
	decoded := string(utf16.Decode(units))
	if !utf8.ValidString(decoded) {
		return stripNULs(s)
	}
	return decoded
}

// stripNULs removes NUL bytes from text that isn't UTF-16. They are never
// meaningful in command output and a raw NUL breaks JSON consumers downstream.
func stripNULs(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// CleanResult normalises a CommandResult's captured streams in place-safe
// fashion, returning the same pointer for convenient chaining. Nil is passed
// through so callers don't have to guard.
func CleanResult(r *CommandResult) *CommandResult {
	if r == nil {
		return nil
	}
	r.Stdout = DecodeWindowsText(r.Stdout)
	r.Stderr = DecodeWindowsText(r.Stderr)
	return r
}
