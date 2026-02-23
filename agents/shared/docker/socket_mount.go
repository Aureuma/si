package docker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

const defaultDockerSocketPath = "/var/run/docker.sock"

// DockerSocketMount returns a bind mount for the host Docker socket if available.
func DockerSocketMount() (mount.Mount, bool) {
	source, ok := dockerSocketPath()
	if !ok {
		return mount.Mount{}, false
	}
	return mount.Mount{
		Type:   mount.TypeBind,
		Source: source,
		Target: defaultDockerSocketPath,
	}, true
}

func dockerSocketPath() (string, bool) {
	if host := strings.TrimSpace(os.Getenv("DOCKER_HOST")); host != "" {
		if strings.HasPrefix(host, "unix://") {
			path := strings.TrimPrefix(host, "unix://")
			if isColimaUnixSocketPath(path) {
				// Colima exposes a client-side socket under ~/.colima/.../docker.sock.
				// Docker bind sources are resolved by the daemon host, so use the
				// daemon-local socket path instead of the client-side socket path.
				return defaultDockerSocketPath, true
			}
			if socketExists(path) {
				return path, true
			}
		}
	}
	if socketExists(defaultDockerSocketPath) {
		return defaultDockerSocketPath, true
	}
	if _, ok := detectColimaHost(); ok {
		return defaultDockerSocketPath, true
	}
	return "", false
}

func isColimaUnixSocketPath(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || filepath.Base(path) != "docker.sock" {
		return false
	}
	colimaHome := strings.TrimSpace(os.Getenv("COLIMA_HOME"))
	if colimaHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return false
		}
		colimaHome = filepath.Join(home, ".colima")
	}
	colimaHome = filepath.Clean(strings.TrimSpace(colimaHome))
	if colimaHome == "" {
		return false
	}
	rel, err := filepath.Rel(colimaHome, path)
	if err != nil {
		return false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
