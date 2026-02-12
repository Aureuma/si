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

func TestHostVaultEnvFileMount(t *testing.T) {
	vaultFile := filepath.Join(t.TempDir(), ".env.vault")
	if err := os.WriteFile(vaultFile, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	got, ok := HostVaultEnvFileMount(vaultFile)
	if !ok {
		t.Fatalf("expected vault mount to be returned")
	}
	if got.Source != vaultFile {
		t.Fatalf("unexpected source %q", got.Source)
	}
	if got.Target != filepath.ToSlash(vaultFile) {
		t.Fatalf("unexpected target %q", got.Target)
	}
}

func TestHostVaultEnvFileMountMissingFile(t *testing.T) {
	if _, ok := HostVaultEnvFileMount(filepath.Join(t.TempDir(), "missing.env")); ok {
		t.Fatalf("expected no mount for missing file")
	}
}

func TestHasHostVaultEnvFileMount(t *testing.T) {
	vaultFile := filepath.Join(t.TempDir(), ".env.vault")
	if err := os.WriteFile(vaultFile, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "bind",
				Source:      vaultFile,
				Destination: filepath.ToSlash(vaultFile),
			},
		},
	}
	if !HasHostVaultEnvFileMount(info, vaultFile) {
		t.Fatalf("expected vault env file mount to be detected")
	}
	other := filepath.Join(filepath.Dir(vaultFile), "other.env")
	if err := os.WriteFile(other, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write other vault file: %v", err)
	}
	if HasHostVaultEnvFileMount(info, other) {
		t.Fatalf("expected wrong vault file to fail detection")
	}
}

func ensureDir(p string) error {
	return os.MkdirAll(p, 0o700)
}
