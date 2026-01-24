package docker

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// InferWorkspaceTarget mirrors a host path under /home/si when it's inside the user's home dir.
func InferWorkspaceTarget(hostPath string) (string, bool) {
	hostPath = strings.TrimSpace(hostPath)
	if hostPath == "" {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	rel, err := filepath.Rel(home, hostPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	return path.Join("/home/si", rel), true
}
