package provider

import (
	"strings"
	"testing"
)

// utf16le encodes ASCII the way Windows PowerShell writes its fatal banner.
func utf16le(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteByte(byte(r))
		b.WriteByte(0)
	}
	return b.String()
}

// The exact payload observed from DC02 via /exec.
func TestDecodeWindowsTextRealPowerShellBanner(t *testing.T) {
	want := "Windows PowerShell terminated with the following error: \r\n " +
		"Could not load file or assembly 'System.Management.Automation'. " +
		"The paging file is too small for this operation to complete."
	got := DecodeWindowsText(utf16le(want))
	if got != want {
		t.Fatalf("decode mismatch:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "\x00") {
		t.Fatal("NULs survived decoding")
	}
}

// Ordinary output must be returned byte-identical — this runs on every result.
func TestDecodeWindowsTextLeavesPlainTextAlone(t *testing.T) {
	for _, s := range []string{
		"",
		"Status : Stopped",
		"Running   dreadindex-dreadgoad-DC02-vm   10.1.1.7",
		"unicode: ünïcode ✓ 日本語",
		strings.Repeat("A", 4096),
	} {
		if got := DecodeWindowsText(s); got != s {
			t.Fatalf("plain text altered:\n got %q\nwant %q", got, s)
		}
	}
}

// A stray NUL in otherwise-UTF-8 text must not trigger UTF-16 decoding; the
// NUL is dropped because it breaks JSON consumers, but the text survives.
func TestDecodeWindowsTextStrayNUL(t *testing.T) {
	got := DecodeWindowsText("service stopped\x00 unexpectedly")
	if got != "service stopped unexpectedly" {
		t.Fatalf("got %q", got)
	}
}

// Odd-length input must not panic or lose the message.
func TestDecodeWindowsTextOddLength(t *testing.T) {
	got := DecodeWindowsText(utf16le("hello") + "\x41")
	if !strings.Contains(got, "hello") {
		t.Fatalf("got %q", got)
	}
}

func TestCleanResultHandlesNilAndBothStreams(t *testing.T) {
	if CleanResult(nil) != nil {
		t.Fatal("nil must pass through")
	}
	r := CleanResult(&CommandResult{
		Status: "Failed",
		Stdout: utf16le("out"),
		Stderr: utf16le("err"),
	})
	if r.Stdout != "out" || r.Stderr != "err" {
		t.Fatalf("got stdout=%q stderr=%q", r.Stdout, r.Stderr)
	}
	if r.Status != "Failed" {
		t.Fatalf("status must be untouched, got %q", r.Status)
	}
}
