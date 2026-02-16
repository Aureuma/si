package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestInferWorkspaceTargetUsesContainerHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(home, "Development", "si")
	got, ok := InferWorkspaceTarget(hostPath, "/root")
	if !ok {
		t.Fatalf("expected workspace target inference to succeed")
	}
	if got != "/root/Development/si" {
		t.Fatalf("unexpected inferred target: %q", got)
	}
}

func TestInferDevelopmentMountReturnsContainerHomeDevelopment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(home, "Development", "si")
	m, ok := InferDevelopmentMount(hostPath, "/home/si")
	if !ok {
		t.Fatalf("expected development mount inference to succeed")
	}
	if m.Source != filepath.Join(home, "Development") {
		t.Fatalf("unexpected development source: %q", m.Source)
	}
	if m.Target != "/home/si/Development" {
		t.Fatalf("unexpected development target: %q", m.Target)
	}
}

func TestHasDevelopmentMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(home, "Development", "si")
	requiredSource := filepath.Join(home, "Development")
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: requiredSource, Destination: "/home/si/Development"},
		},
	}
	if !HasDevelopmentMount(info, hostPath, "/home/si") {
		t.Fatalf("expected development mount requirement to be satisfied")
	}
}

func TestHasDevelopmentMountNotRequiredOutsideDevelopment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(home, "Projects", "si")
	if !HasDevelopmentMount(nil, hostPath, "/home/si") {
		t.Fatalf("expected development mount to be optional outside ~/Development")
	}
}

func TestHasDevelopmentMountFailsWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(home, "Development", "si")
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: filepath.Join(home, "Development"), Destination: "/workspace"},
		},
	}
	if HasDevelopmentMount(info, hostPath, "/home/si") {
		t.Fatalf("expected missing development mount to fail validation")
	}
}

func TestInferDevelopmentMountRequiresHomeDevelopmentSubtree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostPath := filepath.Join(os.TempDir(), "si")
	if _, ok := InferDevelopmentMount(hostPath, "/home/si"); ok {
		t.Fatalf("expected development mount inference to fail outside ~/Development")
	}
}
