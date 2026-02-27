package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyVivaConfigSet(t *testing.T) {
	settings := defaultSettings()
	changed, err := applyVivaConfigSet(&settings, vivaConfigSetInput{
		RepoProvided:  true,
		Repo:          "/tmp/viva",
		BinProvided:   true,
		Bin:           "/tmp/viva/bin/viva",
		BuildProvided: true,
		BuildRaw:      "true",
	})
	if err != nil {
		t.Fatalf("applyVivaConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Viva.Repo != "/tmp/viva" || settings.Viva.Bin != "/tmp/viva/bin/viva" {
		t.Fatalf("unexpected viva repo/bin: %#v", settings.Viva)
	}
	if settings.Viva.Build == nil || !*settings.Viva.Build {
		t.Fatalf("expected viva.build=true")
	}
}

func TestApplyVivaConfigSetClearsBuildOnEmpty(t *testing.T) {
	settings := defaultSettings()
	settings.Viva.Build = boolPtr(true)
	changed, err := applyVivaConfigSet(&settings, vivaConfigSetInput{BuildProvided: true, BuildRaw: ""})
	if err != nil {
		t.Fatalf("applyVivaConfigSet: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if settings.Viva.Build != nil {
		t.Fatalf("expected build unset")
	}
}

func TestApplyVivaConfigSetRejectsInvalidBuild(t *testing.T) {
	settings := defaultSettings()
	_, err := applyVivaConfigSet(&settings, vivaConfigSetInput{BuildProvided: true, BuildRaw: "bad"})
	if err == nil {
		t.Fatalf("expected invalid build error")
	}
}

func TestImportLegacyTunnelProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cloudflare.tunnel.dev.toml")
	raw := `version = 1

[tunnel]
name = "rm-dev-browser"
container_name = "viva-cloudflared-rm-dev-browser"
tunnel_id_env_key = "VIVA_CLOUDFLARE_TUNNEL_ID"
credentials_env_key = "CLOUDFLARE_TUNNEL_CREDENTIALS_JSON"
metrics_addr = "127.0.0.1:20241"

[runtime]
image = "cloudflare/cloudflared:latest"
network_mode = "host"
no_autoupdate = true
pull_image = true
dir = "../../viva/.tmp/viva/cloudflared"

[vault]
env_file = ".env.dev"
repo = "viva"
env = "dev"

[[routes]]
hostname = "ls-dev.lingospeak.ai"
service = "http://127.0.0.1:3000"
`
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write legacy tunnel config: %v", err)
	}

	profile, err := importLegacyTunnelProfile(cfgPath)
	if err != nil {
		t.Fatalf("importLegacyTunnelProfile: %v", err)
	}
	if profile.ContainerName != "viva-cloudflared-rm-dev-browser" {
		t.Fatalf("unexpected container: %q", profile.ContainerName)
	}
	if profile.VaultEnvFile != filepath.Join(dir, ".env.dev") {
		t.Fatalf("expected absolute env file, got %q", profile.VaultEnvFile)
	}
	if profile.RuntimeDir != filepath.Clean(filepath.Join(dir, "../../viva/.tmp/viva/cloudflared")) {
		t.Fatalf("expected absolute runtime dir, got %q", profile.RuntimeDir)
	}
	if len(profile.Routes) != 1 {
		t.Fatalf("expected one route, got %d", len(profile.Routes))
	}
}

func TestNormalizeVivaTunnelProfileDefaults(t *testing.T) {
	profile := normalizeVivaTunnelProfile(VivaTunnelProfile{})
	if profile.TunnelIDEnvKey != "VIVA_CLOUDFLARE_TUNNEL_ID" {
		t.Fatalf("unexpected default tunnel id key: %q", profile.TunnelIDEnvKey)
	}
	if profile.CredentialsEnvKey != "CLOUDFLARE_TUNNEL_CREDENTIALS_JSON" {
		t.Fatalf("unexpected default credentials key: %q", profile.CredentialsEnvKey)
	}
	if profile.Image != "cloudflare/cloudflared:latest" {
		t.Fatalf("unexpected default image: %q", profile.Image)
	}
	if profile.NetworkMode != "host" {
		t.Fatalf("unexpected default network mode: %q", profile.NetworkMode)
	}
	if profile.NoAutoupdate == nil || !*profile.NoAutoupdate {
		t.Fatalf("unexpected default no_autoupdate: %#v", profile.NoAutoupdate)
	}
	if profile.PullImage == nil || !*profile.PullImage {
		t.Fatalf("unexpected default pull_image: %#v", profile.PullImage)
	}
	if profile.VaultRepo != "viva" {
		t.Fatalf("unexpected default vault repo: %q", profile.VaultRepo)
	}
	if profile.VaultEnv != "dev" {
		t.Fatalf("unexpected default vault env: %q", profile.VaultEnv)
	}
}

func TestDefaultVivaRepoPathFindsSiblingRepo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "viva"), 0o755); err != nil {
		t.Fatalf("mkdir viva sibling: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir base: %v", err)
	}

	got := defaultVivaRepoPath()
	want := filepath.Join(base, "viva")
	if got != want {
		t.Fatalf("defaultVivaRepoPath()=%q want=%q", got, want)
	}
}

func TestDefaultVivaRepoPathEmptyWhenMissing(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	base := t.TempDir()
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir base: %v", err)
	}

	got := defaultVivaRepoPath()
	if got != "" {
		t.Fatalf("defaultVivaRepoPath()=%q want empty", got)
	}
}
