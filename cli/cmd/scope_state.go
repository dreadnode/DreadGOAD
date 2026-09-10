package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const scopeStateRelativePath = ".dreadgoad/state/azure/scope-range"

// scopeStateRoot returns the stable, per-user backend root used by SCOPE-RANGE.
// Keeping this outside a checkout means deleting or replacing the checkout does
// not discard ownership of live Azure resources.
func scopeStateRoot(homeDir string) string {
	return filepath.Join(homeDir, scopeStateRelativePath)
}

func legacyScopeStateRoot(projectRoot string) string {
	return filepath.Join(projectRoot, scopeStateRelativePath)
}

// prepareScopeState creates and secures the stable state root and migrates the
// checkout-local layout used by early SCOPE-RANGE builds. It refuses to choose
// between two populated roots so an operator cannot silently switch away from
// the state that owns a live range.
func prepareScopeState(projectRoot, homeDir string) (string, error) {
	if !filepath.IsAbs(projectRoot) || !filepath.IsAbs(homeDir) {
		return "", fmt.Errorf("SCOPE-RANGE state paths must be absolute")
	}

	stableRoot := scopeStateRoot(homeDir)
	stableParent := filepath.Dir(stableRoot)
	for _, dir := range []string{
		filepath.Join(homeDir, ".dreadgoad"),
		filepath.Join(homeDir, ".dreadgoad", "state"),
		stableParent,
	} {
		if err := ensurePrivateDirectory(dir); err != nil {
			return "", err
		}
	}

	legacyRoot := legacyScopeStateRoot(projectRoot)
	legacyExists, err := validateLegacyStateRoot(projectRoot, legacyRoot)
	if err != nil {
		return "", err
	}
	stableExists, err := directoryExistsWithoutSymlink(stableRoot)
	if err != nil {
		return "", err
	}

	if legacyExists {
		switch {
		case !stableExists:
			if err := os.Rename(legacyRoot, stableRoot); err != nil {
				return "", fmt.Errorf("migrate SCOPE-RANGE state from %s to %s: %w", legacyRoot, stableRoot, err)
			}
			stableExists = true
		case hasTerraformState(legacyRoot) && hasTerraformState(stableRoot):
			return "", fmt.Errorf(
				"SCOPE-RANGE state exists in both %s and %s; refusing to select one automatically",
				legacyRoot, stableRoot,
			)
		case hasTerraformState(legacyRoot):
			empty, emptyErr := directoryEmpty(stableRoot)
			if emptyErr != nil {
				return "", emptyErr
			}
			if !empty {
				return "", fmt.Errorf(
					"legacy SCOPE-RANGE state exists at %s but destination %s is not empty; refusing to overwrite it",
					legacyRoot, stableRoot,
				)
			}
			if err := os.Remove(stableRoot); err != nil {
				return "", fmt.Errorf("remove empty SCOPE-RANGE state destination %s: %w", stableRoot, err)
			}
			if err := os.Rename(legacyRoot, stableRoot); err != nil {
				return "", fmt.Errorf("migrate SCOPE-RANGE state from %s to %s: %w", legacyRoot, stableRoot, err)
			}
		}
	}

	if !stableExists {
		if err := ensurePrivateDirectory(stableRoot); err != nil {
			return "", err
		}
	}
	if err := secureScopeState(stableRoot); err != nil {
		return "", err
	}
	return stableRoot, nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create private state directory %s: %w", path, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect state directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("state path %s must be a real directory, not a symlink or file", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure state directory %s: %w", path, err)
	}
	return nil
}

func directoryExistsWithoutSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect state path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("state path %s must be a real directory, not a symlink or file", path)
	}
	return true, nil
}

// validateLegacyStateRoot rejects symlinks anywhere beneath the checkout's
// .dreadgoad directory before migration. This prevents a repository-controlled
// link from redirecting the state move to an unrelated path.
func validateLegacyStateRoot(projectRoot, legacyRoot string) (bool, error) {
	path := projectRoot
	for _, element := range []string{".dreadgoad", "state", "azure", "scope-range"} {
		path = filepath.Join(path, element)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect legacy state path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, fmt.Errorf("legacy state path %s must be a real directory, not a symlink or file", path)
		}
	}
	return legacyRoot == path, nil
}

func directoryEmpty(path string) (bool, error) {
	dir, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open state directory %s: %w", path, err)
	}
	defer dir.Close()
	_, err = dir.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect state directory %s: %w", path, err)
	}
	return false, nil
}

// secureScopeState makes the backend tree private after every Terragrunt
// command. Parent directory permissions already prevent traversal while the
// command runs; this also ensures newly written state files are mode 0600.
func secureScopeState(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("state tree contains unexpected symlink: %s", path)
		}
		mode := fs.FileMode(0o600)
		if entry.IsDir() {
			mode = 0o700
		} else if !entry.Type().IsRegular() {
			return fmt.Errorf("state tree contains unsupported file type: %s", path)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("secure state path %s: %w", path, err)
		}
		return nil
	})
}
