package docker

import (
	"os"
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
			if socketExists(path) {
				return path, true
			}
		}
	}
	if socketExists(defaultDockerSocketPath) {
		return defaultDockerSocketPath, true
	}
	if host, ok := detectColimaHost(); ok {
		path := strings.TrimPrefix(host, "unix://")
		if socketExists(path) {
			return path, true
		}
	}
	return "", false
}
