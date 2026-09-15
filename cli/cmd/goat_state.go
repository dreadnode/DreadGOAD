package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const goatStateRelativePath = ".dreadgoad/state/azure/goat"

// goatStateRoot returns the stable, per-user backend root used by GOAT.
// Keeping this outside a checkout means deleting or replacing the checkout does
// not discard ownership of live Azure resources.
func goatStateRoot(homeDir string) string {
	return filepath.Join(homeDir, goatStateRelativePath)
}

func checkoutGOATStateRoot(projectRoot string) string {
	return filepath.Join(projectRoot, goatStateRelativePath)
}

// prepareGOATState creates and secures the stable state root and migrates the
// checkout-local layout used by early GOAT builds. It refuses to choose
// between two populated roots so an operator cannot silently switch away from
// the state that owns a live range.
func prepareGOATState(projectRoot, homeDir string) (string, error) {
	if !filepath.IsAbs(projectRoot) || !filepath.IsAbs(homeDir) {
		return "", fmt.Errorf("GOAT state paths must be absolute")
	}

	stableRoot := goatStateRoot(homeDir)
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

	checkoutRoot := checkoutGOATStateRoot(projectRoot)
	checkoutExists, err := validateCheckoutStateRoot(projectRoot, checkoutRoot)
	if err != nil {
		return "", err
	}
	stableExists, err := directoryExistsWithoutSymlink(stableRoot)
	if err != nil {
		return "", err
	}

	if checkoutExists {
		stableExists, err = migrateCheckoutGOATState(checkoutRoot, stableRoot, stableExists)
		if err != nil {
			return "", err
		}
	}

	if !stableExists {
		if err := ensurePrivateDirectory(stableRoot); err != nil {
			return "", err
		}
	}
	if err := secureGOATState(stableRoot); err != nil {
		return "", err
	}
	return stableRoot, nil
}

func migrateCheckoutGOATState(checkoutRoot, stableRoot string, stableExists bool) (bool, error) {
	if !stableExists {
		if err := os.Rename(checkoutRoot, stableRoot); err != nil {
			return false, fmt.Errorf("migrate GOAT state from %s to %s: %w", checkoutRoot, stableRoot, err)
		}
		return true, nil
	}

	checkoutHasState := hasTerraformState(checkoutRoot)
	if checkoutHasState && hasTerraformState(stableRoot) {
		return true, fmt.Errorf(
			"GOAT state exists in both %s and %s; refusing to select one automatically",
			checkoutRoot, stableRoot,
		)
	}
	if !checkoutHasState {
		return true, nil
	}

	empty, err := directoryEmpty(stableRoot)
	if err != nil {
		return true, err
	}
	if !empty {
		return true, fmt.Errorf(
			"checkout-local GOAT state exists at %s but destination %s is not empty; refusing to overwrite it",
			checkoutRoot, stableRoot,
		)
	}
	if err := os.Remove(stableRoot); err != nil {
		return true, fmt.Errorf("remove empty GOAT state destination %s: %w", stableRoot, err)
	}
	if err := os.Rename(checkoutRoot, stableRoot); err != nil {
		return false, fmt.Errorf("migrate GOAT state from %s to %s: %w", checkoutRoot, stableRoot, err)
	}
	return true, nil
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

// validateCheckoutStateRoot rejects symlinks anywhere beneath the checkout's
// .dreadgoad directory before migration. This prevents a repository-controlled
// link from redirecting the state move to an unrelated path.
func validateCheckoutStateRoot(projectRoot, checkoutRoot string) (bool, error) {
	path := projectRoot
	for _, element := range []string{".dreadgoad", "state", "azure", "goat"} {
		path = filepath.Join(path, element)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect checkout-local state path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, fmt.Errorf("checkout-local state path %s must be a real directory, not a symlink or file", path)
		}
	}
	return checkoutRoot == path, nil
}

func directoryEmpty(path string) (_ bool, resultErr error) {
	dir, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open state directory %s: %w", path, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, dir.Close())
	}()
	_, err = dir.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect state directory %s: %w", path, err)
	}
	return false, nil
}

// secureGOATState makes the backend tree private after every Terragrunt
// command. Parent directory permissions already prevent traversal while the
// command runs; this also ensures newly written state files are mode 0600.
func secureGOATState(root string) error {
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
