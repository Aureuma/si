package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// readFileScoped opens the parent directory as an os.Root and reads the named
// file from that root to avoid path traversal outside the intended directory.
func readFileScoped(path string) ([]byte, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	if resolved, err := resolveDotenvReadTarget(path); err != nil {
		return nil, err
	} else {
		path = resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(base)
}

func resolveDotenvReadTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	if !isTruthyEnv("SI_VAULT_ALLOW_SYMLINK_ENV_FILE") {
		return "", fmt.Errorf("refusing to read vault env file through symlink: %s (set SI_VAULT_ALLOW_SYMLINK_ENV_FILE=1 to override)", filepath.Clean(path))
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve vault env symlink %s: %w", filepath.Clean(path), err)
	}
	resolved = filepath.Clean(resolved)
	targetInfo, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if targetInfo.IsDir() {
		return "", fmt.Errorf("vault env symlink resolves to directory: %s", resolved)
	}
	return resolved, nil
}
