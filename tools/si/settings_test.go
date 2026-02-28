package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = orig
	}()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr pipe writer: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stderr pipe reader: %v", err)
	}
	return string(out)
}

func TestSettingsHomeDirUsesOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", tmp)
	t.Setenv("HOME", "/should/not/be/used")

	got, err := settingsHomeDir()
	if err != nil {
		t.Fatalf("settingsHomeDir() unexpected err: %v", err)
	}
	if got != tmp {
		t.Fatalf("expected settings home override %q, got %q", tmp, got)
	}
}

func TestSettingsHomeDirRootFallsBackFromForeignHome(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root euid")
	}
	foreignHome := t.TempDir()
	if err := os.Chown(foreignHome, 1001, 1001); err != nil {
		t.Fatalf("chown foreign home: %v", err)
	}
	t.Setenv("SI_SETTINGS_HOME", "")
	t.Setenv("HOME", foreignHome)

	got, err := settingsHomeDir()
	if err != nil {
		t.Fatalf("settingsHomeDir() unexpected err: %v", err)
	}
	rootHome, err := homeDirByUID(0)
	if err != nil {
		t.Fatalf("homeDirByUID(0): %v", err)
	}
	if got != rootHome {
		t.Fatalf("expected root home %q, got %q", rootHome, got)
	}
}

func TestSettingsHomeDirRootFallsBackWithExistingSIState(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root euid")
	}
	foreignHome := t.TempDir()
	if err := os.Chown(foreignHome, 1001, 1001); err != nil {
		t.Fatalf("chown foreign home: %v", err)
	}
	settingsDir := filepath.Join(foreignHome, ".si")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.toml")
	if err := os.WriteFile(settingsPath, []byte("schema_version = 1\n"), 0o600); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	t.Setenv("SI_SETTINGS_HOME", "")
	t.Setenv("HOME", foreignHome)

	got, err := settingsHomeDir()
	if err != nil {
		t.Fatalf("settingsHomeDir() unexpected err: %v", err)
	}
	rootHome, err := homeDirByUID(0)
	if err != nil {
		t.Fatalf("homeDirByUID(0): %v", err)
	}
	if got != rootHome {
		t.Fatalf("expected root home %q, got %q", rootHome, got)
	}
}

func TestLoadSettingsCreatesDefaultWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() unexpected err: %v", err)
	}
	// Spot-check defaults are applied.
	if got.SchemaVersion != 1 {
		t.Fatalf("expected schema_version=1, got %d", got.SchemaVersion)
	}
	if got.Dyad.Loop.SleepSeconds != 3 {
		t.Fatalf("expected dyad.loop.sleep_seconds default=3, got %d", got.Dyad.Loop.SleepSeconds)
	}
	if got.Dyad.Loop.TurnTimeoutSeconds != 900 {
		t.Fatalf("expected dyad.loop.turn_timeout_seconds default=900, got %d", got.Dyad.Loop.TurnTimeoutSeconds)
	}
	if got.Dyad.Loop.TmuxCapture != "main" {
		t.Fatalf("expected dyad.loop.tmux_capture default=main, got %q", got.Dyad.Loop.TmuxCapture)
	}

	path, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected core settings file to be created at %s: %v", path, statErr)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read settings file: %v", readErr)
	}
	if !strings.Contains(string(data), "schema_version") {
		t.Fatalf("expected settings file to contain schema_version, got:\n%s", string(data))
	}
	dyadPath, err := settingsModulePath(settingsModuleDyad)
	if err != nil {
		t.Fatalf("settingsModulePath(dyad): %v", err)
	}
	if _, statErr := os.Stat(dyadPath); statErr != nil {
		t.Fatalf("expected dyad settings file to be created at %s: %v", dyadPath, statErr)
	}
}

func TestLoadSettingsInvalidTomlReturnsDefaultsAndError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := settingsModulePath(settingsModuleDyad)
	if err != nil {
		t.Fatalf("settingsModulePath(dyad): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("dyad = { loop = [ }"), 0o600); err != nil {
		t.Fatalf("write invalid toml: %v", err)
	}

	got, err := loadSettings()
	if err == nil {
		t.Fatalf("expected parse error")
	}
	// The important part: we still return a usable defaults object.
	if got.SchemaVersion != 1 {
		t.Fatalf("expected schema_version=1, got %d", got.SchemaVersion)
	}
	if got.Dyad.Loop.SleepSeconds != 3 {
		t.Fatalf("expected dyad.loop.sleep_seconds default=3, got %d", got.Dyad.Loop.SleepSeconds)
	}
	if got.Dyad.Loop.TurnTimeoutSeconds != 900 {
		t.Fatalf("expected dyad.loop.turn_timeout_seconds default=900, got %d", got.Dyad.Loop.TurnTimeoutSeconds)
	}
	if got.Dyad.Loop.TmuxCapture != "main" {
		t.Fatalf("expected dyad.loop.tmux_capture default=main, got %q", got.Dyad.Loop.TmuxCapture)
	}
	if !strings.Contains(err.Error(), "settings.toml") {
		t.Fatalf("expected error to include settings path, got: %v", err)
	}
}

func TestLoadSettingsClampsBadValues(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path := filepath.Join(tmp, ".si", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Intentionally bad values that should be clamped/defaulted.
	content := `
[dyad.loop]
sleep_seconds = 0
startup_delay_seconds = -1
turn_timeout_seconds = 0
retry_max = 0
retry_base_seconds = 0
prompt_lines = 0
pause_poll_seconds = 0
tmux_capture = "weird"
max_turns = -5
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, err := loadSettings()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Dyad.Loop.SleepSeconds != 3 {
		t.Fatalf("expected sleep_seconds=3 default, got %d", got.Dyad.Loop.SleepSeconds)
	}
	if got.Dyad.Loop.StartupDelaySeconds != 2 {
		t.Fatalf("expected startup_delay_seconds=2 default, got %d", got.Dyad.Loop.StartupDelaySeconds)
	}
	if got.Dyad.Loop.TurnTimeoutSeconds != 900 {
		t.Fatalf("expected turn_timeout_seconds=900 default, got %d", got.Dyad.Loop.TurnTimeoutSeconds)
	}
	if got.Dyad.Loop.RetryMax != 3 {
		t.Fatalf("expected retry_max=3 default, got %d", got.Dyad.Loop.RetryMax)
	}
	if got.Dyad.Loop.RetryBaseSeconds != 2 {
		t.Fatalf("expected retry_base_seconds=2 default, got %d", got.Dyad.Loop.RetryBaseSeconds)
	}
	if got.Dyad.Loop.PromptLines != 3 {
		t.Fatalf("expected prompt_lines=3 default, got %d", got.Dyad.Loop.PromptLines)
	}
	if got.Dyad.Loop.PausePollSeconds != 5 {
		t.Fatalf("expected pause_poll_seconds=5 default, got %d", got.Dyad.Loop.PausePollSeconds)
	}
	if got.Dyad.Loop.TmuxCapture != "main" {
		t.Fatalf("expected tmux_capture=main default, got %q", got.Dyad.Loop.TmuxCapture)
	}
	if got.Dyad.Loop.MaxTurns != 0 {
		t.Fatalf("expected max_turns=0 clamp, got %d", got.Dyad.Loop.MaxTurns)
	}
}

func TestLoadSettingsParsesSkillsVolumeFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	codexPath, err := settingsModulePath(settingsModuleCodex)
	if err != nil {
		t.Fatalf("settingsModulePath(codex): %v", err)
	}
	dyadPath, err := settingsModulePath(settingsModuleDyad)
	if err != nil {
		t.Fatalf("settingsModulePath(dyad): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dyadPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	codexContent := `
[codex]
skills_volume = "si-codex-skills-custom"
`
	dyadContent := `

[dyad]
skills_volume = "si-dyad-skills-custom"
`
	if err := os.WriteFile(codexPath, []byte(codexContent), 0o600); err != nil {
		t.Fatalf("write codex settings: %v", err)
	}
	if err := os.WriteFile(dyadPath, []byte(dyadContent), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, err := loadSettings()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Codex.SkillsVolume != "si-codex-skills-custom" {
		t.Fatalf("unexpected codex.skills_volume: %q", got.Codex.SkillsVolume)
	}
	if got.Dyad.SkillsVolume != "si-dyad-skills-custom" {
		t.Fatalf("unexpected dyad.skills_volume: %q", got.Dyad.SkillsVolume)
	}
}

func TestLoadSettingsOrDefaultWarnsOnceForRepeatedError(t *testing.T) {
	original := loadSettingsForDefault
	t.Cleanup(func() {
		loadSettingsForDefault = original
		resetSettingsWarnOnceStateForTests()
	})
	resetSettingsWarnOnceStateForTests()
	loadErr := errors.New("read settings /tmp/.si/settings.toml: open /tmp/.si/settings.toml: permission denied")
	loadSettingsForDefault = func() (Settings, error) {
		return Settings{}, loadErr
	}

	stderr := captureStderr(t, func() {
		_ = loadSettingsOrDefault()
		_ = loadSettingsOrDefault()
	})

	if got := strings.Count(stderr, "warning: settings load failed:"); got != 1 {
		t.Fatalf("expected one warning, got %d output:\n%s", got, stderr)
	}
	if !strings.Contains(stderr, "permission denied") {
		t.Fatalf("expected warning to include permission denied, got:\n%s", stderr)
	}
}

func TestLoadSettingsOrDefaultWarnsOncePerDistinctError(t *testing.T) {
	original := loadSettingsForDefault
	t.Cleanup(func() {
		loadSettingsForDefault = original
		resetSettingsWarnOnceStateForTests()
	})
	resetSettingsWarnOnceStateForTests()

	loadErrs := []error{
		errors.New("read settings one: permission denied"),
		errors.New("read settings two: parse failure"),
	}
	loadSettingsForDefault = func() (Settings, error) {
		err := loadErrs[0]
		if len(loadErrs) > 1 {
			loadErrs = loadErrs[1:]
		}
		return Settings{}, err
	}

	stderr := captureStderr(t, func() {
		_ = loadSettingsOrDefault()
		_ = loadSettingsOrDefault()
	})

	if got := strings.Count(stderr, "warning: settings load failed:"); got != 2 {
		t.Fatalf("expected two warnings for distinct errors, got %d output:\n%s", got, stderr)
	}
}

func TestApplySettingsDefaultsSetsCodexLoginDefaultBrowser(t *testing.T) {
	settings := Settings{}
	applySettingsDefaults(&settings)
	if settings.Codex.Login.DefaultBrowser == "" {
		t.Fatalf("expected codex.login.default_browser default to be set")
	}
}

func TestApplySettingsDefaultsNormalizesCodexLoginDefaultBrowser(t *testing.T) {
	settings := Settings{
		Codex: CodexSettings{
			Login: CodexLoginSettings{
				DefaultBrowser: "  CHROME ",
			},
		},
	}
	applySettingsDefaults(&settings)
	if settings.Codex.Login.DefaultBrowser != "chrome" {
		t.Fatalf("expected default_browser to normalize to chrome, got %q", settings.Codex.Login.DefaultBrowser)
	}
}

func TestLoadSettingsSunSection(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := settingsModulePath(settingsModuleSun)
	if err != nil {
		t.Fatalf("settingsModulePath(sun): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `
[sun]
base_url = "https://sun.example"
token = "sun-token"
timeout_seconds = 30
taskboard = "shared"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if got.Sun.BaseURL != "https://sun.example" {
		t.Fatalf("expected sun base url override, got %q", got.Sun.BaseURL)
	}
	if got.Sun.Token != "sun-token" {
		t.Fatalf("expected sun token override, got %q", got.Sun.Token)
	}
	if got.Sun.TimeoutSeconds != 30 {
		t.Fatalf("expected sun timeout override, got %d", got.Sun.TimeoutSeconds)
	}
	if got.Sun.Taskboard != "shared" {
		t.Fatalf("expected sun taskboard override, got %q", got.Sun.Taskboard)
	}
}

func TestSaveSettingsWritesSunSection(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = "https://sun-save.example"
	settings.Sun.Token = "save-token"

	if err := saveSettings(settings); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	path, err := settingsModulePath(settingsModuleSun)
	if err != nil {
		t.Fatalf("settingsModulePath(sun): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[sun]") {
		t.Fatalf("expected [sun] section in saved settings, got:\n%s", text)
	}
	if !strings.Contains(text, "sun-save.example") {
		t.Fatalf("expected sun base_url in saved settings, got:\n%s", text)
	}
}

func TestLoadSettingsSurfSection(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := settingsModulePath(settingsModuleSurf)
	if err != nil {
		t.Fatalf("settingsModulePath(surf): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `
[surf]
repo = "/work/surf"
bin = "/work/surf/bin/surf"
build = true
settings_file = "/home/user/.si/surf/settings.toml"
state_dir = "/home/user/.surf-state"

[surf.tunnel]
name = "surf-cloudflared"
mode = "token"
vault_key = "SURF_CLOUDFLARE_TUNNEL_TOKEN"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if got.Surf.Repo != "/work/surf" || got.Surf.Bin != "/work/surf/bin/surf" {
		t.Fatalf("unexpected surf repo/bin: %#v", got.Surf)
	}
	if got.Surf.Build == nil || !*got.Surf.Build {
		t.Fatalf("expected surf.build=true")
	}
	if got.Surf.Tunnel.Mode != "token" || got.Surf.Tunnel.VaultKey != "SURF_CLOUDFLARE_TUNNEL_TOKEN" {
		t.Fatalf("unexpected surf tunnel settings: %#v", got.Surf.Tunnel)
	}
}

func TestApplySettingsDefaultsNormalizesSurfTunnelMode(t *testing.T) {
	settings := defaultSettings()
	settings.Surf.Tunnel.Mode = "BAD"
	applySettingsDefaults(&settings)
	if settings.Surf.Tunnel.Mode != "" {
		t.Fatalf("expected invalid surf.tunnel.mode to normalize empty, got %q", settings.Surf.Tunnel.Mode)
	}
}

func TestSettingsModulePathViva(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", tmp)
	path, err := settingsModulePath(settingsModuleViva)
	if err != nil {
		t.Fatalf("settingsModulePath(viva): %v", err)
	}
	want := filepath.Join(tmp, ".si", "viva", "settings.toml")
	if path != want {
		t.Fatalf("unexpected viva settings path: got %q want %q", path, want)
	}
}

func TestLoadSettingsParsesVivaTunnelProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", tmp)

	vivaPath, err := settingsModulePath(settingsModuleViva)
	if err != nil {
		t.Fatalf("settingsModulePath(viva): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(vivaPath), 0o700); err != nil {
		t.Fatalf("mkdir viva settings dir: %v", err)
	}
	content := `
schema_version = 1

[viva]
repo = "/work/viva"
bin = "/work/viva/bin/viva"

[viva.tunnel]
default_profile = "dev"

[viva.tunnel.profiles.dev]
container_name = "viva-cloudflared-dev-browser"
network_mode = "viva-shared"
additional_networks = ["si", "viva-shared", " ", "supabase_default"]
vault_env_file = "/work/safe/sampleapp/.env.dev"
vault_repo = "sampleapp"
vault_env = "dev"

[[viva.tunnel.profiles.dev.routes]]
hostname = "dev.example.app"
service = "http://127.0.0.1:3000"
`
	if err := os.WriteFile(vivaPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write viva settings: %v", err)
	}

	settings, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if settings.Viva.Repo != "/work/viva" || settings.Viva.Bin != "/work/viva/bin/viva" {
		t.Fatalf("unexpected viva wrapper settings: %#v", settings.Viva)
	}
	if settings.Viva.Tunnel.DefaultProfile != "dev" {
		t.Fatalf("unexpected default profile: %q", settings.Viva.Tunnel.DefaultProfile)
	}
	profile, ok := settings.Viva.Tunnel.Profiles["dev"]
	if !ok {
		t.Fatalf("expected dev profile")
	}
	if profile.ContainerName != "viva-cloudflared-dev-browser" {
		t.Fatalf("unexpected container name: %q", profile.ContainerName)
	}
	if len(profile.AdditionalNetworks) != 2 || profile.AdditionalNetworks[0] != "si" || profile.AdditionalNetworks[1] != "supabase_default" {
		t.Fatalf("unexpected additional networks: %#v", profile.AdditionalNetworks)
	}
	if len(profile.Routes) != 1 || profile.Routes[0].Hostname != "dev.example.app" {
		t.Fatalf("unexpected routes: %#v", profile.Routes)
	}
}
