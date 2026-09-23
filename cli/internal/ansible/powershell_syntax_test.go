package ansible

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var ambiguousPowerShellColonVar = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*):`)
var allowedPowerShellScopes = map[string]bool{
	"alias": true, "env": true, "function": true, "global": true,
	"local": true, "private": true, "script": true, "using": true,
	"variable": true,
}

func TestPowerShellVariablesBeforeColonAreDelimited(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	ansibleRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "ansible")

	err := filepath.WalkDir(ansibleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".ps1" && ext != ".yml" && ext != ".yaml" {
			return nil
		}

		return checkPowerShellColonVars(t, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAmbiguousPowerShellColonVariableDetection(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{name: "ambiguous interpolation", line: `Write-Output "Secure channel to $domain: OK"`, want: []string{"domain"}},
		{name: "delimited interpolation", line: `Write-Output "Secure channel to ${domain}: OK"`},
		{name: "environment drive", line: `Remove-Item "$env:TEMP\\*"`},
		{name: "using scope", line: `Write-Output "$Using:DomainName"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ambiguousPowerShellColonVariables(tt.line); !slices.Equal(got, tt.want) {
				t.Errorf("ambiguous variables = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrustPeerExtractionAvoidsJinjaBackslashSplit(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	path := filepath.Join(
		filepath.Dir(sourceFile),
		"..", "..", "..", "ansible", "roles", "groups_domains", "tasks", "main.yml",
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)

	if strings.Contains(source, `m.split('\\')`) {
		t.Fatal("trust peer extraction regressed to Ansible's unreliable Jinja backslash split")
	}
	for _, required := range []string{
		`CurrentDomain: "{{ domain }}"`,
		`$member.IndexOf([char]92)`,
		`$peer = $member.Substring(0, $separator)`,
		`if ($peer -ine $CurrentDomain)`,
		`nltest /sc_verify:$domain`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("trust peer extraction is missing %q", required)
		}
	}
}

func ambiguousPowerShellColonVariables(line string) []string {
	var variables []string
	for _, match := range ambiguousPowerShellColonVar.FindAllStringSubmatch(line, -1) {
		if !allowedPowerShellScopes[strings.ToLower(match[1])] {
			variables = append(variables, match[1])
		}
	}
	return variables
}

func checkPowerShellColonVars(t *testing.T, path string) (err error) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		for _, variable := range ambiguousPowerShellColonVariables(scanner.Text()) {
			t.Errorf(
				"%s:%d: ambiguous PowerShell interpolation $%s:; use ${%s}: to delimit the variable",
				path, lineNumber, variable, variable,
			)
		}
	}
	return scanner.Err()
}
