package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestHostSiCodexProfileMountsMountsWholeSiDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	siDir := filepath.Join(home, ".si")
	if err := ensureDir(siDir); err != nil {
		t.Fatalf("ensure .si dir: %v", err)
	}

	mounts := HostSiCodexProfileMounts("/home/si")
	if len(mounts) != 1 {
		t.Fatalf("expected one mount, got %d (%+v)", len(mounts), mounts)
	}
	if got := mounts[0].Source; got != siDir {
		t.Fatalf("unexpected source %q", got)
	}
	if got := mounts[0].Target; got != "/home/si/.si" {
		t.Fatalf("unexpected target %q", got)
	}
	if mounts[0].ReadOnly {
		t.Fatalf("expected ~/.si mount to be read-write")
	}
}

func TestHostSiCodexProfileMountsNoSiDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	mounts := HostSiCodexProfileMounts("/home/si")
	if len(mounts) != 0 {
		t.Fatalf("expected no mounts, got %+v", mounts)
	}
}

func TestHasHostSiMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	siDir := filepath.Join(home, ".si")
	if err := ensureDir(siDir); err != nil {
		t.Fatalf("ensure .si dir: %v", err)
	}

	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "bind",
				Source:      siDir,
				Destination: "/root/.si",
			},
		},
	}
	if !HasHostSiMount(info, "/root") {
		t.Fatalf("expected host ~/.si mount to be detected")
	}
	if HasHostSiMount(info, "/home/si") {
		t.Fatalf("expected wrong target home to fail detection")
	}
}

func TestHasHostSiMountNoHostSiDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if HasHostSiMount(&types.ContainerJSON{}, "/root") == false {
		t.Fatalf("expected true when host ~/.si does not exist")
	}
}

func ensureDir(p string) error {
	return os.MkdirAll(p, 0o700)
}
