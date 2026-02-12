package docker

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
)

// HostSiCodexProfileMounts returns bind mounts that make the host's full ~/.si
// tree visible inside a container's HOME.
//
// This enables the complete `si` command surface inside containers, including
// `si vault` subcommands that rely on ~/.si state (settings, trust store, keys,
// logs, and provider context).
func HostSiCodexProfileMounts(containerHome string) []mount.Mount {
	containerHome = strings.TrimSpace(containerHome)
	if containerHome == "" {
		return nil
	}
	source, ok := hostSiDirSource()
	if !ok {
		return nil
	}
	return []mount.Mount{{
		Type:   mount.TypeBind,
		Source: source,
		Target: path.Join(containerHome, ".si"),
	}}
}

// HostVaultEnvFileMount returns a bind mount that exposes the configured host
// vault env file at the same absolute path inside the container.
func HostVaultEnvFileMount(hostFile string) (mount.Mount, bool) {
	source, ok := hostVaultEnvFileSource(hostFile)
	if !ok {
		return mount.Mount{}, false
	}
	return mount.Mount{
		Type:   mount.TypeBind,
		Source: source,
		Target: filepath.ToSlash(source),
	}, true
}

// HasHostSiMount reports whether container info includes the host ~/.si bind
// mount at <containerHome>/.si. If host ~/.si is unavailable, this returns true.
func HasHostSiMount(info *types.ContainerJSON, containerHome string) bool {
	source, required := hostSiDirSource()
	if !required {
		return true
	}
	containerHome = strings.TrimSpace(containerHome)
	if info == nil || containerHome == "" {
		return false
	}
	target := path.Join(containerHome, ".si")
	for _, point := range info.Mounts {
		if !strings.EqualFold(strings.TrimSpace(string(point.Type)), "bind") {
			continue
		}
		pointSource := filepath.Clean(strings.TrimSpace(point.Source))
		pointTarget := filepath.ToSlash(strings.TrimSpace(point.Destination))
		if pointSource == source && pointTarget == target {
			return true
		}
	}
	return false
}

// HasHostVaultEnvFileMount reports whether container info includes the host
// vault env file bind mount at the same absolute path. If the host vault file
// path is empty/unavailable, this returns true.
func HasHostVaultEnvFileMount(info *types.ContainerJSON, hostFile string) bool {
	source, required := hostVaultEnvFileSource(hostFile)
	if !required {
		return true
	}
	if info == nil {
		return false
	}
	target := filepath.ToSlash(source)
	for _, point := range info.Mounts {
		if !strings.EqualFold(strings.TrimSpace(string(point.Type)), "bind") {
			continue
		}
		pointSource := filepath.Clean(strings.TrimSpace(point.Source))
		pointTarget := filepath.ToSlash(strings.TrimSpace(point.Destination))
		if pointSource == source && pointTarget == target {
			return true
		}
	}
	return false
}

func hostSiDirSource() (string, bool) {
	hostHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(hostHome) == "" {
		return "", false
	}
	siDir := filepath.Clean(filepath.Join(hostHome, ".si"))
	if !isDir(siDir) {
		return "", false
	}
	return siDir, true
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func hostVaultEnvFileSource(pathHint string) (string, bool) {
	pathHint = filepath.Clean(strings.TrimSpace(pathHint))
	if pathHint == "" {
		return "", false
	}
	pathHint = filepath.ToSlash(pathHint)
	if !strings.HasPrefix(pathHint, "/") {
		return "", false
	}
	pathHint = filepath.Clean(pathHint)
	if !isFile(pathHint) {
		return "", false
	}
	return pathHint, true
}
