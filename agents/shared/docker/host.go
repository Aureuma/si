package docker

import (
	"os"
	"path/filepath"
	"runtime"
)

func AutoDockerHost() (string, bool) {
	if os.Getenv("DOCKER_HOST") != "" {
		return "", false
	}
	if defaultDockerSocketAvailable() {
		return "", false
	}
	host, ok := detectColimaHost()
	if ok {
		return host, true
	}
	return "", false
}

func defaultDockerSocketAvailable() bool {
	return socketExists("/var/run/docker.sock")
}

func detectColimaHost() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	candidate := filepath.Join(home, ".colima", "default", "docker.sock")
	if socketExists(candidate) {
		return "unix://" + candidate, true
	}
	return "", false
}

func socketExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}
