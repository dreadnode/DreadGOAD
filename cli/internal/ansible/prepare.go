package ansible

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

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
