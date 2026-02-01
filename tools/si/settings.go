package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Settings struct {
	SchemaVersion int              `toml:"schema_version"`
	Paths         SettingsPaths    `toml:"paths"`
	Codex         CodexSettings    `toml:"codex"`
	Dyad          DyadSettings     `toml:"dyad"`
	Shell         ShellSettings    `toml:"shell"`
	Metadata      SettingsMetadata `toml:"metadata,omitempty"`
}

type SettingsMetadata struct {
	UpdatedAt string `toml:"updated_at,omitempty"`
}

type SettingsPaths struct {
	Root             string `toml:"root,omitempty"`
	SettingsFile     string `toml:"settings,omitempty"`
	CodexProfilesDir string `toml:"codex_profiles_dir,omitempty"`
}

type CodexSettings struct {
	Image        string               `toml:"image,omitempty"`
	Network      string               `toml:"network,omitempty"`
	Workspace    string               `toml:"workspace,omitempty"`
	Workdir      string               `toml:"workdir,omitempty"`
	Repo         string               `toml:"repo,omitempty"`
	GHPAT        string               `toml:"gh_pat,omitempty"`
	CodexVolume  string               `toml:"codex_volume,omitempty"`
	GHVolume     string               `toml:"gh_volume,omitempty"`
	DockerSocket *bool                `toml:"docker_socket,omitempty"`
	Profile      string               `toml:"profile,omitempty"`
	Detach       *bool                `toml:"detach,omitempty"`
	CleanSlate   *bool                `toml:"clean_slate,omitempty"`
	Login        CodexLoginSettings   `toml:"login,omitempty"`
	Exec         CodexExecSettings    `toml:"exec,omitempty"`
	Profiles     CodexProfilesSetting `toml:"profiles,omitempty"`
}

type CodexLoginSettings struct {
	DeviceAuth     *bool  `toml:"device_auth,omitempty"`
	OpenURL        *bool  `toml:"open_url,omitempty"`
	OpenURLCommand string `toml:"open_url_command,omitempty"`
}

type CodexExecSettings struct {
	Model  string `toml:"model,omitempty"`
	Effort string `toml:"effort,omitempty"`
}

type CodexProfilesSetting struct {
	Active  string                       `toml:"active,omitempty"`
	Entries map[string]CodexProfileEntry `toml:"entries,omitempty"`
}

type CodexProfileEntry struct {
	Name        string `toml:"name,omitempty"`
	Email       string `toml:"email,omitempty"`
	AuthPath    string `toml:"auth_path,omitempty"`
	AuthUpdated string `toml:"auth_updated,omitempty"`
}

type DyadSettings struct {
	ActorImage        string `toml:"actor_image,omitempty"`
	CriticImage       string `toml:"critic_image,omitempty"`
	CodexModel        string `toml:"codex_model,omitempty"`
	CodexEffortActor  string `toml:"codex_effort_actor,omitempty"`
	CodexEffortCritic string `toml:"codex_effort_critic,omitempty"`
	CodexModelLow     string `toml:"codex_model_low,omitempty"`
	CodexModelMedium  string `toml:"codex_model_medium,omitempty"`
	CodexModelHigh    string `toml:"codex_model_high,omitempty"`
	CodexEffortLow    string `toml:"codex_effort_low,omitempty"`
	CodexEffortMedium string `toml:"codex_effort_medium,omitempty"`
	CodexEffortHigh   string `toml:"codex_effort_high,omitempty"`
	Workspace         string `toml:"workspace,omitempty"`
	Configs           string `toml:"configs,omitempty"`
	ForwardPorts      string `toml:"forward_ports,omitempty"`
	DockerSocket      *bool  `toml:"docker_socket,omitempty"`
}

type ShellSettings struct {
	Prompt ShellPromptSettings `toml:"prompt,omitempty"`
}

type ShellPromptSettings struct {
	Enabled        *bool             `toml:"enabled,omitempty"`
	GitEnabled     *bool             `toml:"git_enabled,omitempty"`
	PrefixTemplate string            `toml:"prefix_template,omitempty"`
	Format         string            `toml:"format,omitempty"`
	Symbol         string            `toml:"symbol,omitempty"`
	Colors         ShellPromptColors `toml:"colors,omitempty"`
}

type ShellPromptColors struct {
	Profile string `toml:"profile,omitempty"`
	Cwd     string `toml:"cwd,omitempty"`
	Git     string `toml:"git,omitempty"`
	Symbol  string `toml:"symbol,omitempty"`
	Error   string `toml:"error,omitempty"`
	Reset   string `toml:"reset,omitempty"`
}

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	return filepath.Join(home, ".si", "settings.toml"), nil
}

func defaultSettings() Settings {
	settings := Settings{
		SchemaVersion: 1,
		Paths: SettingsPaths{
			Root:             "~/.si",
			SettingsFile:     "~/.si/settings.toml",
			CodexProfilesDir: "~/.si/codex/profiles",
		},
		Shell: ShellSettings{
			Prompt: ShellPromptSettings{
				Enabled:        boolPtr(true),
				GitEnabled:     boolPtr(true),
				PrefixTemplate: "[{profile}] ",
				Format:         "{prefix}{cwd}{git} {symbol} ",
				Symbol:         "$",
				Colors: ShellPromptColors{
					Profile: "cyan",
					Cwd:     "blue",
					Git:     "magenta",
					Symbol:  "green",
					Error:   "red",
					Reset:   "reset",
				},
			},
		},
	}
	return settings
}

func applySettingsDefaults(settings *Settings) {
	if settings == nil {
		return
	}
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = 1
	}
	if settings.Shell.Prompt.Enabled == nil {
		settings.Shell.Prompt.Enabled = boolPtr(true)
	}
	if settings.Shell.Prompt.GitEnabled == nil {
		settings.Shell.Prompt.GitEnabled = boolPtr(true)
	}
	if settings.Shell.Prompt.PrefixTemplate == "" {
		settings.Shell.Prompt.PrefixTemplate = "[{profile}] "
	}
	if settings.Shell.Prompt.Format == "" {
		settings.Shell.Prompt.Format = "{prefix}{cwd}{git} {symbol} "
	}
	if settings.Shell.Prompt.Symbol == "" {
		settings.Shell.Prompt.Symbol = "$"
	}
	if settings.Shell.Prompt.Colors.Profile == "" {
		settings.Shell.Prompt.Colors.Profile = "cyan"
	}
	if settings.Shell.Prompt.Colors.Cwd == "" {
		settings.Shell.Prompt.Colors.Cwd = "blue"
	}
	if settings.Shell.Prompt.Colors.Git == "" {
		settings.Shell.Prompt.Colors.Git = "magenta"
	}
	if settings.Shell.Prompt.Colors.Symbol == "" {
		settings.Shell.Prompt.Colors.Symbol = "green"
	}
	if settings.Shell.Prompt.Colors.Error == "" {
		settings.Shell.Prompt.Colors.Error = "red"
	}
	if settings.Shell.Prompt.Colors.Reset == "" {
		settings.Shell.Prompt.Colors.Reset = "reset"
	}
	if settings.Paths.Root == "" {
		settings.Paths.Root = "~/.si"
	}
	if settings.Paths.SettingsFile == "" {
		settings.Paths.SettingsFile = "~/.si/settings.toml"
	}
	if settings.Paths.CodexProfilesDir == "" {
		settings.Paths.CodexProfilesDir = "~/.si/codex/profiles"
	}
}

func loadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return defaultSettings(), err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			settings := defaultSettings()
			_ = saveSettings(settings)
			return settings, nil
		}
		return defaultSettings(), err
	}
	settings := defaultSettings()
	if err := toml.Unmarshal(data, &settings); err != nil {
		return defaultSettings(), err
	}
	applySettingsDefaults(&settings)
	return settings, nil
}

func loadSettingsOrDefault() Settings {
	settings, err := loadSettings()
	if err != nil {
		warnf("settings load failed: %v", err)
		return defaultSettings()
	}
	return settings
}

func saveSettings(settings Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	settings.Metadata.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := toml.Marshal(settings)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "settings-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func updateSettingsProfile(profile codexProfile) error {
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	if settings.Codex.Profiles.Entries == nil {
		settings.Codex.Profiles.Entries = map[string]CodexProfileEntry{}
	}
	if strings.TrimSpace(profile.ID) != "" {
		settings.Codex.Profiles.Active = profile.ID
		status := codexProfileAuthStatus(profile)
		entry := CodexProfileEntry{
			Name:        profile.Name,
			Email:       profile.Email,
			AuthPath:    status.Path,
			AuthUpdated: "",
		}
		if status.Exists {
			entry.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
		}
		settings.Codex.Profiles.Entries[profile.ID] = entry
	}
	return saveSettings(settings)
}

func boolPtr(val bool) *bool {
	return &val
}
