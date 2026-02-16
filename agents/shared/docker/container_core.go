package docker

import (
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

// ContainerCoreMountPlan defines the shared workspace + host-state mounts used
// across regular and dyad containers.
type ContainerCoreMountPlan struct {
	WorkspaceHost          string
	WorkspacePrimaryTarget string
	WorkspaceMirrorTarget  string
	ContainerHome          string
	IncludeHostSi          bool
	HostVaultEnvFile       string
}

// BuildContainerCoreMounts builds a normalized mount set for shared container
// behavior across codex and dyad flows.
func BuildContainerCoreMounts(plan ContainerCoreMountPlan) []mount.Mount {
	workspaceHost := filepath.Clean(strings.TrimSpace(plan.WorkspaceHost))
	if workspaceHost == "" || !strings.HasPrefix(workspaceHost, "/") {
		return nil
	}
	primary := filepath.ToSlash(strings.TrimSpace(plan.WorkspacePrimaryTarget))
	if primary == "" || !strings.HasPrefix(primary, "/") {
		primary = "/workspace"
	}
	mirror := filepath.ToSlash(strings.TrimSpace(plan.WorkspaceMirrorTarget))

	mounts := make([]mount.Mount, 0, 3)
	appendUniqueMount(&mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: workspaceHost,
		Target: primary,
	})
	if mirror != "" && mirror != primary && strings.HasPrefix(mirror, "/") {
		appendUniqueMount(&mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: workspaceHost,
			Target: mirror,
		})
	}
	if m, ok := InferDevelopmentMount(workspaceHost, plan.ContainerHome); ok {
		appendUniqueMount(&mounts, m)
	}
	if plan.IncludeHostSi {
		for _, m := range HostSiCodexProfileMounts(plan.ContainerHome) {
			appendUniqueMount(&mounts, m)
		}
	}
	if m, ok := HostVaultEnvFileMount(plan.HostVaultEnvFile); ok {
		appendUniqueMount(&mounts, m)
	}
	return mounts
}

func appendUniqueMount(dst *[]mount.Mount, next mount.Mount) {
	if dst == nil {
		return
	}
	src := filepath.Clean(strings.TrimSpace(next.Source))
	target := filepath.ToSlash(strings.TrimSpace(next.Target))
	mType := strings.TrimSpace(string(next.Type))
	if src == "" || target == "" || mType == "" {
		return
	}
	next.Source = src
	next.Target = target
	for _, existing := range *dst {
		if filepath.Clean(strings.TrimSpace(existing.Source)) == src &&
			filepath.ToSlash(strings.TrimSpace(existing.Target)) == target &&
			strings.EqualFold(strings.TrimSpace(string(existing.Type)), mType) {
			return
		}
	}
	*dst = append(*dst, next)
}
