package docker

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
)

// InferWorkspaceTarget mirrors a host path under containerHome when it's inside
// the host user's home dir.
func InferWorkspaceTarget(hostPath, containerHome string) (string, bool) {
	hostPath = strings.TrimSpace(hostPath)
	containerHome = filepath.ToSlash(strings.TrimSpace(containerHome))
	if hostPath == "" || containerHome == "" || !strings.HasPrefix(containerHome, "/") {
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
	return path.Join(containerHome, rel), true
}

// InferDevelopmentMount returns a bind mount that exposes the host's full
// ~/Development directory at <containerHome>/Development when hostPath is
// inside that tree.
func InferDevelopmentMount(hostPath, containerHome string) (mount.Mount, bool) {
	hostPath = filepath.Clean(strings.TrimSpace(hostPath))
	containerHome = filepath.ToSlash(strings.TrimSpace(containerHome))
	if hostPath == "" || containerHome == "" || !strings.HasPrefix(hostPath, "/") || !strings.HasPrefix(containerHome, "/") {
		return mount.Mount{}, false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return mount.Mount{}, false
	}
	developmentHost := filepath.Join(home, "Development")
	rel, err := filepath.Rel(developmentHost, hostPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return mount.Mount{}, false
	}
	return mount.Mount{
		Type:   mount.TypeBind,
		Source: developmentHost,
		Target: path.Join(containerHome, "Development"),
	}, true
}

// HasDevelopmentMount reports whether container info includes the host
// ~/Development bind mount at <containerHome>/Development when required for the
// given host path.
func HasDevelopmentMount(info *types.ContainerJSON, hostPath, containerHome string) bool {
	required, ok := InferDevelopmentMount(hostPath, containerHome)
	if !ok {
		return true
	}
	if info == nil {
		return false
	}
	requiredSource := filepath.Clean(strings.TrimSpace(required.Source))
	requiredTarget := filepath.ToSlash(strings.TrimSpace(required.Target))
	for _, point := range info.Mounts {
		if !strings.EqualFold(strings.TrimSpace(string(point.Type)), "bind") {
			continue
		}
		source := filepath.Clean(strings.TrimSpace(point.Source))
		target := filepath.ToSlash(strings.TrimSpace(point.Destination))
		if source == requiredSource && target == requiredTarget {
			return true
		}
	}
	return false
}
