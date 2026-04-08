package ansible

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// InstallRequirements installs third-party Ansible collections and roles
// from requirements.yml.
func InstallRequirements(projectRoot string) error {
	reqFile := filepath.Join(projectRoot, "ansible", "requirements.yml")
	if _, err := os.Stat(reqFile); os.IsNotExist(err) {
		return nil
	}

	slog.Info("installing Ansible collection dependencies")
	cmd := exec.Command("ansible-galaxy", "collection", "install",
		"-r", reqFile, "--upgrade")
	cmd.Dir = filepath.Join(projectRoot, "ansible")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("collection dependency install failed: %w\n%s", err, out)
	}

	slog.Info("installing Ansible role dependencies")
	cmd = exec.Command("ansible-galaxy", "role", "install",
		"-r", reqFile, "--force")
	cmd.Dir = filepath.Join(projectRoot, "ansible")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("role dependency install failed: %w\n%s", err, out)
	}

	return nil
}

// BuildCollection builds and installs the dreadnode.goad Ansible collection
// from the local source so that playbooks always run the latest role code.
func BuildCollection(projectRoot string) error {
	ansibleDir := filepath.Join(projectRoot, "ansible")

	// Remove stale tarballs before building so the glob after build
	// always picks up the freshly built artifact.
	oldTarballs, _ := filepath.Glob(filepath.Join(ansibleDir, "dreadnode-goad-*.tar.gz"))
	for _, f := range oldTarballs {
		_ = os.Remove(f)
	}

	// Remove .ansible directories from roles that are left behind by
	// ansible-galaxy and can interfere with a clean collection build.
	rolesDir := filepath.Join(ansibleDir, "roles")
	if entries, err := os.ReadDir(rolesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dotAnsible := filepath.Join(rolesDir, e.Name(), ".ansible")
			if _, err := os.Stat(dotAnsible); err == nil {
				_ = os.RemoveAll(dotAnsible)
			}
		}
	}

	slog.Info("building dreadnode.goad collection")
	build := exec.Command("ansible-galaxy", "collection", "build", "--force")
	build.Dir = ansibleDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("collection build failed: %w\n%s", err, out)
	}

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

		if _, err := os.Stat(zipPath); err == nil {
			continue
		}

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
