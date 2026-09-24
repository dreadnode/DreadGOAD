package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

func TestEnsureAWSInventoryTransportRepairsAndValidatesContract(t *testing.T) {
	cfg := &config.Config{
		Env: "range", ProjectRoot: t.TempDir(), Provider: "aws", Region: "us-east-2",
	}
	body := "[default]\n" +
		"dc01 ansible_host=i-0123456789abcdef0 dict_key=dc01 " +
		"ansible_shell_type=cmd ansible_become=true ansible_remote_tmp=/tmp\n\n" +
		"lx01 ansible_host=10.0.1.12 dict_key=lx01 ansible_connection='SSH'\n\n" +
		"[all:vars]\n" +
		"ansible_connection=winrm\n" +
		"ansible_connection=ssh\n" +
		"ansible_aws_ssm_region=wrong-region\n" +
		"ansible_shell_type=powershell\n" +
		"ansible_become=false\n" +
		`ansible_remote_tmp=C:\Windows\Temp` + "\n" +
		"ansible_aws_ssm_bucket_name=existing-bucket\n" +
		"ansible_aws_ssm_retries=7\n"
	writeInventoryTestFile(t, cfg.InventoryPath(), body, 0o640)

	if err := ensureAWSInventoryTransport(cfg); err != nil {
		t.Fatal(err)
	}
	got := readInventoryForTest(t, cfg.InventoryPath())
	for _, want := range []string{
		"ansible_connection=amazon.aws.aws_ssm",
		"ansible_aws_ssm_region=us-east-2",
		"ansible_shell_type=powershell",
		"ansible_become=false",
		"ansible_aws_ssm_bucket_name=existing-bucket",
		"ansible_aws_ssm_s3_addressing_style=virtual",
		"ansible_aws_ssm_retries=7",
		`ansible_remote_tmp='C:\Windows\Temp'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repaired inventory is missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "ansible_connection=amazon.aws.aws_ssm") != 2 {
		t.Errorf("duplicate stale connection assignments were not both repaired:\n%s", got)
	}
	_, globals := inventoryAllVars(got)
	for _, variable := range awsWindowsHostVariables {
		if _, exists := globals[variable.key]; exists {
			t.Errorf("Windows-only %s remained global:\n%s", variable.key, got)
		}
		if value := hostVariableForTest(t, got, "dc01", variable.key); value != variable.value {
			t.Errorf("dc01 %s = %q, want %q", variable.key, value, variable.value)
		}
		if value := hostVariableForTest(t, got, "lx01", variable.key); value != "" {
			t.Errorf("SSH host inherited %s=%q:\n%s", variable.key, value, got)
		}
	}
	if info, err := os.Stat(cfg.InventoryPath()); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("inventory mode changed: info=%v err=%v", info, err)
	}

	first := got
	if err := ensureAWSInventoryTransport(cfg); err != nil {
		t.Fatal(err)
	}
	if second := readInventoryForTest(t, cfg.InventoryPath()); second != first {
		t.Errorf("second reconciliation was not byte-idempotent")
	}
}

func hostVariableForTest(t *testing.T, content, host, key string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), host+" ") {
			return inventoryHostVariable(line, key)
		}
	}
	t.Fatalf("host %s not found in inventory:\n%s", host, content)
	return ""
}

func TestEnsureAWSInventoryTransportAddsMissingSectionAndAutomaticBucket(t *testing.T) {
	cfg := &config.Config{
		Env: "range", ProjectRoot: t.TempDir(), Provider: "aws", Region: "us-west-1",
	}
	writeInventoryTestFile(t, cfg.InventoryPath(),
		"[default]\ndc01 ansible_host=i-0123456789abcdef0 dict_key=dc01\n", 0o644)

	if err := ensureAWSInventoryTransport(cfg); err != nil {
		t.Fatal(err)
	}
	got := readInventoryForTest(t, cfg.InventoryPath())
	for _, want := range []string{
		"[all:vars]", "ansible_connection=amazon.aws.aws_ssm",
		"ansible_aws_ssm_region=us-west-1", "ansible_aws_ssm_bucket_name=AUTO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repaired inventory is missing %q:\n%s", want, got)
		}
	}
}

func TestEnsureAWSInventoryTransportDoesNotTouchNonAWSInventory(t *testing.T) {
	cfg := &config.Config{
		Env: "range", ProjectRoot: t.TempDir(), Provider: "azure", Region: "centralus",
	}
	body := "[all:vars]\nansible_connection=winrm\n"
	writeInventoryTestFile(t, cfg.InventoryPath(), body, 0o644)

	if err := ensureAWSInventoryTransport(cfg); err != nil {
		t.Fatal(err)
	}
	if got := readInventoryForTest(t, cfg.InventoryPath()); got != body {
		t.Errorf("non-AWS inventory changed:\n%s", got)
	}
}

func TestEnsureAWSInventoryTransportMissingRegionDoesNotMutateInventory(t *testing.T) {
	cfg := &config.Config{Env: "range", ProjectRoot: t.TempDir(), Provider: "aws"}
	body := "[all:vars]\nansible_connection=winrm\n"
	writeInventoryTestFile(t, cfg.InventoryPath(), body, 0o640)

	err := ensureAWSInventoryTransport(cfg)
	if err == nil || !strings.Contains(err.Error(), "cloud region not configured") {
		t.Fatalf("error = %v, want missing cloud region", err)
	}
	if got := readInventoryForTest(t, cfg.InventoryPath()); got != body {
		t.Errorf("inventory changed despite unresolved region:\n%s", got)
	}
}

func TestValidateAWSInventoryTransportRejectsIncompleteContract(t *testing.T) {
	err := validateAWSInventoryTransportContent(
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n",
		"us-east-1",
	)
	if err == nil || !strings.Contains(err.Error(), "ansible_aws_ssm_region") {
		t.Fatalf("validation error = %v, want missing region", err)
	}
}

func TestProviderInstanceUpdatesPreservesBothAddressForms(t *testing.T) {
	discovered := []provider.Instance{{
		ID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
	}}

	updates := providerInstanceUpdates(discovered)
	if len(updates) != 1 || updates[0].InstanceID != discovered[0].ID ||
		updates[0].PrivateIP != discovered[0].PrivateIP {
		t.Fatalf("provider update = %#v, want both address forms preserved", updates)
	}
}

func TestLoadInstancesJSONUsesEffectiveAWSHostTransport(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "instances.json")
	body := `[` +
		`{"InstanceId":"i-0123456789abcdef0","Name":"dreadgoad-dc01","PrivateIP":"10.0.1.10"},` +
		`{"InstanceId":"i-0fedcba9876543210","Name":"dreadgoad-lx01","PrivateIP":"10.0.1.11"}` +
		`]`
	if err := os.WriteFile(jsonPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	awsCfg := &config.Config{Env: "range", ProjectRoot: dir, Provider: "aws"}
	writeInventoryTestFile(t, awsCfg.InventoryPath(), "[default]\n"+
		"dc01 ansible_host=10.0.9.10 dict_key=dc01\n"+
		"lx01 ansible_host=i-wrong dict_key=lx01 ansible_connection=ssh\n\n"+
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n", 0o640)
	aws, err := loadInstances(t.Context(), jsonPath, awsCfg.InventoryPath(), awsCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyInstanceUpdatesForProvider(awsCfg.InventoryPath(), aws, true); err != nil {
		t.Fatal(err)
	}
	got := readInventoryForTest(t, awsCfg.InventoryPath())
	for _, want := range []string{
		"dc01 ansible_host=i-0123456789abcdef0",
		"lx01 ansible_host=10.0.1.11",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transport-aware AWS inventory is missing %q:\n%s", want, got)
		}
	}
	if info, err := os.Stat(awsCfg.InventoryPath()); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("inventory mode changed: info=%v err=%v", info, err)
	}
}

func TestApplyAWSInstanceUpdatesRepairsBlankAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	writeInventoryTestFile(t, path, "[default]\n"+
		"dc01 ansible_host= dict_key=dc01\n\n"+
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n", 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
		InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readInventoryForTest(t, path); !strings.Contains(got, "dc01 ansible_host=i-0123456789abcdef0") {
		t.Errorf("blank AWS address was not repaired:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesRepairsIndentedReorderedAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	writeInventoryTestFile(t, path, "[default]\n"+
		"  dc01 dict_key=dc01 ansible_host= dns_domain=dc01\n\n"+
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n", 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
		InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readInventoryForTest(t, path); !strings.Contains(
		got, "  dc01 dict_key=dc01 ansible_host=i-0123456789abcdef0 dns_domain=dc01",
	) {
		t.Errorf("indented reordered AWS address was not repaired:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesRejectsIncompleteDiscoveryWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	body := "[default]\n" +
		"dc01 ansible_host=PENDING dict_key=dc01\n" +
		"lx01 ansible_host=PENDING dict_key=lx01 ansible_connection=ssh\n\n" +
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n"
	writeInventoryTestFile(t, path, body, 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
		InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "lx01: no discovered instance") {
		t.Fatalf("error = %v, want missing lx01 discovery", err)
	}
	if got := readInventoryForTest(t, path); got != body {
		t.Errorf("inventory was partially mutated after incomplete discovery:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesRequiresPrivateIPForHostOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	body := "[default]\nlx01 ansible_host=PENDING dict_key=lx01 ansible_connection=ssh\n"
	writeInventoryTestFile(t, path, body, 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
		InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-lx01",
	}}, true)
	if err == nil || !strings.Contains(err.Error(), "ssh transport requires a discovered private IP") {
		t.Fatalf("error = %v, want missing private IP", err)
	}
	if got := readInventoryForTest(t, path); got != body {
		t.Errorf("inventory changed despite missing SSH address:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesUsesPrivateIPForExplicitNonSSMTransports(t *testing.T) {
	for _, connection := range []string{"ssh", "winrm", "psrp"} {
		t.Run(connection, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "inventory")
			writeInventoryTestFile(t, path, "[default]\n"+
				"dc01 ansible_host=i-stale dict_key=dc01 ansible_connection="+connection+"\n\n"+
				"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n", 0o644)

			err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
				InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
			}}, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := readInventoryForTest(t, path); !strings.Contains(got, "dc01 ansible_host=10.0.1.10") {
				t.Errorf("%s host did not receive its private IP:\n%s", connection, got)
			}
		})
	}
}

func TestApplyAWSInstanceUpdatesUsesInheritedNonSSMTransport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	writeInventoryTestFile(t, path, "[default]\n"+
		"dc01 ansible_host=i-stale dict_key=dc01\n\n"+
		"[all:vars]\nansible_connection=ssh\n", 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{{
		InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10",
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readInventoryForTest(t, path); !strings.Contains(got, "dc01 ansible_host=10.0.1.10") {
		t.Errorf("host inheriting SSH did not receive its private IP:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesRejectsAmbiguousDiscoveryWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	body := "[default]\ndc01 ansible_host=PENDING dict_key=dc01\n\n" +
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n"
	writeInventoryTestFile(t, path, body, 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{
		{InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10"},
		{InstanceID: "i-0fedcba9876543210", Name: "prefix-dreadgoad-dc01", PrivateIP: "10.0.1.11"},
	}, true)
	if err == nil || !strings.Contains(err.Error(), "map to the same inventory host") {
		t.Fatalf("error = %v, want ambiguous dc01 discovery", err)
	}
	if got := readInventoryForTest(t, path); got != body {
		t.Errorf("inventory was mutated after ambiguous discovery:\n%s", got)
	}
}

func TestApplyAWSInstanceUpdatesIgnoresAmbiguityForUnrelatedRoles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory")
	writeInventoryTestFile(t, path, "[default]\n"+
		"dc01 ansible_host=PENDING dict_key=dc01\n\n"+
		"[all:vars]\nansible_connection=amazon.aws.aws_ssm\n", 0o644)

	err := applyInstanceUpdatesForProvider(path, []instanceInfo{
		{InstanceID: "i-0123456789abcdef0", Name: "dreadgoad-dc01", PrivateIP: "10.0.1.10"},
		{InstanceID: "i-unused-1", Name: "dreadgoad-unused", PrivateIP: "10.0.1.20"},
		{InstanceID: "i-unused-2", Name: "prefix-dreadgoad-unused", PrivateIP: "10.0.1.21"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readInventoryForTest(t, path); !strings.Contains(got, "dc01 ansible_host=i-0123456789abcdef0") {
		t.Errorf("wanted host was not repaired:\n%s", got)
	}
}
