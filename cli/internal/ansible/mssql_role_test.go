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

func TestMSSQLSecretsAreTransportedWithoutPowerShellInterpolation(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	rolesRoot := filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "..", "ansible", "roles",
	)
	playbooksRoot := filepath.Join(rolesRoot, "..", "playbooks")

	servers, err := os.ReadFile(filepath.Join(playbooksRoot, "servers.yml"))
	if err != nil {
		t.Fatal(err)
	}
	serversSource := string(servers)
	diagnosticStart := strings.Index(serversSource, "- name: Display non-sensitive MSSQL installation variables")
	if diagnosticStart < 0 {
		t.Fatal("could not locate the MSSQL diagnostic task")
	}
	diagnosticEnd := strings.Index(serversSource[diagnosticStart:], "\n  roles:")
	if diagnosticEnd < 0 {
		t.Fatal("could not locate the end of the MSSQL diagnostic task")
	}
	diagnosticBlock := serversSource[diagnosticStart : diagnosticStart+diagnosticEnd]
	for _, secret := range []string{"SQLSVCPASSWORD", "domain_admin_password", "sa_password", "linked_servers:"} {
		if strings.Contains(diagnosticBlock, secret) {
			t.Errorf("MSSQL diagnostic task logs secret-bearing field %q", secret)
		}
	}

	config, err := os.ReadFile(filepath.Join(rolesRoot, "mssql", "tasks", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	configSource := string(config)
	for _, unsafe := range []string{
		`$saPassword = "{{ sa_password }}"`,
		`PASSWORD = '{{ sa_password }}'`,
	} {
		if strings.Contains(configSource, unsafe) {
			t.Fatalf("MSSQL config still interpolates a secret directly into PowerShell: %s", unsafe)
		}
	}
	if strings.Contains(configSource, `$serviceName = "{{ mssql_service_name }}"`) {
		t.Fatal("MSSQL named-instance service name is vulnerable to PowerShell interpolation")
	}
	for _, line := range strings.Split(configSource, "\n") {
		delimiter := strings.TrimSpace(line)
		if delimiter == `@"` || delimiter == `"@` {
			t.Fatal("MSSQL role uses a PowerShell here-string inside an indented YAML block scalar")
		}
	}
	for _, required := range []string{
		`{{ sa_password | b64encode }}`,
		`[Convert]::FromBase64String`,
		`$saPassword.Replace("'", "''")`,
		`$serviceName = '{{ mssql_service_name }}'`,
		`[Security.Principal.WindowsIdentity]::GetCurrent().Name`,
		`-mSQLCMD`,
		`$sqlcmd -b -E`,
		`SQL sysadmin verification failed`,
	} {
		if !strings.Contains(configSource, required) {
			t.Errorf("MSSQL bootstrap is missing %q", required)
		}
	}

	linkedLogins, err := os.ReadFile(filepath.Join(rolesRoot, "mssql_link", "tasks", "logins.yml"))
	if err != nil {
		t.Fatal(err)
	}
	linkedSource := string(linkedLogins)
	if strings.Contains(linkedSource, `@rmtpassword = N'{{ mapping_item.remote_password }}'`) {
		t.Fatal("linked-server password is still interpolated directly into PowerShell")
	}
	for _, required := range []string{
		`{{ mapping_item.remote_password | b64encode }}`,
		`$remotePassword.Replace("'", "''")`,
		`@rmtpassword = N'$remotePasswordSql'`,
		`$sqlcmd -b -E`,
	} {
		if !strings.Contains(linkedSource, required) {
			t.Errorf("linked-server credential handling is missing %q", required)
		}
	}
}
