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

func TestBuildContainerCoreMountsIncludesVaultEnvFileMount(t *testing.T) {
	workspace := t.TempDir()
	vaultFile := filepath.Join(t.TempDir(), ".env.vault")
	if err := os.WriteFile(vaultFile, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	mounts := BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          workspace,
		WorkspacePrimaryTarget: "/workspace",
		ContainerHome:          "/home/si",
		IncludeHostSi:          false,
		HostVaultEnvFile:       vaultFile,
	})
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d: %+v", len(mounts), mounts)
	}
	if mounts[1].Source != vaultFile || mounts[1].Target != filepath.ToSlash(vaultFile) {
		t.Fatalf("unexpected vault mount: %+v", mounts[1])
	}
}

func TestBuildContainerCoreMountsIncludesDevelopmentMirrorMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "Development", "si")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	mounts := BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          workspace,
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  "/home/si/Development/si",
		ContainerHome:          "/home/si",
	})
	if len(mounts) != 4 {
		t.Fatalf("expected 4 mounts, got %d: %+v", len(mounts), mounts)
	}
	if mounts[2].Source != filepath.Join(home, "Development") || mounts[2].Target != "/home/si/Development" {
		t.Fatalf("unexpected development mount: %+v", mounts[2])
	}
	if mounts[3].Source != filepath.Join(home, "Development") || mounts[3].Target != filepath.ToSlash(filepath.Join(home, "Development")) {
		t.Fatalf("unexpected host development mount: %+v", mounts[3])
	}
}
