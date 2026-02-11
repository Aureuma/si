package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildContainerCoreMountsIncludesWorkspaceMirrorAndHostSi(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	workspace := t.TempDir()

	mounts := BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          workspace,
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  "/workspace-mirror",
		ContainerHome:          "/home/si",
		IncludeHostSi:          true,
	})
	if len(mounts) != 3 {
		t.Fatalf("expected 3 mounts, got %d: %+v", len(mounts), mounts)
	}
	if mounts[0].Source != workspace || mounts[0].Target != "/workspace" {
		t.Fatalf("unexpected primary workspace mount: %+v", mounts[0])
	}
	if mounts[1].Source != workspace || mounts[1].Target != "/workspace-mirror" {
		t.Fatalf("unexpected mirror workspace mount: %+v", mounts[1])
	}
	if mounts[2].Source != filepath.Join(home, ".si") || mounts[2].Target != "/home/si/.si" {
		t.Fatalf("unexpected host .si mount: %+v", mounts[2])
	}
}

func TestBuildContainerCoreMountsDedupesMirrorTarget(t *testing.T) {
	workspace := t.TempDir()
	mounts := BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          workspace,
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  "/workspace",
		ContainerHome:          "/home/si",
		IncludeHostSi:          false,
	})
	if len(mounts) != 1 {
		t.Fatalf("expected a single workspace mount, got %d: %+v", len(mounts), mounts)
	}
}

func TestBuildContainerCoreMountsRejectsEmptyWorkspace(t *testing.T) {
	mounts := BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost: " ",
	})
	if len(mounts) != 0 {
		t.Fatalf("expected no mounts for empty workspace host, got %+v", mounts)
	}
}
