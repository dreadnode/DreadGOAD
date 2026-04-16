package helpers

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/modules/terraform-aws-instance-factory/test/types"
	"github.com/gruntwork-io/terratest/modules/random"
)

// RetryWithTimeout retries an operation until it succeeds or the timeout is reached.
// The operation is retried with a delay between attempts.
//
// **Parameters:**
//
// t: The testing object
// config: The RetryConfig object containing retry configuration
// operation: The RetryFunc function to be executed
//
// **Returns:**
//
// error: An error if the operation fails after the maximum number of retries
func RetryWithTimeout(t *testing.T, config types.RetryConfig, operation types.RetryFunc) error {
	for i := 0; i < config.MaxRetries; i++ {
		err := operation()
		if err == nil {
			return nil
		}

		if i == config.MaxRetries-1 {
			return fmt.Errorf("%s failed after %d attempts: %v", config.Description, config.MaxRetries, err)
		}

		t.Logf("Attempt %d: Waiting for %s...", i+1, config.Description)
		time.Sleep(config.Delay * time.Second)
	}
	return nil
}

// WriteTerraformVars writes variables to a tfvars file securely
//
// **Parameters:**
//
// filename: The name of the tfvars file to create
// vars: Map of variables to write
//
// **Returns:**
//
// error: An error if writing fails
func WriteTerraformVars(filename string, vars map[string]interface{}) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create tfvars file: %v", err)
	}
	defer file.Close()

	// Ensure the file is only readable by the current user
	if err := os.Chmod(filename, 0600); err != nil {
		return fmt.Errorf("failed to set tfvars file permissions: %v", err)
	}

	for k, v := range vars {
		switch val := v.(type) {
		case string:
			_, err = fmt.Fprintf(file, "%s = \"%s\"\n\n", k, val)
		case []string:
			// Format array of strings properly
			strValues := make([]string, len(val))
			for i, s := range val {
				strValues[i] = fmt.Sprintf("\"%s\"", s)
			}
			_, err = fmt.Fprintf(file, "%s = [%s]\n\n", k, strings.Join(strValues, ", "))
		case map[string]string:
			// Format map[string]string for HCL
			var pairs []string
			for mk, mv := range val {
				pairs = append(pairs, fmt.Sprintf("%s = \"%s\"", mk, mv))
			}
			_, err = fmt.Fprintf(file, "%s = {\n  %s\n}\n\n", k, strings.Join(pairs, "\n  "))
		case map[string]interface{}:
			// Handle nested maps by recursively converting them to HCL format
			var buf bytes.Buffer
			err = writeHCLMap(&buf, val, 1)
			if err != nil {
				return fmt.Errorf("failed to format map: %v", err)
			}
			_, err = fmt.Fprintf(file, "%s = %s\n\n", k, buf.String())
		default:
			_, err = fmt.Fprintf(file, "%s = %v\n\n", k, val)
		}
		if err != nil {
			return fmt.Errorf("failed to write to tfvars file: %v", err)
		}
	}

	return nil
}

// writeHCLMap writes a map to a writer in HCL format
func writeHCLMap(w io.Writer, m map[string]interface{}, indent int) error {
	_, err := fmt.Fprintln(w, "{")
	if err != nil {
		return err
	}

	indentStr := strings.Repeat("  ", indent)

	for k, v := range m {
		switch val := v.(type) {
		case string:
			_, err = fmt.Fprintf(w, "%s%s = \"%s\"\n", indentStr, k, val)
		case []string:
			strValues := make([]string, len(val))
			for i, s := range val {
				strValues[i] = fmt.Sprintf("\"%s\"", s)
			}
			_, err = fmt.Fprintf(w, "%s%s = [%s]\n", indentStr, k, strings.Join(strValues, ", "))
		case []interface{}:
			var items []string
			for _, item := range val {
				var buf bytes.Buffer
				if itemMap, ok := item.(map[string]interface{}); ok {
					if err := writeHCLMap(&buf, itemMap, indent+1); err != nil {
						return err
					}
					items = append(items, buf.String())
				}
			}
			_, err = fmt.Fprintf(w, "%s%s = [%s]\n", indentStr, k, strings.Join(items, ", "))
		case map[string]string:
			var pairs []string
			for mk, mv := range val {
				pairs = append(pairs, fmt.Sprintf("%s = \"%s\"", mk, mv))
			}
			_, err = fmt.Fprintf(w, "%s%s = {\n%s  %s\n%s}\n", indentStr, k, indentStr, strings.Join(pairs, "\n  "+indentStr), indentStr)
		case map[string]interface{}:
			_, err = fmt.Fprintf(w, "%s%s = ", indentStr, k)
			if err != nil {
				return err
			}
			err = writeHCLMap(w, val, indent+1)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(w)
		default:
			_, err = fmt.Fprintf(w, "%s%s = %v\n", indentStr, k, val)
		}
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, "%s}", strings.Repeat("  ", indent-1))
	return err
}

// CopyModuleToTempDir copies the entire module directory to a temporary directory
//
// **Parameters:**
//
// t: The testing object
// sourceDir: The source directory to copy
//
// **Returns:**
//
// string: The path to the temporary directory
// error: An error if copying fails
func CopyModuleToTempDir(t *testing.T, sourceDir string) (string, error) {
	// Create a unique temporary directory path
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("terraform-test-%s", random.UniqueId()))

	// Create directory if it doesn't exist
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp dir: %v", err)
	}

	absSourceDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %v", err)
	}
	moduleDir := filepath.Dir(absSourceDir)

	// Copy files, including Terraform state and provider files
	err = filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Calculate relative path
		relPath, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %v", err)
		}

		destPath := filepath.Join(tempDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		// Copy file
		input, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read source file %s: %v", path, err)
		}

		return os.WriteFile(destPath, input, info.Mode())
	})

	if err != nil {
		return "", fmt.Errorf("failed to copy module files: %v", err)
	}

	// Force Terraform to re-initialize with new versions
	err = os.RemoveAll(filepath.Join(tempDir, ".terraform"))
	if err != nil {
		return "", fmt.Errorf("failed to clean .terraform directory: %v", err)
	}

	err = os.RemoveAll(filepath.Join(tempDir, ".terraform.lock.hcl"))
	if err != nil {
		return "", fmt.Errorf("failed to clean lock file: %v", err)
	}

	return tempDir, nil
}
