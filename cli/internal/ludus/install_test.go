package ludus

import (
	"runtime"
	"testing"
)

func TestClientAssetName(t *testing.T) {
	name, err := clientAssetName("2.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it contains the version.
	if got := name; got == "" {
		t.Fatal("empty asset name")
	}

	// Verify OS mapping.
	switch runtime.GOOS {
	case "darwin":
		if got := name; !contains(got, "macOS") {
			t.Errorf("expected macOS in asset name, got %s", got)
		}
	case "linux":
		if got := name; !contains(got, "linux") {
			t.Errorf("expected linux in asset name, got %s", got)
		}
	}

	// Verify arch is present.
	if got := name; !contains(got, runtime.GOARCH) {
		t.Errorf("expected %s in asset name, got %s", runtime.GOARCH, got)
	}

	// Verify version is present.
	if got := name; !contains(got, "2.1.1") {
		t.Errorf("expected version 2.1.1 in asset name, got %s", got)
	}
}

func TestDownloadURL(t *testing.T) {
	url := downloadURL("2.1.1", "ludus-client_linux-amd64-2.1.1")
	expected := "https://gitlab.com/api/v4/projects/54052321/packages/generic/ludus/2.1.1/ludus-client_linux-amd64-2.1.1"
	if url != expected {
		t.Errorf("got %s, want %s", url, expected)
	}
}

func TestChecksumsURL(t *testing.T) {
	url := checksumsURL("2.1.1")
	expected := "https://gitlab.com/api/v4/projects/54052321/packages/generic/ludus/2.1.1/ludus_2.1.1_checksums.txt"
	if url != expected {
		t.Errorf("got %s, want %s", url, expected)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
