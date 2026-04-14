package ansible

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildCollection builds and installs the dreadnode.goad Ansible collection
// from the local source so that playbooks always run the latest role code.
func BuildCollection(projectRoot string) error {
	ansibleDir := filepath.Join(projectRoot, "ansible")

	// Build the collection tarball
	slog.Info("building dreadnode.goad collection")
	build := exec.Command("ansible-galaxy", "collection", "build", "--force")
	build.Dir = ansibleDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("collection build failed: %w\n%s", err, out)
	}

	// Find the built tarball (name includes the version from galaxy.yml)
	matches, err := filepath.Glob(filepath.Join(ansibleDir, "dreadnode-goad-*.tar.gz"))
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("collection tarball not found in %s", ansibleDir)
	}
	tarball := matches[0]
	slog.Info("installing dreadnode.goad collection")
	install := exec.Command("ansible-galaxy", "collection", "install", tarball,
		"-p", filepath.Join(os.Getenv("HOME"), ".ansible", "collections"),
		"--force")
	install.Dir = ansibleDir
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("collection install failed: %w\n%s", err, out)
	}

	// Clean up tarball
	if err := os.Remove(tarball); err != nil {
		slog.Warn("failed to remove collection tarball", "path", tarball, "error", err)
	}
	return nil
}

// PrepareADCSZips creates the ADCSTemplate.zip files needed by ADCS roles.
func PrepareADCSZips(projectRoot string) error {
	dirs := []string{
		filepath.Join(projectRoot, "ansible", "roles", "adcs_templates", "files"),
		filepath.Join(projectRoot, "ansible", "roles", "vulns_adcs_templates", "files"),
	}

	for _, dir := range dirs {
		zipPath := filepath.Join(dir, "ADCSTemplate.zip")
		templateDir := filepath.Join(dir, "ADCSTemplate")

		// Skip if zip already exists
		if _, err := os.Stat(zipPath); err == nil {
			continue
		}

		// Skip if template dir doesn't exist
		if _, err := os.Stat(templateDir); os.IsNotExist(err) {
			continue
		}

		slog.Info("creating ADCS template zip", "dir", dir)
		cmd := exec.Command("zip", "-r", "ADCSTemplate.zip", "ADCSTemplate/")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			slog.Warn("failed to create ADCS zip", "dir", dir, "error", err, "output", string(output))
			return err
		}
	}
	return nil
}
