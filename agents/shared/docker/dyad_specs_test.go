package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/mount"
)

func TestBuildDyadSpecsIncludesHostSiMounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	workspace := t.TempDir()
	configs := filepath.Join(workspace, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}

	actor, critic, err := BuildDyadSpecs(DyadOptions{
		Dyad:          "mounttest",
		Role:          "generic",
		ActorImage:    "aureuma/si:local",
		CriticImage:   "aureuma/si:local",
		WorkspaceHost: workspace,
		ConfigsHost:   configs,
		Network:       DefaultNetwork,
	})
	if err != nil {
		t.Fatalf("build specs: %v", err)
	}

	if !mountExists(actor.HostConfig.Mounts, filepath.Join(home, ".si"), "/root/.si") {
		t.Fatalf("actor spec missing host ~/.si mount: %+v", actor.HostConfig.Mounts)
	}
	if !mountExists(critic.HostConfig.Mounts, filepath.Join(home, ".si"), "/root/.si") {
		t.Fatalf("critic spec missing host ~/.si mount: %+v", critic.HostConfig.Mounts)
	}
	if !mountExists(actor.HostConfig.Mounts, DefaultCodexSkillsVolume, "/root/.codex/skills") {
		t.Fatalf("actor spec missing shared skills volume mount: %+v", actor.HostConfig.Mounts)
	}
	if !mountExists(critic.HostConfig.Mounts, DefaultCodexSkillsVolume, "/root/.codex/skills") {
		t.Fatalf("critic spec missing shared skills volume mount: %+v", critic.HostConfig.Mounts)
	}
}

func TestBuildDyadSpecsRespectsCustomSkillsVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	workspace := t.TempDir()
	configs := filepath.Join(workspace, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}

	const customSkills = "si-codex-skills-custom"
	actor, critic, err := BuildDyadSpecs(DyadOptions{
		Dyad:          "mounttest-custom",
		Role:          "generic",
		ActorImage:    "aureuma/si:local",
		CriticImage:   "aureuma/si:local",
		WorkspaceHost: workspace,
		ConfigsHost:   configs,
		SkillsVolume:  customSkills,
		Network:       DefaultNetwork,
	})
	if err != nil {
		t.Fatalf("build specs: %v", err)
	}
	if !mountExists(actor.HostConfig.Mounts, customSkills, "/root/.codex/skills") {
		t.Fatalf("actor spec missing custom shared skills volume mount: %+v", actor.HostConfig.Mounts)
	}
	if !mountExists(critic.HostConfig.Mounts, customSkills, "/root/.codex/skills") {
		t.Fatalf("critic spec missing custom shared skills volume mount: %+v", critic.HostConfig.Mounts)
	}
}

func mountExists(mounts []mount.Mount, source string, target string) bool {
	for _, m := range mounts {
		if filepath.Clean(m.Source) == filepath.Clean(source) && filepath.ToSlash(m.Target) == target {
			return true
		}
	}
	return false
}

func TestBuildDyadSpecsIncludesVaultEnvFileMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	workspace := t.TempDir()
	configs := filepath.Join(workspace, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	vaultFile := filepath.Join(t.TempDir(), ".env.vault")
	if err := os.WriteFile(vaultFile, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	actor, critic, err := BuildDyadSpecs(DyadOptions{
		Dyad:          "mounttest-vault",
		Role:          "generic",
		ActorImage:    "aureuma/si:local",
		CriticImage:   "aureuma/si:local",
		WorkspaceHost: workspace,
		ConfigsHost:   configs,
		VaultEnvFile:  vaultFile,
		Network:       DefaultNetwork,
	})
	if err != nil {
		t.Fatalf("build specs: %v", err)
	}
	if !mountExists(actor.HostConfig.Mounts, vaultFile, filepath.ToSlash(vaultFile)) {
		t.Fatalf("actor spec missing vault env file mount: %+v", actor.HostConfig.Mounts)
	}
	if !mountExists(critic.HostConfig.Mounts, vaultFile, filepath.ToSlash(vaultFile)) {
		t.Fatalf("critic spec missing vault env file mount: %+v", critic.HostConfig.Mounts)
	}
}

func TestBuildDyadSpecsIncludesDevelopmentMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	workspace := filepath.Join(home, "Development", "si")
	configs := filepath.Join(workspace, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}

	actor, critic, err := BuildDyadSpecs(DyadOptions{
		Dyad:          "mounttest-dev",
		Role:          "generic",
		ActorImage:    "aureuma/si:local",
		CriticImage:   "aureuma/si:local",
		WorkspaceHost: workspace,
		ConfigsHost:   configs,
		Network:       DefaultNetwork,
	})
	if err != nil {
		t.Fatalf("build specs: %v", err)
	}

	developmentHost := filepath.Join(home, "Development")
	if !mountExists(actor.HostConfig.Mounts, developmentHost, "/root/Development") {
		t.Fatalf("actor spec missing host ~/Development mount: %+v", actor.HostConfig.Mounts)
	}
	if !mountExists(critic.HostConfig.Mounts, developmentHost, "/root/Development") {
		t.Fatalf("critic spec missing host ~/Development mount: %+v", critic.HostConfig.Mounts)
	}
}
