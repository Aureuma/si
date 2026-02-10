package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cleanLocalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	return filepath.Clean(path), nil
}

func readLocalFile(path string) ([]byte, error) {
	path, err := cleanLocalPath(path)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- local CLI path handling intentionally supports variable paths.
	return os.ReadFile(path)
}

func openLocalFile(path string) (*os.File, error) {
	path, err := cleanLocalPath(path)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- local CLI path handling intentionally supports variable paths.
	return os.Open(path)
}

func openLocalFileFlags(path string, flags int, perm os.FileMode) (*os.File, error) {
	path, err := cleanLocalPath(path)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- local CLI path handling intentionally supports variable paths.
	return os.OpenFile(path, flags, perm)
}
