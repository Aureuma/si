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

func TestHostDockerConfigMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dockerDir := filepath.Join(home, ".docker")
	if err := ensureDir(dockerDir); err != nil {
		t.Fatalf("ensure .docker dir: %v", err)
	}

	got, ok := HostDockerConfigMount("/home/si")
	if !ok {
		t.Fatalf("expected docker config mount to be returned")
	}
	if got.Source != dockerDir {
		t.Fatalf("unexpected source %q", got.Source)
	}
	if got.Target != "/home/si/.docker" {
		t.Fatalf("unexpected target %q", got.Target)
	}
}

func TestHasHostDockerConfigMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dockerDir := filepath.Join(home, ".docker")
	if err := ensureDir(dockerDir); err != nil {
		t.Fatalf("ensure .docker dir: %v", err)
	}
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "bind",
				Source:      dockerDir,
				Destination: "/root/.docker",
			},
		},
	}
	if !HasHostDockerConfigMount(info, "/root") {
		t.Fatalf("expected host ~/.docker mount to be detected")
	}
	if HasHostDockerConfigMount(info, "/home/si") {
		t.Fatalf("expected wrong target home to fail detection")
	}
}

func TestHostSiGoToolchainMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	goDir := filepath.Join(home, ".local", "share", "si", "go")
	if err := ensureDir(goDir); err != nil {
		t.Fatalf("ensure go dir: %v", err)
	}

	got, ok := HostSiGoToolchainMount("/home/si")
	if !ok {
		t.Fatalf("expected go toolchain mount to be returned")
	}
	if got.Source != goDir {
		t.Fatalf("unexpected source %q", got.Source)
	}
	if got.Target != "/home/si/.local/share/si/go" {
		t.Fatalf("unexpected target %q", got.Target)
	}
	if !got.ReadOnly {
		t.Fatalf("expected go toolchain mount to be read-only")
	}
}

func TestHasHostSiGoToolchainMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	goDir := filepath.Join(home, ".local", "share", "si", "go")
	if err := ensureDir(goDir); err != nil {
		t.Fatalf("ensure go dir: %v", err)
	}
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "bind",
				Source:      goDir,
				Destination: "/root/.local/share/si/go",
			},
		},
	}
	if !HasHostSiGoToolchainMount(info, "/root") {
		t.Fatalf("expected host go toolchain mount to be detected")
	}
	if HasHostSiGoToolchainMount(info, "/home/si") {
		t.Fatalf("expected wrong target home to fail detection")
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
