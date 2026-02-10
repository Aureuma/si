package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"si/tools/si/internal/providers"
)

type Settings struct {
	SchemaVersion int                `toml:"schema_version"`
	Paths         SettingsPaths      `toml:"paths"`
	Codex         CodexSettings      `toml:"codex"`
	Vault         VaultSettings      `toml:"vault,omitempty"`
	Stripe        StripeSettings     `toml:"stripe,omitempty"`
	Github        GitHubSettings     `toml:"github,omitempty"`
	Cloudflare    CloudflareSettings `toml:"cloudflare,omitempty"`
	Google        GoogleSettings     `toml:"google,omitempty"`
	Social        SocialSettings     `toml:"social,omitempty"`
	WorkOS        WorkOSSettings     `toml:"workos,omitempty"`
	AWS           AWSSettings        `toml:"aws,omitempty"`
	GCP           GCPSettings        `toml:"gcp,omitempty"`
	OCI           OCISettings        `toml:"oci,omitempty"`
	Dyad          DyadSettings       `toml:"dyad"`
	Shell         ShellSettings      `toml:"shell"`
	Metadata      SettingsMetadata   `toml:"metadata,omitempty"`
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
	ActorImage        string           `toml:"actor_image,omitempty"`
	CriticImage       string           `toml:"critic_image,omitempty"`
	CodexModel        string           `toml:"codex_model,omitempty"`
	CodexEffortActor  string           `toml:"codex_effort_actor,omitempty"`
	CodexEffortCritic string           `toml:"codex_effort_critic,omitempty"`
	CodexModelLow     string           `toml:"codex_model_low,omitempty"`
	CodexModelMedium  string           `toml:"codex_model_medium,omitempty"`
	CodexModelHigh    string           `toml:"codex_model_high,omitempty"`
	CodexEffortLow    string           `toml:"codex_effort_low,omitempty"`
	CodexEffortMedium string           `toml:"codex_effort_medium,omitempty"`
	CodexEffortHigh   string           `toml:"codex_effort_high,omitempty"`
	Workspace         string           `toml:"workspace,omitempty"`
	Configs           string           `toml:"configs,omitempty"`
	ForwardPorts      string           `toml:"forward_ports,omitempty"`
	DockerSocket      *bool            `toml:"docker_socket,omitempty"`
	Loop              DyadLoopSettings `toml:"loop,omitempty"`
}

type DyadLoopSettings struct {
	Enabled             *bool  `toml:"enabled,omitempty"`
	Goal                string `toml:"goal,omitempty"`
	SeedCriticPrompt    string `toml:"seed_critic_prompt,omitempty"`
	MaxTurns            int    `toml:"max_turns,omitempty"`
	SleepSeconds        int    `toml:"sleep_seconds,omitempty"`
	StartupDelaySeconds int    `toml:"startup_delay_seconds,omitempty"`
	TurnTimeoutSeconds  int    `toml:"turn_timeout_seconds,omitempty"`
	RetryMax            int    `toml:"retry_max,omitempty"`
	RetryBaseSeconds    int    `toml:"retry_base_seconds,omitempty"`
	PromptLines         int    `toml:"prompt_lines,omitempty"`
	AllowMCPStartup     *bool  `toml:"allow_mcp_startup,omitempty"`
	TmuxCapture         string `toml:"tmux_capture,omitempty"`
	PausePollSeconds    int    `toml:"pause_poll_seconds,omitempty"`
}

type VaultSettings struct {
	Dir        string `toml:"dir,omitempty"`
	TrustStore string `toml:"trust_store,omitempty"`
	AuditLog   string `toml:"audit_log,omitempty"`

	// KeyBackend selects where the device private key is stored.
	// Supported values: keyring, file
	KeyBackend string `toml:"key_backend,omitempty"`

	// KeyFile is used when KeyBackend=file (or when file fallback is explicitly enabled).
	KeyFile string `toml:"key_file,omitempty"`
}

type StripeSettings struct {
	Organization   string                          `toml:"organization,omitempty"`
	DefaultAccount string                          `toml:"default_account,omitempty"`
	DefaultEnv     string                          `toml:"default_env,omitempty"`
	LogFile        string                          `toml:"log_file,omitempty"`
	Accounts       map[string]StripeAccountSetting `toml:"accounts,omitempty"`
}

type StripeAccountSetting struct {
	ID            string `toml:"id,omitempty"`
	Name          string `toml:"name,omitempty"`
	LiveKey       string `toml:"live_key,omitempty"`
	SandboxKey    string `toml:"sandbox_key,omitempty"`
	LiveKeyEnv    string `toml:"live_key_env,omitempty"`
	SandboxKeyEnv string `toml:"sandbox_key_env,omitempty"`
}

type GitHubSettings struct {
	DefaultAccount  string                        `toml:"default_account,omitempty"`
	DefaultAuthMode string                        `toml:"default_auth_mode,omitempty"`
	APIBaseURL      string                        `toml:"api_base_url,omitempty"`
	DefaultOwner    string                        `toml:"default_owner,omitempty"`
	VaultFile       string                        `toml:"vault_file,omitempty"`
	LogFile         string                        `toml:"log_file,omitempty"`
	Accounts        map[string]GitHubAccountEntry `toml:"accounts,omitempty"`
}

type GitHubAccountEntry struct {
	Name             string `toml:"name,omitempty"`
	Owner            string `toml:"owner,omitempty"`
	APIBaseURL       string `toml:"api_base_url,omitempty"`
	AuthMode         string `toml:"auth_mode,omitempty"`
	VaultPrefix      string `toml:"vault_prefix,omitempty"`
	AppID            int64  `toml:"app_id,omitempty"`
	AppIDEnv         string `toml:"app_id_env,omitempty"`
	AppPrivateKeyPEM string `toml:"app_private_key_pem,omitempty"`
	AppPrivateKeyEnv string `toml:"app_private_key_env,omitempty"`
	InstallationID   int64  `toml:"installation_id,omitempty"`
	InstallationEnv  string `toml:"installation_id_env,omitempty"`
}

type CloudflareSettings struct {
	DefaultAccount string                            `toml:"default_account,omitempty"`
	DefaultEnv     string                            `toml:"default_env,omitempty"`
	APIBaseURL     string                            `toml:"api_base_url,omitempty"`
	VaultFile      string                            `toml:"vault_file,omitempty"`
	LogFile        string                            `toml:"log_file,omitempty"`
	Accounts       map[string]CloudflareAccountEntry `toml:"accounts,omitempty"`
}

type CloudflareAccountEntry struct {
	Name            string `toml:"name,omitempty"`
	AccountID       string `toml:"account_id,omitempty"`
	AccountIDEnv    string `toml:"account_id_env,omitempty"`
	APIBaseURL      string `toml:"api_base_url,omitempty"`
	VaultPrefix     string `toml:"vault_prefix,omitempty"`
	DefaultZoneID   string `toml:"default_zone_id,omitempty"`
	DefaultZoneName string `toml:"default_zone_name,omitempty"`
	ProdZoneID      string `toml:"prod_zone_id,omitempty"`
	StagingZoneID   string `toml:"staging_zone_id,omitempty"`
	DevZoneID       string `toml:"dev_zone_id,omitempty"`
	APITokenEnv     string `toml:"api_token_env,omitempty"`
}

type GoogleSettings struct {
	DefaultAccount string                        `toml:"default_account,omitempty"`
	DefaultEnv     string                        `toml:"default_env,omitempty"`
	APIBaseURL     string                        `toml:"api_base_url,omitempty"`
	VaultFile      string                        `toml:"vault_file,omitempty"`
	LogFile        string                        `toml:"log_file,omitempty"`
	YouTube        GoogleYouTubeSettings         `toml:"youtube,omitempty"`
	Accounts       map[string]GoogleAccountEntry `toml:"accounts,omitempty"`
}

type GoogleAccountEntry struct {
	Name                   string `toml:"name,omitempty"`
	ProjectID              string `toml:"project_id,omitempty"`
	ProjectIDEnv           string `toml:"project_id_env,omitempty"`
	APIBaseURL             string `toml:"api_base_url,omitempty"`
	VaultPrefix            string `toml:"vault_prefix,omitempty"`
	PlacesAPIKeyEnv        string `toml:"places_api_key_env,omitempty"`
	ProdPlacesAPIKeyEnv    string `toml:"prod_places_api_key_env,omitempty"`
	StagingPlacesAPIKeyEnv string `toml:"staging_places_api_key_env,omitempty"`
	DevPlacesAPIKeyEnv     string `toml:"dev_places_api_key_env,omitempty"`
	DefaultRegionCode      string `toml:"default_region_code,omitempty"`
	DefaultLanguageCode    string `toml:"default_language_code,omitempty"`
}

type GoogleYouTubeSettings struct {
	APIBaseURL      string                               `toml:"api_base_url,omitempty"`
	UploadBaseURL   string                               `toml:"upload_base_url,omitempty"`
	DefaultAuthMode string                               `toml:"default_auth_mode,omitempty"`
	UploadChunkSize int                                  `toml:"upload_chunk_size_mb,omitempty"`
	Accounts        map[string]GoogleYouTubeAccountEntry `toml:"accounts,omitempty"`
}

type GoogleYouTubeAccountEntry struct {
	Name                    string `toml:"name,omitempty"`
	ProjectID               string `toml:"project_id,omitempty"`
	ProjectIDEnv            string `toml:"project_id_env,omitempty"`
	VaultPrefix             string `toml:"vault_prefix,omitempty"`
	YouTubeAPIKeyEnv        string `toml:"youtube_api_key_env,omitempty"`
	ProdYouTubeAPIKeyEnv    string `toml:"prod_youtube_api_key_env,omitempty"`
	StagingYouTubeAPIKeyEnv string `toml:"staging_youtube_api_key_env,omitempty"`
	DevYouTubeAPIKeyEnv     string `toml:"dev_youtube_api_key_env,omitempty"`
	YouTubeClientIDEnv      string `toml:"youtube_client_id_env,omitempty"`
	YouTubeClientSecretEnv  string `toml:"youtube_client_secret_env,omitempty"`
	YouTubeRedirectURIEnv   string `toml:"youtube_redirect_uri_env,omitempty"`
	YouTubeRefreshTokenEnv  string `toml:"youtube_refresh_token_env,omitempty"`
	DefaultRegionCode       string `toml:"default_region_code,omitempty"`
	DefaultLanguageCode     string `toml:"default_language_code,omitempty"`
}

type SocialSettings struct {
	DefaultAccount string                          `toml:"default_account,omitempty"`
	DefaultEnv     string                          `toml:"default_env,omitempty"`
	LogFile        string                          `toml:"log_file,omitempty"`
	Facebook       SocialPlatformSettings          `toml:"facebook,omitempty"`
	Instagram      SocialPlatformSettings          `toml:"instagram,omitempty"`
	X              SocialPlatformSettings          `toml:"x,omitempty"`
	LinkedIn       SocialPlatformSettings          `toml:"linkedin,omitempty"`
	Accounts       map[string]SocialAccountSetting `toml:"accounts,omitempty"`
}

type SocialPlatformSettings struct {
	APIBaseURL string `toml:"api_base_url,omitempty"`
	APIVersion string `toml:"api_version,omitempty"`
	AuthStyle  string `toml:"auth_style,omitempty"`
}

type SocialAccountSetting struct {
	Name string `toml:"name,omitempty"`

	VaultPrefix string `toml:"vault_prefix,omitempty"`

	FacebookAccessTokenEnv  string `toml:"facebook_access_token_env,omitempty"`
	InstagramAccessTokenEnv string `toml:"instagram_access_token_env,omitempty"`
	XAccessTokenEnv         string `toml:"x_access_token_env,omitempty"`
	LinkedInAccessTokenEnv  string `toml:"linkedin_access_token_env,omitempty"`

	FacebookPageID          string `toml:"facebook_page_id,omitempty"`
	InstagramBusinessID     string `toml:"instagram_business_id,omitempty"`
	XUserID                 string `toml:"x_user_id,omitempty"`
	XUsername               string `toml:"x_username,omitempty"`
	LinkedInPersonURN       string `toml:"linkedin_person_urn,omitempty"`
	LinkedInOrganizationURN string `toml:"linkedin_organization_urn,omitempty"`
}

type WorkOSSettings struct {
	DefaultAccount        string                        `toml:"default_account,omitempty"`
	DefaultEnv            string                        `toml:"default_env,omitempty"`
	APIBaseURL            string                        `toml:"api_base_url,omitempty"`
	DefaultOrganizationID string                        `toml:"default_organization_id,omitempty"`
	LogFile               string                        `toml:"log_file,omitempty"`
	Accounts              map[string]WorkOSAccountEntry `toml:"accounts,omitempty"`
}

type WorkOSAccountEntry struct {
	Name             string `toml:"name,omitempty"`
	VaultPrefix      string `toml:"vault_prefix,omitempty"`
	APIBaseURL       string `toml:"api_base_url,omitempty"`
	APIKeyEnv        string `toml:"api_key_env,omitempty"`
	ProdAPIKeyEnv    string `toml:"prod_api_key_env,omitempty"`
	StagingAPIKeyEnv string `toml:"staging_api_key_env,omitempty"`
	DevAPIKeyEnv     string `toml:"dev_api_key_env,omitempty"`
	ClientIDEnv      string `toml:"client_id_env,omitempty"`
	OrganizationID   string `toml:"organization_id,omitempty"`
}

type AWSSettings struct {
	DefaultAccount string                     `toml:"default_account,omitempty"`
	DefaultRegion  string                     `toml:"default_region,omitempty"`
	APIBaseURL     string                     `toml:"api_base_url,omitempty"`
	LogFile        string                     `toml:"log_file,omitempty"`
	Accounts       map[string]AWSAccountEntry `toml:"accounts,omitempty"`
}

type AWSAccountEntry struct {
	Name               string `toml:"name,omitempty"`
	VaultPrefix        string `toml:"vault_prefix,omitempty"`
	Region             string `toml:"region,omitempty"`
	AccessKeyIDEnv     string `toml:"access_key_id_env,omitempty"`
	SecretAccessKeyEnv string `toml:"secret_access_key_env,omitempty"`
	SessionTokenEnv    string `toml:"session_token_env,omitempty"`
}

type GCPSettings struct {
	DefaultAccount string                     `toml:"default_account,omitempty"`
	DefaultEnv     string                     `toml:"default_env,omitempty"`
	APIBaseURL     string                     `toml:"api_base_url,omitempty"`
	LogFile        string                     `toml:"log_file,omitempty"`
	Accounts       map[string]GCPAccountEntry `toml:"accounts,omitempty"`
}

type GCPAccountEntry struct {
	Name           string `toml:"name,omitempty"`
	VaultPrefix    string `toml:"vault_prefix,omitempty"`
	ProjectID      string `toml:"project_id,omitempty"`
	ProjectIDEnv   string `toml:"project_id_env,omitempty"`
	AccessTokenEnv string `toml:"access_token_env,omitempty"`
	APIBaseURL     string `toml:"api_base_url,omitempty"`
}

type OCISettings struct {
	DefaultAccount string                     `toml:"default_account,omitempty"`
	Profile        string                     `toml:"profile,omitempty"`
	ConfigFile     string                     `toml:"config_file,omitempty"`
	Region         string                     `toml:"region,omitempty"`
	APIBaseURL     string                     `toml:"api_base_url,omitempty"`
	LogFile        string                     `toml:"log_file,omitempty"`
	Accounts       map[string]OCIAccountEntry `toml:"accounts,omitempty"`
}

type OCIAccountEntry struct {
	Name              string `toml:"name,omitempty"`
	VaultPrefix       string `toml:"vault_prefix,omitempty"`
	Profile           string `toml:"profile,omitempty"`
	ConfigFile        string `toml:"config_file,omitempty"`
	Region            string `toml:"region,omitempty"`
	TenancyOCID       string `toml:"tenancy_ocid,omitempty"`
	TenancyOCIDEnv    string `toml:"tenancy_ocid_env,omitempty"`
	UserOCID          string `toml:"user_ocid,omitempty"`
	UserOCIDEnv       string `toml:"user_ocid_env,omitempty"`
	Fingerprint       string `toml:"fingerprint,omitempty"`
	FingerprintEnv    string `toml:"fingerprint_env,omitempty"`
	PrivateKeyPath    string `toml:"private_key_path,omitempty"`
	PrivateKeyPathEnv string `toml:"private_key_path_env,omitempty"`
	PassphraseEnv     string `toml:"passphrase_env,omitempty"`
	CompartmentID     string `toml:"compartment_id,omitempty"`
	CompartmentIDEnv  string `toml:"compartment_id_env,omitempty"`
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
	if settings.Vault.Dir == "" {
		settings.Vault.Dir = "vault"
	}
	if settings.Vault.TrustStore == "" {
		settings.Vault.TrustStore = "~/.si/vault/trust.json"
	}
	if settings.Vault.AuditLog == "" {
		settings.Vault.AuditLog = "~/.si/logs/vault.log"
	}
	if settings.Vault.KeyBackend == "" {
		settings.Vault.KeyBackend = "keyring"
	}
	if settings.Vault.KeyFile == "" {
		settings.Vault.KeyFile = "~/.si/vault/keys/age.key"
	}
	settings.Dyad.Loop.TmuxCapture = strings.ToLower(strings.TrimSpace(settings.Dyad.Loop.TmuxCapture))
	switch settings.Dyad.Loop.TmuxCapture {
	case "", "main", "alt":
	default:
		settings.Dyad.Loop.TmuxCapture = "main"
	}
	if settings.Dyad.Loop.TmuxCapture == "" {
		settings.Dyad.Loop.TmuxCapture = "main"
	}
	if settings.Dyad.Loop.MaxTurns < 0 {
		settings.Dyad.Loop.MaxTurns = 0
	}
	if strings.TrimSpace(settings.Dyad.Loop.Goal) == "" {
		settings.Dyad.Loop.Goal = "Continuously improve the task outcome through actor execution and critic review."
	}
	if settings.Dyad.Loop.SleepSeconds <= 0 {
		settings.Dyad.Loop.SleepSeconds = 3
	}
	if settings.Dyad.Loop.StartupDelaySeconds <= 0 {
		settings.Dyad.Loop.StartupDelaySeconds = 2
	}
	if settings.Dyad.Loop.TurnTimeoutSeconds <= 0 {
		settings.Dyad.Loop.TurnTimeoutSeconds = 900
	}
	if settings.Dyad.Loop.RetryMax <= 0 {
		settings.Dyad.Loop.RetryMax = 3
	}
	if settings.Dyad.Loop.RetryBaseSeconds <= 0 {
		settings.Dyad.Loop.RetryBaseSeconds = 2
	}
	if settings.Dyad.Loop.PromptLines <= 0 {
		settings.Dyad.Loop.PromptLines = 3
	}
	if settings.Dyad.Loop.PausePollSeconds <= 0 {
		settings.Dyad.Loop.PausePollSeconds = 5
	}
	settings.Stripe.DefaultEnv = normalizeStripeEnvironment(settings.Stripe.DefaultEnv)
	if settings.Stripe.DefaultEnv == "" {
		settings.Stripe.DefaultEnv = "sandbox"
	}
	settings.Github.DefaultAuthMode = strings.ToLower(strings.TrimSpace(settings.Github.DefaultAuthMode))
	if settings.Github.DefaultAuthMode == "" {
		settings.Github.DefaultAuthMode = "app"
	}
	settings.Github.APIBaseURL = strings.TrimSpace(settings.Github.APIBaseURL)
	if settings.Github.APIBaseURL == "" {
		settings.Github.APIBaseURL = "https://api.github.com"
	}
	settings.Github.DefaultOwner = strings.TrimSpace(settings.Github.DefaultOwner)
	settings.Github.DefaultAccount = strings.TrimSpace(settings.Github.DefaultAccount)
	settings.Cloudflare.DefaultEnv = normalizeCloudflareEnvironment(settings.Cloudflare.DefaultEnv)
	if settings.Cloudflare.DefaultEnv == "" {
		settings.Cloudflare.DefaultEnv = "prod"
	}
	settings.Cloudflare.APIBaseURL = strings.TrimSpace(settings.Cloudflare.APIBaseURL)
	if settings.Cloudflare.APIBaseURL == "" {
		settings.Cloudflare.APIBaseURL = "https://api.cloudflare.com/client/v4"
	}
	settings.Cloudflare.DefaultAccount = strings.TrimSpace(settings.Cloudflare.DefaultAccount)
	settings.Google.DefaultEnv = normalizeGoogleEnvironment(settings.Google.DefaultEnv)
	if settings.Google.DefaultEnv == "" {
		settings.Google.DefaultEnv = "prod"
	}
	settings.Google.APIBaseURL = strings.TrimSpace(settings.Google.APIBaseURL)
	if settings.Google.APIBaseURL == "" {
		settings.Google.APIBaseURL = "https://places.googleapis.com"
	}
	settings.Google.DefaultAccount = strings.TrimSpace(settings.Google.DefaultAccount)
	settings.Google.YouTube.DefaultAuthMode = strings.ToLower(strings.TrimSpace(settings.Google.YouTube.DefaultAuthMode))
	switch settings.Google.YouTube.DefaultAuthMode {
	case "api-key", "oauth":
	default:
		settings.Google.YouTube.DefaultAuthMode = "api-key"
	}
	settings.Google.YouTube.APIBaseURL = strings.TrimSpace(settings.Google.YouTube.APIBaseURL)
	if settings.Google.YouTube.APIBaseURL == "" {
		settings.Google.YouTube.APIBaseURL = "https://www.googleapis.com"
	}
	settings.Google.YouTube.UploadBaseURL = strings.TrimSpace(settings.Google.YouTube.UploadBaseURL)
	if settings.Google.YouTube.UploadBaseURL == "" {
		settings.Google.YouTube.UploadBaseURL = "https://www.googleapis.com/upload"
	}
	if settings.Google.YouTube.UploadChunkSize <= 0 {
		settings.Google.YouTube.UploadChunkSize = 16
	}
	settings.Social.DefaultEnv = normalizeSocialEnvironment(settings.Social.DefaultEnv)
	if settings.Social.DefaultEnv == "" {
		settings.Social.DefaultEnv = "prod"
	}
	settings.Social.DefaultAccount = strings.TrimSpace(settings.Social.DefaultAccount)
	facebookSpec := providers.Resolve(providers.SocialFacebook)
	instagramSpec := providers.Resolve(providers.SocialInstagram)
	xSpec := providers.Resolve(providers.SocialX)
	linkedinSpec := providers.Resolve(providers.SocialLinkedIn)
	settings.Social.Facebook.AuthStyle = normalizeSocialAuthStyle(settings.Social.Facebook.AuthStyle)
	if settings.Social.Facebook.AuthStyle == "" {
		settings.Social.Facebook.AuthStyle = firstNonEmpty(settings.Social.Facebook.AuthStyle, facebookSpec.AuthStyle, "query")
	}
	settings.Social.Instagram.AuthStyle = normalizeSocialAuthStyle(settings.Social.Instagram.AuthStyle)
	if settings.Social.Instagram.AuthStyle == "" {
		settings.Social.Instagram.AuthStyle = firstNonEmpty(settings.Social.Instagram.AuthStyle, instagramSpec.AuthStyle, "query")
	}
	settings.Social.X.AuthStyle = normalizeSocialAuthStyle(settings.Social.X.AuthStyle)
	if settings.Social.X.AuthStyle == "" {
		settings.Social.X.AuthStyle = firstNonEmpty(settings.Social.X.AuthStyle, xSpec.AuthStyle, "bearer")
	}
	settings.Social.LinkedIn.AuthStyle = normalizeSocialAuthStyle(settings.Social.LinkedIn.AuthStyle)
	if settings.Social.LinkedIn.AuthStyle == "" {
		settings.Social.LinkedIn.AuthStyle = firstNonEmpty(settings.Social.LinkedIn.AuthStyle, linkedinSpec.AuthStyle, "bearer")
	}
	settings.Social.Facebook.APIBaseURL = strings.TrimSpace(settings.Social.Facebook.APIBaseURL)
	if settings.Social.Facebook.APIBaseURL == "" {
		settings.Social.Facebook.APIBaseURL = firstNonEmpty(settings.Social.Facebook.APIBaseURL, facebookSpec.BaseURL, "https://graph.facebook.com")
	}
	settings.Social.Instagram.APIBaseURL = strings.TrimSpace(settings.Social.Instagram.APIBaseURL)
	if settings.Social.Instagram.APIBaseURL == "" {
		settings.Social.Instagram.APIBaseURL = firstNonEmpty(settings.Social.Instagram.APIBaseURL, instagramSpec.BaseURL, "https://graph.facebook.com")
	}
	settings.Social.X.APIBaseURL = strings.TrimSpace(settings.Social.X.APIBaseURL)
	if settings.Social.X.APIBaseURL == "" {
		settings.Social.X.APIBaseURL = firstNonEmpty(settings.Social.X.APIBaseURL, xSpec.BaseURL, "https://api.twitter.com")
	}
	settings.Social.LinkedIn.APIBaseURL = strings.TrimSpace(settings.Social.LinkedIn.APIBaseURL)
	if settings.Social.LinkedIn.APIBaseURL == "" {
		settings.Social.LinkedIn.APIBaseURL = firstNonEmpty(settings.Social.LinkedIn.APIBaseURL, linkedinSpec.BaseURL, "https://api.linkedin.com")
	}
	settings.Social.Facebook.APIVersion = strings.TrimSpace(settings.Social.Facebook.APIVersion)
	if settings.Social.Facebook.APIVersion == "" {
		settings.Social.Facebook.APIVersion = firstNonEmpty(settings.Social.Facebook.APIVersion, facebookSpec.APIVersion, "v22.0")
	}
	settings.Social.Instagram.APIVersion = strings.TrimSpace(settings.Social.Instagram.APIVersion)
	if settings.Social.Instagram.APIVersion == "" {
		settings.Social.Instagram.APIVersion = firstNonEmpty(settings.Social.Instagram.APIVersion, instagramSpec.APIVersion, "v22.0")
	}
	settings.Social.X.APIVersion = strings.TrimSpace(settings.Social.X.APIVersion)
	if settings.Social.X.APIVersion == "" {
		settings.Social.X.APIVersion = firstNonEmpty(settings.Social.X.APIVersion, xSpec.APIVersion, "2")
	}
	settings.Social.LinkedIn.APIVersion = strings.TrimSpace(settings.Social.LinkedIn.APIVersion)
	if settings.Social.LinkedIn.APIVersion == "" {
		settings.Social.LinkedIn.APIVersion = firstNonEmpty(settings.Social.LinkedIn.APIVersion, linkedinSpec.APIVersion, "v2")
	}
	settings.WorkOS.DefaultEnv = normalizeWorkOSEnvironment(settings.WorkOS.DefaultEnv)
	if settings.WorkOS.DefaultEnv == "" {
		settings.WorkOS.DefaultEnv = "prod"
	}
	settings.WorkOS.DefaultAccount = strings.TrimSpace(settings.WorkOS.DefaultAccount)
	settings.WorkOS.DefaultOrganizationID = strings.TrimSpace(settings.WorkOS.DefaultOrganizationID)
	settings.WorkOS.APIBaseURL = strings.TrimSpace(settings.WorkOS.APIBaseURL)
	if settings.WorkOS.APIBaseURL == "" {
		workosSpec := providers.Resolve(providers.WorkOS)
		settings.WorkOS.APIBaseURL = firstNonEmpty(workosSpec.BaseURL, "https://api.workos.com")
	}
	settings.AWS.DefaultAccount = strings.TrimSpace(settings.AWS.DefaultAccount)
	settings.AWS.DefaultRegion = strings.TrimSpace(settings.AWS.DefaultRegion)
	if settings.AWS.DefaultRegion == "" {
		settings.AWS.DefaultRegion = "us-east-1"
	}
	settings.AWS.APIBaseURL = strings.TrimSpace(settings.AWS.APIBaseURL)
	if settings.AWS.APIBaseURL == "" {
		awsSpec := providers.Resolve(providers.AWSIAM)
		settings.AWS.APIBaseURL = firstNonEmpty(awsSpec.BaseURL, "https://iam.amazonaws.com")
	}
	settings.GCP.DefaultAccount = strings.TrimSpace(settings.GCP.DefaultAccount)
	settings.GCP.DefaultEnv = normalizeIntegrationEnvironment(settings.GCP.DefaultEnv)
	if settings.GCP.DefaultEnv == "" {
		settings.GCP.DefaultEnv = "prod"
	}
	settings.GCP.APIBaseURL = strings.TrimSpace(settings.GCP.APIBaseURL)
	if settings.GCP.APIBaseURL == "" {
		gcpSpec := providers.Resolve(providers.GCPServiceUsage)
		settings.GCP.APIBaseURL = firstNonEmpty(gcpSpec.BaseURL, "https://serviceusage.googleapis.com")
	}
	settings.OCI.DefaultAccount = strings.TrimSpace(settings.OCI.DefaultAccount)
	settings.OCI.Profile = strings.TrimSpace(settings.OCI.Profile)
	if settings.OCI.Profile == "" {
		settings.OCI.Profile = "DEFAULT"
	}
	settings.OCI.ConfigFile = strings.TrimSpace(settings.OCI.ConfigFile)
	if settings.OCI.ConfigFile == "" {
		settings.OCI.ConfigFile = "~/.oci/config"
	}
	settings.OCI.Region = strings.TrimSpace(settings.OCI.Region)
	if settings.OCI.Region == "" {
		settings.OCI.Region = "us-ashburn-1"
	}
	settings.OCI.APIBaseURL = strings.TrimSpace(settings.OCI.APIBaseURL)
	if settings.OCI.APIBaseURL == "" {
		settings.OCI.APIBaseURL = "https://iaas." + settings.OCI.Region + ".oraclecloud.com"
	}
}

func normalizeIntegrationEnvironment(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "prod", "staging", "dev":
		return value
	default:
		return ""
	}
}

func loadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		settings := defaultSettings()
		applySettingsDefaults(&settings)
		return settings, err
	}
	data, err := readLocalFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			settings := defaultSettings()
			applySettingsDefaults(&settings)
			_ = saveSettings(settings)
			return settings, nil
		}
		settings := defaultSettings()
		applySettingsDefaults(&settings)
		return settings, fmt.Errorf("read settings %s: %w", path, err)
	}
	settings := defaultSettings()
	if err := toml.Unmarshal(data, &settings); err != nil {
		fallback := defaultSettings()
		applySettingsDefaults(&fallback)
		return fallback, fmt.Errorf("parse settings %s: %w", path, err)
	}
	applySettingsDefaults(&settings)
	return settings, nil
}

func loadSettingsOrDefault() Settings {
	settings, err := loadSettings()
	if err != nil {
		warnf("settings load failed: %v", err)
		fallback := defaultSettings()
		applySettingsDefaults(&fallback)
		return fallback
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
