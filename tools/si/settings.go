package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"

	"si/tools/si/internal/orbital"
	"si/tools/si/internal/providers"
)

type Settings struct {
	SchemaVersion int                   `toml:"schema_version"`
	Paths         SettingsPaths         `toml:"paths"`
	Codex         CodexSettings         `toml:"codex"`
	Vault         VaultSettings         `toml:"vault,omitempty"`
	Paas          PaasSettings          `toml:"paas,omitempty"`
	Stripe        StripeSettings        `toml:"stripe,omitempty"`
	Github        GitHubSettings        `toml:"github,omitempty"`
	Cloudflare    CloudflareSettings    `toml:"cloudflare,omitempty"`
	Google        GoogleSettings        `toml:"google,omitempty"`
	Apple         AppleSettings         `toml:"apple,omitempty"`
	Social        SocialSettings        `toml:"social,omitempty"`
	WorkOS        WorkOSSettings        `toml:"workos,omitempty"`
	AWS           AWSSettings           `toml:"aws,omitempty"`
	GCP           GCPSettings           `toml:"gcp,omitempty"`
	OpenAI        OpenAISettings        `toml:"openai,omitempty"`
	Surf          SurfSettings          `toml:"surf,omitempty"`
	RemoteControl RemoteControlSettings `toml:"remote_control,omitempty"`
	Viva          VivaSettings          `toml:"viva,omitempty"`
	Sun           SunSettings           `toml:"sun,omitempty"`
	OCI           OCISettings           `toml:"oci,omitempty"`
	Dyad          DyadSettings          `toml:"dyad"`
	Shell         ShellSettings         `toml:"shell"`
	Metadata      SettingsMetadata      `toml:"metadata,omitempty"`
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
	SkillsVolume string               `toml:"skills_volume,omitempty"`
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
	DefaultBrowser string `toml:"default_browser,omitempty"`
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
	SkillsVolume      string           `toml:"skills_volume,omitempty"`
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
	// File is the default logical Sun scope for `si vault` commands when --scope/--file is not provided.
	File       string `toml:"file,omitempty"`
	TrustStore string `toml:"trust_store,omitempty"`
	AuditLog   string `toml:"audit_log,omitempty"`
	// SyncBackend selects how vault state sync is handled.
	// Supported values: sun
	// - sun: Sun backup required on mutating vault operations
	SyncBackend string `toml:"sync_backend,omitempty"`

	// KeyBackend is deprecated in Sun remote mode and kept only for compatibility.
	KeyBackend string `toml:"key_backend,omitempty"`

	// KeyFile is deprecated in Sun remote mode and kept only for compatibility.
	KeyFile string `toml:"key_file,omitempty"`
}

type PaasSettings struct {
	// SyncBackend selects how paas state sync is handled.
	// Supported values: git, sun
	// - git: local/git-based only
	// - sun: Sun snapshot backup required on mutating paas operations
	SyncBackend string `toml:"sync_backend,omitempty"`

	// SnapshotName is the Sun object name used for paas control plane snapshots.
	// When empty, the current context name is used.
	SnapshotName string `toml:"snapshot_name,omitempty"`
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
	OAuthAccessToken string `toml:"oauth_access_token,omitempty"`
	OAuthTokenEnv    string `toml:"oauth_token_env,omitempty"`
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
	Play           GooglePlaySettings            `toml:"play,omitempty"`
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

type GooglePlaySettings struct {
	APIBaseURL       string                            `toml:"api_base_url,omitempty"`
	UploadBaseURL    string                            `toml:"upload_base_url,omitempty"`
	CustomAppBaseURL string                            `toml:"custom_app_base_url,omitempty"`
	Accounts         map[string]GooglePlayAccountEntry `toml:"accounts,omitempty"`
}

type GooglePlayAccountEntry struct {
	Name                         string `toml:"name,omitempty"`
	ProjectID                    string `toml:"project_id,omitempty"`
	ProjectIDEnv                 string `toml:"project_id_env,omitempty"`
	VaultPrefix                  string `toml:"vault_prefix,omitempty"`
	DeveloperAccountID           string `toml:"developer_account_id,omitempty"`
	DefaultPackageName           string `toml:"default_package_name,omitempty"`
	DefaultLanguageCode          string `toml:"default_language_code,omitempty"`
	ServiceAccountJSONEnv        string `toml:"service_account_json_env,omitempty"`
	ProdServiceAccountJSONEnv    string `toml:"prod_service_account_json_env,omitempty"`
	StagingServiceAccountJSONEnv string `toml:"staging_service_account_json_env,omitempty"`
	DevServiceAccountJSONEnv     string `toml:"dev_service_account_json_env,omitempty"`
	ServiceAccountFile           string `toml:"service_account_file,omitempty"`
	ServiceAccountFileEnv        string `toml:"service_account_file_env,omitempty"`
}

type AppleSettings struct {
	DefaultAccount string                `toml:"default_account,omitempty"`
	DefaultEnv     string                `toml:"default_env,omitempty"`
	APIBaseURL     string                `toml:"api_base_url,omitempty"`
	LogFile        string                `toml:"log_file,omitempty"`
	AppStore       AppleAppStoreSettings `toml:"appstore,omitempty"`
}

type AppleAppStoreSettings struct {
	APIBaseURL string                               `toml:"api_base_url,omitempty"`
	Accounts   map[string]AppleAppStoreAccountEntry `toml:"accounts,omitempty"`
}

type AppleAppStoreAccountEntry struct {
	Name              string `toml:"name,omitempty"`
	ProjectID         string `toml:"project_id,omitempty"`
	ProjectIDEnv      string `toml:"project_id_env,omitempty"`
	VaultPrefix       string `toml:"vault_prefix,omitempty"`
	IssuerID          string `toml:"issuer_id,omitempty"`
	IssuerIDEnv       string `toml:"issuer_id_env,omitempty"`
	KeyID             string `toml:"key_id,omitempty"`
	KeyIDEnv          string `toml:"key_id_env,omitempty"`
	PrivateKeyPEM     string `toml:"private_key_pem,omitempty"`
	PrivateKeyEnv     string `toml:"private_key_env,omitempty"`
	PrivateKeyFile    string `toml:"private_key_file,omitempty"`
	PrivateKeyFileEnv string `toml:"private_key_file_env,omitempty"`
	DefaultBundleID   string `toml:"default_bundle_id,omitempty"`
	DefaultLanguage   string `toml:"default_language,omitempty"`
	DefaultPlatform   string `toml:"default_platform,omitempty"`
}

type SocialSettings struct {
	DefaultAccount string                          `toml:"default_account,omitempty"`
	DefaultEnv     string                          `toml:"default_env,omitempty"`
	LogFile        string                          `toml:"log_file,omitempty"`
	Facebook       SocialPlatformSettings          `toml:"facebook,omitempty"`
	Instagram      SocialPlatformSettings          `toml:"instagram,omitempty"`
	X              SocialPlatformSettings          `toml:"x,omitempty"`
	LinkedIn       SocialPlatformSettings          `toml:"linkedin,omitempty"`
	Reddit         SocialPlatformSettings          `toml:"reddit,omitempty"`
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
	RedditAccessTokenEnv    string `toml:"reddit_access_token_env,omitempty"`

	FacebookPageID          string `toml:"facebook_page_id,omitempty"`
	InstagramBusinessID     string `toml:"instagram_business_id,omitempty"`
	XUserID                 string `toml:"x_user_id,omitempty"`
	XUsername               string `toml:"x_username,omitempty"`
	LinkedInPersonURN       string `toml:"linkedin_person_urn,omitempty"`
	LinkedInOrganizationURN string `toml:"linkedin_organization_urn,omitempty"`
	RedditUsername          string `toml:"reddit_username,omitempty"`
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
	APIKeyEnv      string `toml:"api_key_env,omitempty"`
	APIBaseURL     string `toml:"api_base_url,omitempty"`
}

type OpenAISettings struct {
	DefaultAccount        string                        `toml:"default_account,omitempty"`
	APIBaseURL            string                        `toml:"api_base_url,omitempty"`
	DefaultOrganizationID string                        `toml:"default_organization_id,omitempty"`
	DefaultProjectID      string                        `toml:"default_project_id,omitempty"`
	LogFile               string                        `toml:"log_file,omitempty"`
	Accounts              map[string]OpenAIAccountEntry `toml:"accounts,omitempty"`
}

type OpenAIAccountEntry struct {
	Name              string `toml:"name,omitempty"`
	VaultPrefix       string `toml:"vault_prefix,omitempty"`
	APIBaseURL        string `toml:"api_base_url,omitempty"`
	APIKeyEnv         string `toml:"api_key_env,omitempty"`
	AdminAPIKeyEnv    string `toml:"admin_api_key_env,omitempty"`
	OrganizationID    string `toml:"organization_id,omitempty"`
	OrganizationIDEnv string `toml:"organization_id_env,omitempty"`
	ProjectID         string `toml:"project_id,omitempty"`
	ProjectIDEnv      string `toml:"project_id_env,omitempty"`
}

type SurfSettings struct {
	Repo         string             `toml:"repo,omitempty"`
	Bin          string             `toml:"bin,omitempty"`
	Build        *bool              `toml:"build,omitempty"`
	SettingsFile string             `toml:"settings_file,omitempty"`
	StateDir     string             `toml:"state_dir,omitempty"`
	Tunnel       SurfTunnelSettings `toml:"tunnel,omitempty"`
}

type SurfTunnelSettings struct {
	Name     string `toml:"name,omitempty"`
	Mode     string `toml:"mode,omitempty"`
	VaultKey string `toml:"vault_key,omitempty"`
}

type RemoteControlSettings struct {
	Repo  string `toml:"repo,omitempty"`
	Bin   string `toml:"bin,omitempty"`
	Build *bool  `toml:"build,omitempty"`
}

type VivaSettings struct {
	Repo   string             `toml:"repo,omitempty"`
	Bin    string             `toml:"bin,omitempty"`
	Build  *bool              `toml:"build,omitempty"`
	Tunnel VivaTunnelSettings `toml:"tunnel,omitempty"`
}

type VivaTunnelSettings struct {
	DefaultProfile string                       `toml:"default_profile,omitempty"`
	Profiles       map[string]VivaTunnelProfile `toml:"profiles,omitempty"`
}

type VivaTunnelProfile struct {
	Name               string            `toml:"name,omitempty"`
	ContainerName      string            `toml:"container_name,omitempty"`
	TunnelIDEnvKey     string            `toml:"tunnel_id_env_key,omitempty"`
	CredentialsEnvKey  string            `toml:"credentials_env_key,omitempty"`
	MetricsAddr        string            `toml:"metrics_addr,omitempty"`
	Image              string            `toml:"image,omitempty"`
	NetworkMode        string            `toml:"network_mode,omitempty"`
	AdditionalNetworks []string          `toml:"additional_networks,omitempty"`
	NoAutoupdate       *bool             `toml:"no_autoupdate,omitempty"`
	PullImage          *bool             `toml:"pull_image,omitempty"`
	RuntimeDir         string            `toml:"runtime_dir,omitempty"`
	VaultEnvFile       string            `toml:"vault_env_file,omitempty"`
	VaultRepo          string            `toml:"vault_repo,omitempty"`
	VaultEnv           string            `toml:"vault_env,omitempty"`
	Routes             []VivaTunnelRoute `toml:"routes,omitempty"`
}

type VivaTunnelRoute struct {
	Hostname string `toml:"hostname,omitempty"`
	Service  string `toml:"service,omitempty"`
}

type SunSettings struct {
	BaseURL               string `toml:"base_url,omitempty"`
	Account               string `toml:"account,omitempty"`
	Token                 string `toml:"token,omitempty"`
	TimeoutSeconds        int    `toml:"timeout_seconds,omitempty"`
	AutoSync              bool   `toml:"auto_sync,omitempty"`
	OrbitGatewayRegistry  string `toml:"orbit_gateway_registry,omitempty"`
	OrbitGatewaySlots     int    `toml:"orbit_gateway_slots,omitempty"`
	Taskboard             string `toml:"taskboard,omitempty"`
	TaskboardAgent        string `toml:"taskboard_agent,omitempty"`
	TaskboardLeaseSeconds int    `toml:"taskboard_lease_seconds,omitempty"`
	MachineID             string `toml:"machine_id,omitempty"`
	OperatorID            string `toml:"operator_id,omitempty"`
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

func mergeSunSettings(settings *Settings) {
	if settings == nil {
		return
	}
	sun := settings.Sun
	if trimmed := strings.TrimSpace(sun.BaseURL); trimmed != "" {
		settings.Sun.BaseURL = trimmed
	}
	if trimmed := strings.TrimSpace(sun.Account); trimmed != "" {
		settings.Sun.Account = trimmed
	}
	if trimmed := strings.TrimSpace(sun.Token); trimmed != "" {
		settings.Sun.Token = trimmed
	}
	if sun.TimeoutSeconds > 0 {
		settings.Sun.TimeoutSeconds = sun.TimeoutSeconds
	}
	// Bool zero-value cannot distinguish unset from explicit false; prefer true when set.
	if sun.AutoSync {
		settings.Sun.AutoSync = true
	}
	if trimmed := strings.TrimSpace(sun.OrbitGatewayRegistry); trimmed != "" {
		settings.Sun.OrbitGatewayRegistry = trimmed
	}
	if sun.OrbitGatewaySlots > 0 {
		settings.Sun.OrbitGatewaySlots = sun.OrbitGatewaySlots
	}
	if trimmed := strings.TrimSpace(sun.Taskboard); trimmed != "" {
		settings.Sun.Taskboard = trimmed
	}
	if trimmed := strings.TrimSpace(sun.TaskboardAgent); trimmed != "" {
		settings.Sun.TaskboardAgent = trimmed
	}
	if sun.TaskboardLeaseSeconds > 0 {
		settings.Sun.TaskboardLeaseSeconds = sun.TaskboardLeaseSeconds
	}
	if trimmed := strings.TrimSpace(sun.MachineID); trimmed != "" {
		settings.Sun.MachineID = trimmed
	}
	if trimmed := strings.TrimSpace(sun.OperatorID); trimmed != "" {
		settings.Sun.OperatorID = trimmed
	}
}

const (
	settingsModuleCore          = "core"
	settingsModuleCodex         = "codex"
	settingsModuleDyad          = "dyad"
	settingsModuleVault         = "vault"
	settingsModulePaas          = "paas"
	settingsModuleStripe        = "stripe"
	settingsModuleGitHub        = "github"
	settingsModuleCloudflare    = "cloudflare"
	settingsModuleGoogle        = "google"
	settingsModuleApple         = "apple"
	settingsModuleSocial        = "social"
	settingsModuleWorkOS        = "workos"
	settingsModuleAWS           = "aws"
	settingsModuleGCP           = "gcp"
	settingsModuleOpenAI        = "openai"
	settingsModuleSurf          = "surf"
	settingsModuleRemoteControl = "remote-control"
	settingsModuleViva          = "viva"
	settingsModuleSun           = "sun"
	settingsModuleOCI           = "oci"
)

func settingsModuleNames() []string {
	return []string{
		settingsModuleCore,
		settingsModuleCodex,
		settingsModuleDyad,
		settingsModuleVault,
		settingsModulePaas,
		settingsModuleStripe,
		settingsModuleGitHub,
		settingsModuleCloudflare,
		settingsModuleGoogle,
		settingsModuleApple,
		settingsModuleSocial,
		settingsModuleWorkOS,
		settingsModuleAWS,
		settingsModuleGCP,
		settingsModuleOpenAI,
		settingsModuleSurf,
		settingsModuleRemoteControl,
		settingsModuleViva,
		settingsModuleSun,
		settingsModuleOCI,
	}
}

func settingsRootPath() (string, error) {
	home, err := settingsHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	return filepath.Join(home, ".si"), nil
}

func settingsPath() (string, error) {
	return settingsModulePath(settingsModuleCore)
}

func settingsModulePath(module string) (string, error) {
	root, err := settingsRootPath()
	if err != nil {
		return "", err
	}
	module = strings.TrimSpace(strings.ToLower(module))
	switch module {
	case settingsModuleCore:
		return filepath.Join(root, "settings.toml"), nil
	case settingsModuleSurf:
		// surf runtime already owns ~/.si/surf/settings.toml; keep SI wrapper
		// settings in a dedicated sibling file to avoid schema conflicts.
		return filepath.Join(root, "surf", "si.settings.toml"), nil
	case settingsModuleRemoteControl:
		// remote-control runtime owns ~/.si/remote-control/settings.toml; keep SI wrapper
		// settings in a dedicated sibling file to avoid schema conflicts.
		return filepath.Join(root, "remote-control", "si.settings.toml"), nil
	case "":
		return "", fmt.Errorf("settings module required")
	default:
		return filepath.Join(root, module, "settings.toml"), nil
	}
}

func settingsHomeDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SI_SETTINGS_HOME")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return home, err
	}
	// Guardrail: avoid writing root-owned settings into a non-root HOME path
	// when running as root with a preserved user HOME (e.g., sudo -E).
	//
	// If explicit SI_SETTINGS_HOME is needed for admin workflows, callers can
	// still opt in via that override. Without explicit override we should avoid
	// polluting user-owned ~/.si with root-owned files.
	if os.Geteuid() == 0 {
		rootHome, rootErr := homeDirByUID(0)
		if rootErr == nil && strings.TrimSpace(rootHome) != "" && !pathsEqual(rootHome, home) {
			if ownedByRoot, ownErr := pathOwnedByUID(home, 0); ownErr != nil || !ownedByRoot {
				home = rootHome
			}
		}
	}
	return home, nil
}

func hasSIStateInHome(home string) bool {
	home = strings.TrimSpace(home)
	if home == "" {
		return false
	}
	candidates := []string{
		filepath.Join(home, ".si", "settings.toml"),
		filepath.Join(home, ".si", "vault", "keys", "age.key"),
		filepath.Join(home, ".si", "vault", "trust.json"),
	}
	for _, module := range settingsModuleNames() {
		if module == settingsModuleCore {
			continue
		}
		moduleFile := "settings.toml"
		if module == settingsModuleSurf || module == settingsModuleRemoteControl {
			moduleFile = "si.settings.toml"
		}
		candidates = append(candidates, filepath.Join(home, ".si", module, moduleFile))
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			return true
		}
	}
	return false
}

func homeDirByUID(uid int) (string, error) {
	entry, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(entry.HomeDir), nil
}

func pathOwnedByUID(path string, uid uint32) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("unsupported file stat type for %s", path)
	}
	return stat.Uid == uid, nil
}

func pathsEqual(a, b string) bool {
	left := filepath.Clean(strings.TrimSpace(a))
	right := filepath.Clean(strings.TrimSpace(b))
	return left == right
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
	settings.Codex.Login.DefaultBrowser = normalizeLoginDefaultBrowser(settings.Codex.Login.DefaultBrowser)
	if settings.Codex.Login.DefaultBrowser == "" && strings.TrimSpace(settings.Codex.Login.OpenURLCommand) == "" {
		if runtime.GOOS == "darwin" {
			settings.Codex.Login.DefaultBrowser = "safari"
		} else {
			settings.Codex.Login.DefaultBrowser = "chrome"
		}
	}
	if settings.Vault.File == "" {
		settings.Vault.File = defaultVaultScope
	}
	if settings.Vault.TrustStore == "" {
		settings.Vault.TrustStore = ""
	}
	if settings.Vault.AuditLog == "" {
		settings.Vault.AuditLog = ""
	}
	if settings.Vault.KeyBackend == "" {
		settings.Vault.KeyBackend = "keyring"
	}
	if settings.Vault.KeyFile == "" {
		settings.Vault.KeyFile = ""
	}
	settings.Vault.SyncBackend = strings.ToLower(strings.TrimSpace(settings.Vault.SyncBackend))
	settings.Paas.SyncBackend = strings.ToLower(strings.TrimSpace(settings.Paas.SyncBackend))
	settings.Paas.SnapshotName = strings.TrimSpace(settings.Paas.SnapshotName)
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
	switch settings.Github.DefaultAuthMode {
	case "", "app", "oauth":
	default:
		settings.Github.DefaultAuthMode = "app"
	}
	if settings.Github.DefaultAuthMode == "" {
		settings.Github.DefaultAuthMode = "app"
	}
	settings.Github.APIBaseURL = strings.TrimSpace(settings.Github.APIBaseURL)
	if settings.Github.APIBaseURL == "" {
		settings.Github.APIBaseURL = "https://api.github.com"
	}
	settings.Github.DefaultOwner = strings.TrimSpace(settings.Github.DefaultOwner)
	settings.Github.DefaultAccount = strings.TrimSpace(settings.Github.DefaultAccount)
	if len(settings.Github.Accounts) > 0 {
		for alias, entry := range settings.Github.Accounts {
			entry.AuthMode = strings.ToLower(strings.TrimSpace(entry.AuthMode))
			switch entry.AuthMode {
			case "", "app", "oauth":
			default:
				entry.AuthMode = ""
			}
			settings.Github.Accounts[alias] = entry
		}
	}
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
	settings.Google.Play.APIBaseURL = strings.TrimSpace(settings.Google.Play.APIBaseURL)
	if settings.Google.Play.APIBaseURL == "" {
		settings.Google.Play.APIBaseURL = "https://androidpublisher.googleapis.com"
	}
	settings.Google.Play.UploadBaseURL = strings.TrimSpace(settings.Google.Play.UploadBaseURL)
	if settings.Google.Play.UploadBaseURL == "" {
		settings.Google.Play.UploadBaseURL = "https://androidpublisher.googleapis.com"
	}
	settings.Google.Play.CustomAppBaseURL = strings.TrimSpace(settings.Google.Play.CustomAppBaseURL)
	if settings.Google.Play.CustomAppBaseURL == "" {
		settings.Google.Play.CustomAppBaseURL = "https://playcustomapp.googleapis.com"
	}
	settings.Apple.DefaultEnv = normalizeIntegrationEnvironment(settings.Apple.DefaultEnv)
	if settings.Apple.DefaultEnv == "" {
		settings.Apple.DefaultEnv = "prod"
	}
	settings.Apple.DefaultAccount = strings.TrimSpace(settings.Apple.DefaultAccount)
	settings.Apple.APIBaseURL = strings.TrimSpace(settings.Apple.APIBaseURL)
	if settings.Apple.APIBaseURL == "" {
		settings.Apple.APIBaseURL = "https://api.appstoreconnect.apple.com"
	}
	settings.Apple.AppStore.APIBaseURL = strings.TrimSpace(settings.Apple.AppStore.APIBaseURL)
	if settings.Apple.AppStore.APIBaseURL == "" {
		settings.Apple.AppStore.APIBaseURL = settings.Apple.APIBaseURL
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
	redditSpec := providers.Resolve(providers.SocialReddit)
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
	settings.Social.Reddit.AuthStyle = normalizeSocialAuthStyle(settings.Social.Reddit.AuthStyle)
	if settings.Social.Reddit.AuthStyle == "" {
		settings.Social.Reddit.AuthStyle = firstNonEmpty(settings.Social.Reddit.AuthStyle, redditSpec.AuthStyle, "bearer")
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
	settings.Social.Reddit.APIBaseURL = strings.TrimSpace(settings.Social.Reddit.APIBaseURL)
	if settings.Social.Reddit.APIBaseURL == "" {
		settings.Social.Reddit.APIBaseURL = firstNonEmpty(settings.Social.Reddit.APIBaseURL, redditSpec.BaseURL, "https://oauth.reddit.com")
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
	settings.Social.Reddit.APIVersion = strings.TrimSpace(settings.Social.Reddit.APIVersion)
	if settings.Social.Reddit.APIVersion == "" {
		settings.Social.Reddit.APIVersion = firstNonEmpty(settings.Social.Reddit.APIVersion, redditSpec.APIVersion)
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
	settings.OpenAI.DefaultAccount = strings.TrimSpace(settings.OpenAI.DefaultAccount)
	settings.OpenAI.DefaultOrganizationID = strings.TrimSpace(settings.OpenAI.DefaultOrganizationID)
	settings.OpenAI.DefaultProjectID = strings.TrimSpace(settings.OpenAI.DefaultProjectID)
	settings.OpenAI.APIBaseURL = strings.TrimSpace(settings.OpenAI.APIBaseURL)
	if settings.OpenAI.APIBaseURL == "" {
		openAISpec := providers.Resolve(providers.OpenAI)
		settings.OpenAI.APIBaseURL = firstNonEmpty(openAISpec.BaseURL, "https://api.openai.com")
	}
	settings.Surf.Repo = strings.TrimSpace(settings.Surf.Repo)
	settings.Surf.Bin = strings.TrimSpace(settings.Surf.Bin)
	settings.Surf.SettingsFile = strings.TrimSpace(settings.Surf.SettingsFile)
	settings.Surf.StateDir = strings.TrimSpace(settings.Surf.StateDir)
	settings.Surf.Tunnel.Name = strings.TrimSpace(settings.Surf.Tunnel.Name)
	settings.Surf.Tunnel.VaultKey = strings.TrimSpace(settings.Surf.Tunnel.VaultKey)
	settings.Surf.Tunnel.Mode = strings.ToLower(strings.TrimSpace(settings.Surf.Tunnel.Mode))
	switch settings.Surf.Tunnel.Mode {
	case "", "quick", "token":
	default:
		settings.Surf.Tunnel.Mode = ""
	}
	settings.Viva.Repo = strings.TrimSpace(settings.Viva.Repo)
	settings.Viva.Bin = strings.TrimSpace(settings.Viva.Bin)
	settings.Viva.Tunnel.DefaultProfile = strings.TrimSpace(settings.Viva.Tunnel.DefaultProfile)
	if len(settings.Viva.Tunnel.Profiles) > 0 {
		norm := make(map[string]VivaTunnelProfile, len(settings.Viva.Tunnel.Profiles))
		for key, profile := range settings.Viva.Tunnel.Profiles {
			profileKey := strings.ToLower(strings.TrimSpace(key))
			if profileKey == "" {
				continue
			}
			norm[profileKey] = normalizeVivaTunnelProfile(profile)
		}
		settings.Viva.Tunnel.Profiles = norm
		if settings.Viva.Tunnel.DefaultProfile == "" {
			if _, ok := norm["dev"]; ok {
				settings.Viva.Tunnel.DefaultProfile = "dev"
			}
		}
		if settings.Viva.Tunnel.DefaultProfile != "" {
			settings.Viva.Tunnel.DefaultProfile = strings.ToLower(settings.Viva.Tunnel.DefaultProfile)
			if _, ok := settings.Viva.Tunnel.Profiles[settings.Viva.Tunnel.DefaultProfile]; !ok {
				settings.Viva.Tunnel.DefaultProfile = ""
			}
		}
	}
	mergeSunSettings(settings)
	settings.Sun.BaseURL = strings.TrimSpace(settings.Sun.BaseURL)
	if settings.Sun.BaseURL == "" {
		settings.Sun.BaseURL = "http://127.0.0.1:8080"
	}
	settings.Sun.Account = strings.TrimSpace(settings.Sun.Account)
	settings.Sun.Token = strings.TrimSpace(settings.Sun.Token)
	if settings.Sun.TimeoutSeconds <= 0 {
		settings.Sun.TimeoutSeconds = 15
	}
	settings.Sun.OrbitGatewayRegistry = strings.TrimSpace(settings.Sun.OrbitGatewayRegistry)
	if settings.Sun.OrbitGatewayRegistry == "" {
		settings.Sun.OrbitGatewayRegistry = defaultOrbitGatewayName
	}
	if settings.Sun.OrbitGatewaySlots <= 0 {
		settings.Sun.OrbitGatewaySlots = orbital.GatewayDefaultSlotsPerNamespace
	}
	if settings.Sun.OrbitGatewaySlots > orbital.GatewayMaxSlotsPerNamespace {
		settings.Sun.OrbitGatewaySlots = orbital.GatewayMaxSlotsPerNamespace
	}
	settings.Sun.Taskboard = strings.TrimSpace(settings.Sun.Taskboard)
	if settings.Sun.Taskboard == "" {
		settings.Sun.Taskboard = "default"
	}
	settings.Sun.TaskboardAgent = strings.TrimSpace(settings.Sun.TaskboardAgent)
	if settings.Sun.TaskboardLeaseSeconds <= 0 {
		settings.Sun.TaskboardLeaseSeconds = 1800
	}
	settings.Sun.MachineID = strings.TrimSpace(settings.Sun.MachineID)
	settings.Sun.OperatorID = strings.TrimSpace(settings.Sun.OperatorID)
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
	settings := defaultSettings()
	applySettingsDefaults(&settings)
	var loadedAny bool
	var missingAny bool
	for _, module := range settingsModuleNames() {
		path, err := settingsModulePath(module)
		if err != nil {
			return settings, err
		}
		data, err := readLocalFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				missingAny = true
				continue
			}
			fallback := defaultSettings()
			applySettingsDefaults(&fallback)
			return fallback, fmt.Errorf("read settings module %s (%s): %w", module, path, err)
		}
		if err := decodeSettingsModule(module, data, &settings); err != nil {
			fallback := defaultSettings()
			applySettingsDefaults(&fallback)
			return fallback, fmt.Errorf("parse settings module %s (%s): %w", module, path, err)
		}
		loadedAny = true
	}
	applySettingsDefaults(&settings)
	if !loadedAny || missingAny {
		_ = saveSettings(settings)
	}
	return settings, nil
}

var (
	loadSettingsForDefault = loadSettings
	settingsWarnOnceMu     sync.Mutex
	settingsWarnOnceSeen   = map[string]struct{}{}
)

func warnSettingsLoadFailedOnce(err error) {
	if err == nil {
		return
	}
	key := strings.TrimSpace(err.Error())
	if key == "" {
		key = "<empty settings error>"
	}
	settingsWarnOnceMu.Lock()
	_, alreadyWarned := settingsWarnOnceSeen[key]
	if !alreadyWarned {
		settingsWarnOnceSeen[key] = struct{}{}
	}
	settingsWarnOnceMu.Unlock()
	if alreadyWarned {
		return
	}
	warnf("settings load failed: %v", err)
}

func resetSettingsWarnOnceStateForTests() {
	settingsWarnOnceMu.Lock()
	settingsWarnOnceSeen = map[string]struct{}{}
	settingsWarnOnceMu.Unlock()
}

func loadSettingsOrDefault() Settings {
	settings, err := loadSettingsForDefault()
	if err != nil {
		warnSettingsLoadFailedOnce(err)
		fallback := defaultSettings()
		applySettingsDefaults(&fallback)
		return fallback
	}
	return settings
}

func saveSettings(settings Settings) error {
	applySettingsDefaults(&settings)
	settings.Metadata.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	for _, module := range settingsModuleNames() {
		path, err := settingsModulePath(module)
		if err != nil {
			return err
		}
		data, err := encodeSettingsModule(module, settings)
		if err != nil {
			return fmt.Errorf("marshal settings module %s: %w", module, err)
		}
		if err := writeSettingsFileAtomic(path, data); err != nil {
			return fmt.Errorf("write settings module %s (%s): %w", module, path, err)
		}
	}
	return nil
}

func writeSettingsFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
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

func decodeSettingsModule(module string, data []byte, settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("settings is nil")
	}
	switch module {
	case settingsModuleCore:
		var payload struct {
			SchemaVersion int              `toml:"schema_version"`
			Paths         SettingsPaths    `toml:"paths,omitempty"`
			Shell         ShellSettings    `toml:"shell,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		if payload.SchemaVersion > 0 {
			settings.SchemaVersion = payload.SchemaVersion
		}
		settings.Paths = payload.Paths
		settings.Shell = payload.Shell
		if strings.TrimSpace(payload.Metadata.UpdatedAt) != "" {
			settings.Metadata = payload.Metadata
		}
	case settingsModuleCodex:
		var payload struct {
			Codex CodexSettings `toml:"codex"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Codex = payload.Codex
	case settingsModuleDyad:
		var payload struct {
			Dyad DyadSettings `toml:"dyad"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Dyad = payload.Dyad
	case settingsModuleVault:
		var payload struct {
			Vault VaultSettings `toml:"vault,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Vault = payload.Vault
	case settingsModulePaas:
		var payload struct {
			Paas PaasSettings `toml:"paas,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Paas = payload.Paas
	case settingsModuleStripe:
		var payload struct {
			Stripe StripeSettings `toml:"stripe,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Stripe = payload.Stripe
	case settingsModuleGitHub:
		var payload struct {
			Github GitHubSettings `toml:"github,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Github = payload.Github
	case settingsModuleCloudflare:
		var payload struct {
			Cloudflare CloudflareSettings `toml:"cloudflare,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Cloudflare = payload.Cloudflare
	case settingsModuleGoogle:
		var payload struct {
			Google GoogleSettings `toml:"google,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Google = payload.Google
	case settingsModuleApple:
		var payload struct {
			Apple AppleSettings `toml:"apple,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Apple = payload.Apple
	case settingsModuleSocial:
		var payload struct {
			Social SocialSettings `toml:"social,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Social = payload.Social
	case settingsModuleWorkOS:
		var payload struct {
			WorkOS WorkOSSettings `toml:"workos,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.WorkOS = payload.WorkOS
	case settingsModuleAWS:
		var payload struct {
			AWS AWSSettings `toml:"aws,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.AWS = payload.AWS
	case settingsModuleGCP:
		var payload struct {
			GCP GCPSettings `toml:"gcp,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.GCP = payload.GCP
	case settingsModuleOpenAI:
		var payload struct {
			OpenAI OpenAISettings `toml:"openai,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.OpenAI = payload.OpenAI
	case settingsModuleSurf:
		var payload struct {
			Surf SurfSettings `toml:"surf,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Surf = payload.Surf
	case settingsModuleRemoteControl:
		var payload struct {
			RemoteControl RemoteControlSettings `toml:"remote_control,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.RemoteControl = payload.RemoteControl
	case settingsModuleViva:
		var payload struct {
			Viva VivaSettings `toml:"viva,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Viva = payload.Viva
	case settingsModuleSun:
		var payload struct {
			Sun SunSettings `toml:"sun,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.Sun = payload.Sun
	case settingsModuleOCI:
		var payload struct {
			OCI OCISettings `toml:"oci,omitempty"`
		}
		if err := toml.Unmarshal(data, &payload); err != nil {
			return err
		}
		settings.OCI = payload.OCI
	default:
		return fmt.Errorf("unsupported settings module: %s", module)
	}
	return nil
}

func encodeSettingsModule(module string, settings Settings) ([]byte, error) {
	switch module {
	case settingsModuleCore:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Paths         SettingsPaths    `toml:"paths,omitempty"`
			Shell         ShellSettings    `toml:"shell,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{
			SchemaVersion: settings.SchemaVersion,
			Paths:         settings.Paths,
			Shell:         settings.Shell,
			Metadata:      settings.Metadata,
		})
	case settingsModuleCodex:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Codex         CodexSettings    `toml:"codex"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Codex, settings.Metadata})
	case settingsModuleDyad:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Dyad          DyadSettings     `toml:"dyad"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Dyad, settings.Metadata})
	case settingsModuleVault:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Vault         VaultSettings    `toml:"vault,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Vault, settings.Metadata})
	case settingsModulePaas:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Paas          PaasSettings     `toml:"paas,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Paas, settings.Metadata})
	case settingsModuleStripe:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Stripe        StripeSettings   `toml:"stripe,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Stripe, settings.Metadata})
	case settingsModuleGitHub:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Github        GitHubSettings   `toml:"github,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Github, settings.Metadata})
	case settingsModuleCloudflare:
		return toml.Marshal(struct {
			SchemaVersion int                `toml:"schema_version"`
			Cloudflare    CloudflareSettings `toml:"cloudflare,omitempty"`
			Metadata      SettingsMetadata   `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Cloudflare, settings.Metadata})
	case settingsModuleGoogle:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Google        GoogleSettings   `toml:"google,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Google, settings.Metadata})
	case settingsModuleApple:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Apple         AppleSettings    `toml:"apple,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Apple, settings.Metadata})
	case settingsModuleSocial:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Social        SocialSettings   `toml:"social,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Social, settings.Metadata})
	case settingsModuleWorkOS:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			WorkOS        WorkOSSettings   `toml:"workos,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.WorkOS, settings.Metadata})
	case settingsModuleAWS:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			AWS           AWSSettings      `toml:"aws,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.AWS, settings.Metadata})
	case settingsModuleGCP:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			GCP           GCPSettings      `toml:"gcp,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.GCP, settings.Metadata})
	case settingsModuleOpenAI:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			OpenAI        OpenAISettings   `toml:"openai,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.OpenAI, settings.Metadata})
	case settingsModuleSurf:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Surf          SurfSettings     `toml:"surf,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Surf, settings.Metadata})
	case settingsModuleRemoteControl:
		return toml.Marshal(struct {
			SchemaVersion int                   `toml:"schema_version"`
			RemoteControl RemoteControlSettings `toml:"remote_control,omitempty"`
			Metadata      SettingsMetadata      `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.RemoteControl, settings.Metadata})
	case settingsModuleViva:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Viva          VivaSettings     `toml:"viva,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Viva, settings.Metadata})
	case settingsModuleSun:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			Sun           SunSettings      `toml:"sun,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.Sun, settings.Metadata})
	case settingsModuleOCI:
		return toml.Marshal(struct {
			SchemaVersion int              `toml:"schema_version"`
			OCI           OCISettings      `toml:"oci,omitempty"`
			Metadata      SettingsMetadata `toml:"metadata,omitempty"`
		}{settings.SchemaVersion, settings.OCI, settings.Metadata})
	default:
		return nil, fmt.Errorf("unsupported settings module: %s", module)
	}
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

func normalizeVivaTunnelProfile(profile VivaTunnelProfile) VivaTunnelProfile {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.ContainerName = strings.TrimSpace(profile.ContainerName)
	profile.TunnelIDEnvKey = strings.TrimSpace(profile.TunnelIDEnvKey)
	if profile.TunnelIDEnvKey == "" {
		profile.TunnelIDEnvKey = "VIVA_CLOUDFLARE_TUNNEL_ID"
	}
	profile.CredentialsEnvKey = strings.TrimSpace(profile.CredentialsEnvKey)
	if profile.CredentialsEnvKey == "" {
		profile.CredentialsEnvKey = "CLOUDFLARE_TUNNEL_CREDENTIALS_JSON"
	}
	profile.MetricsAddr = strings.TrimSpace(profile.MetricsAddr)
	profile.Image = strings.TrimSpace(profile.Image)
	if profile.Image == "" {
		profile.Image = "cloudflare/cloudflared:latest"
	}
	profile.NetworkMode = strings.TrimSpace(profile.NetworkMode)
	if profile.NetworkMode == "" {
		profile.NetworkMode = "host"
	}
	if len(profile.AdditionalNetworks) > 0 {
		seen := map[string]bool{}
		primary := strings.TrimSpace(profile.NetworkMode)
		if primary != "" {
			seen[primary] = true
		}
		networks := make([]string, 0, len(profile.AdditionalNetworks))
		for _, raw := range profile.AdditionalNetworks {
			nw := strings.TrimSpace(raw)
			if nw == "" {
				continue
			}
			if seen[nw] {
				continue
			}
			seen[nw] = true
			networks = append(networks, nw)
		}
		profile.AdditionalNetworks = networks
	}
	if profile.NoAutoupdate == nil {
		profile.NoAutoupdate = boolPtr(true)
	}
	if profile.PullImage == nil {
		profile.PullImage = boolPtr(true)
	}
	profile.RuntimeDir = strings.TrimSpace(profile.RuntimeDir)
	profile.VaultEnvFile = strings.TrimSpace(profile.VaultEnvFile)
	profile.VaultRepo = strings.TrimSpace(profile.VaultRepo)
	if profile.VaultRepo == "" {
		profile.VaultRepo = "viva"
	}
	profile.VaultEnv = strings.ToLower(strings.TrimSpace(profile.VaultEnv))
	if profile.VaultEnv == "" {
		profile.VaultEnv = "dev"
	}
	if len(profile.Routes) > 0 {
		routes := make([]VivaTunnelRoute, 0, len(profile.Routes))
		for _, r := range profile.Routes {
			route := VivaTunnelRoute{
				Hostname: strings.TrimSpace(r.Hostname),
				Service:  strings.TrimSpace(r.Service),
			}
			if route.Service == "" {
				continue
			}
			routes = append(routes, route)
		}
		profile.Routes = routes
	}
	return profile
}

func boolPtr(val bool) *bool {
	return &val
}
