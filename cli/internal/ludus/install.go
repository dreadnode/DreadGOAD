package ludus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// GitLab project ID for badsectorlabs/ludus.
	gitlabProjectID = "54052321"
	gitlabAPIBase   = "https://gitlab.com/api/v4/projects/" + gitlabProjectID
)

// latestVersion fetches the latest Ludus release tag from the GitLab API.
func latestVersion(ctx context.Context) (string, error) {
	url := gitlabAPIBase + "/releases/permalink/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Use HEAD and follow redirects to extract the tag from the final URL.
	// The permalink redirects to /releases/<tag>.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest ludus release: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return "", fmt.Errorf("close ludus release response: %w", err)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect from ludus release permalink (status %d)", resp.StatusCode)
	}
	// Location is like /badsectorlabs/ludus/-/releases/v2.1.1
	parts := strings.Split(loc, "/")
	tag := parts[len(parts)-1]
	// Strip leading "v" if present.
	return strings.TrimPrefix(tag, "v"), nil
}

// clientAssetName returns the expected binary asset name for the current platform.
func clientAssetName(version string) (string, error) {
	var osName string
	switch runtime.GOOS {
	case "darwin":
		osName = "macOS"
	case "linux":
		osName = "linux"
	case "windows":
		osName = "windows"
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	case "arm":
		arch = "arm"
	case "386":
		arch = "386"
	default:
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}

	name := fmt.Sprintf("ludus-client_%s-%s-%s", osName, arch, version)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

// downloadURL returns the GitLab generic package URL for a given asset.
func downloadURL(version, asset string) string {
	return fmt.Sprintf("%s/packages/generic/ludus/%s/%s", gitlabAPIBase, version, asset)
}

// checksumsURL returns the URL for the checksums file for a given version.
func checksumsURL(version string) string {
	return fmt.Sprintf("%s/packages/generic/ludus/%s/ludus_%s_checksums.txt", gitlabAPIBase, version, version)
}

// installDir returns the directory where the ludus binary will be installed.
// Prefers /usr/local/bin, falls back to ~/.local/bin if /usr/local/bin is not writable.
func installDir() string {
	preferred := "/usr/local/bin"
	if runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		return filepath.Join(home, "AppData", "Local", "Programs", "ludus")
	}

	// Check if we can write to /usr/local/bin via a probe file. If we can
	// create+close+remove cleanly, the directory is usable.
	if f, err := os.CreateTemp(preferred, ".ludus-test-*"); err == nil {
		name := f.Name()
		closeErr := f.Close()
		removeErr := os.Remove(name)
		if closeErr == nil && removeErr == nil {
			return preferred
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "bin")
}

// EnsureCLI checks for the ludus binary in PATH and installs it if missing.
// Returns the path to the ludus binary. If the binary is already available,
// this is a no-op.
func EnsureCLI(ctx context.Context) (string, error) {
	if p, lookErr := exec.LookPath("ludus"); lookErr == nil {
		return p, nil
	}

	fmt.Println("ludus CLI not found in PATH, installing...")

	version, err := latestVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("determine latest ludus version: %w", err)
	}
	fmt.Printf("  latest version: %s\n", version)

	asset, err := clientAssetName(version)
	if err != nil {
		return "", err
	}

	tmpName, err := downloadAndVerify(ctx, version, asset)
	if err != nil {
		return "", err
	}
	defer func() {
		if rerr := os.Remove(tmpName); rerr != nil && !os.IsNotExist(rerr) {
			fmt.Printf("  warning: failed to remove temp file %s: %v\n", tmpName, rerr)
		}
	}()

	return installLudusBinary(tmpName)
}

// downloadAndVerify fetches the ludus binary into a temp file and validates
// its checksum. Returns the temp file path on success; the caller is
// responsible for removing it.
func downloadAndVerify(ctx context.Context, version, asset string) (string, error) {
	expectedHash, err := fetchExpectedChecksum(ctx, version, asset)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "ludus-client-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()

	url := downloadURL(version, asset)
	fmt.Printf("  downloading %s\n", asset)

	if dlErr := downloadFile(ctx, url, tmpFile); dlErr != nil {
		closeAndRemove(tmpFile, tmpName)
		return "", fmt.Errorf("download ludus binary: %w", dlErr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		if rerr := os.Remove(tmpName); rerr != nil && !os.IsNotExist(rerr) {
			return "", fmt.Errorf("close ludus temp file: %w (remove: %v)", cerr, rerr)
		}
		return "", fmt.Errorf("close ludus temp file: %w", cerr)
	}

	if err := verifyChecksum(tmpName, expectedHash); err != nil {
		if rerr := os.Remove(tmpName); rerr != nil && !os.IsNotExist(rerr) {
			return "", fmt.Errorf("%w (remove: %v)", err, rerr)
		}
		return "", err
	}
	return tmpName, nil
}

// verifyChecksum compares the file's SHA-256 with the expected hash. An empty
// expectedHash skips verification (best-effort for older releases).
func verifyChecksum(path, expectedHash string) error {
	if expectedHash == "" {
		return nil
	}
	actualHash, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	fmt.Println("  checksum verified")
	return nil
}

// closeAndRemove tries to close a temp file and remove it; surfacing any
// failures via stderr since the caller is already returning a primary error.
func closeAndRemove(f *os.File, name string) {
	if cerr := f.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "warning: close %s: %v\n", name, cerr)
	}
	if rerr := os.Remove(name); rerr != nil && !os.IsNotExist(rerr) {
		fmt.Fprintf(os.Stderr, "warning: remove %s: %v\n", name, rerr)
	}
}

// installLudusBinary copies the staged binary into the install directory,
// makes it executable, and reports the final location.
func installLudusBinary(srcPath string) (string, error) {
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create install directory %s: %w", dir, err)
	}

	destName := "ludus"
	if runtime.GOOS == "windows" {
		destName = "ludus.exe"
	}
	dest := filepath.Join(dir, destName)

	if err := copyFile(srcPath, dest); err != nil {
		return "", fmt.Errorf("install ludus to %s: %w", dest, err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return "", fmt.Errorf("chmod ludus binary: %w", err)
	}

	fmt.Printf("  installed to %s\n", dest)

	if p, lookErr := exec.LookPath("ludus"); lookErr == nil {
		return p, nil
	}
	fmt.Printf("  warning: %s is not in your PATH, add it to use ludus directly\n", dir)
	return dest, nil
}

// fetchExpectedChecksum downloads the checksums file and extracts the hash
// for the given asset. Returns empty string if checksums are unavailable.
func fetchExpectedChecksum(ctx context.Context, version, asset string) (hash string, err error) {
	url := checksumsURL(version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil //nolint:nilerr // checksums are best-effort
	}
	resp, doErr := http.DefaultClient.Do(req)
	if doErr != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			if cerr := resp.Body.Close(); cerr != nil {
				return "", nil //nolint:nilerr // best-effort
			}
		}
		return "", nil // checksums unavailable, skip verification
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close checksums response: %w", cerr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil //nolint:nilerr // best-effort
	}

	for _, line := range strings.Split(string(body), "\n") {
		// Format: "<hash>  <filename>"
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", nil // asset not found in checksums, skip verification
}

func downloadFile(ctx context.Context, url string, dest *os.File) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	_, err = io.Copy(dest, resp.Body)
	return err
}

func fileSHA256(path string) (digest string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := in.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}
