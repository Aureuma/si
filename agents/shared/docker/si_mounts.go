package docker

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

// HostSiCodexProfileMounts returns bind mounts that make the host's Codex profile
// settings + auth cache visible inside a container's HOME.
//
// We intentionally mount only the minimal required surfaces (settings.toml and
// codex/profiles) instead of the entire ~/.si directory to avoid exposing vault
// materials to containers.
func HostSiCodexProfileMounts(containerHome string) []mount.Mount {
	containerHome = strings.TrimSpace(containerHome)
	if containerHome == "" {
		return nil
	}
	hostHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(hostHome) == "" {
		return nil
	}

	settingsPath := filepath.Join(hostHome, ".si", "settings.toml")
	profilesDir := filepath.Join(hostHome, ".si", "codex", "profiles")

	var mounts []mount.Mount
	if isRegularFile(settingsPath) {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   settingsPath,
			Target:   path.Join(containerHome, ".si", "settings.toml"),
			ReadOnly: true,
		})
	}
	if isDir(profilesDir) {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   profilesDir,
			Target:   path.Join(containerHome, ".si", "codex", "profiles"),
			ReadOnly: true,
		})
	}
	return mounts
}

func isRegularFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.IsDir()
}
