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
