package ansible

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMSSQLInstallerUsesStableDownloadAndRejectsStaleFiles(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	roleRoot := filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "..", "ansible", "roles", "mssql",
	)

	defaults, err := os.ReadFile(filepath.Join(roleRoot, "defaults", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	defaultsSource := string(defaults)
	if !strings.Contains(defaultsSource, "download_url_2019: https://go.microsoft.com/fwlink/?linkid=866658") {
		t.Fatal("SQL Server 2019 Express does not use Microsoft's stable forwarding link")
	}
	if strings.Contains(defaultsSource, "7f8a9c43-8c8a-4f7c-9f92-83c18d96b681") {
		t.Fatal("retired SQL Server 2019 Express download URL is still configured")
	}

	install, err := os.ReadFile(filepath.Join(roleRoot, "tasks", "install.yml"))
	if err != nil {
		t.Fatal(err)
	}
	installSource := string(install)
	for _, required := range []string{
		"force: true",
		"timeout: 600",
		"retries: 3",
		"until: installer_download is succeeded",
		"(installer_file.stat.size | default(0) | int) < 1048576",
	} {
		if !strings.Contains(installSource, required) {
			t.Errorf("MSSQL installer download is missing %q", required)
		}
	}
	if strings.Contains(installSource, "when: not installer_file.stat.exists") {
		t.Fatal("MSSQL installer download still trusts a possibly stale partial file")
	}

	readme, err := os.ReadFile(filepath.Join(roleRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readmeSource := string(readme)
	for _, required := range []string{
		"`https://go.microsoft.com/fwlink/?linkid=866658`",
		"**Validate SQL Server bootstrap installer**",
	} {
		if !strings.Contains(readmeSource, required) {
			t.Errorf("MSSQL role README is missing %q", required)
		}
	}
	if strings.Contains(readmeSource, "**Check if installation media already exists**") {
		t.Fatal("MSSQL role README still documents the removed stale-file shortcut")
	}
}
