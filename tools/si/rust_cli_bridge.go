package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	siExperimentalRustCLIEnv = "SI_EXPERIMENTAL_RUST_CLI"
	siRustCLIBinEnv          = "SI_RUST_CLI_BIN"
)

var (
	rustCLIExecCommand = exec.Command
	rustCLILookPath    = exec.LookPath
	rustCLIRepoRoot    = repoRoot
)

type rustCodexSpawnPlanRequest struct {
	Name          string
	ProfileID     string
	Workspace     string
	Workdir       string
	CodexVolume   string
	SkillsVolume  string
	GHVolume      string
	Repo          string
	GHPAT         string
	DockerSocket  bool
	Detach        bool
	CleanSlate    bool
	Image         string
	Network       string
	VaultEnvFile  string
	IncludeHostSI bool
}

type rustCodexSpawnPlan struct {
	Name                   string                    `json:"name"`
	ContainerName          string                    `json:"container_name"`
	Image                  string                    `json:"image"`
	NetworkName            string                    `json:"network_name"`
	WorkspaceHost          string                    `json:"workspace_host"`
	WorkspacePrimaryTarget string                    `json:"workspace_primary_target"`
	WorkspaceMirrorTarget  string                    `json:"workspace_mirror_target"`
	Workdir                string                    `json:"workdir"`
	CodexVolume            string                    `json:"codex_volume"`
	SkillsVolume           string                    `json:"skills_volume"`
	GHVolume               string                    `json:"gh_volume"`
	DockerSocket           bool                      `json:"docker_socket"`
	CleanSlate             bool                      `json:"clean_slate"`
	Detach                 bool                      `json:"detach"`
	Env                    []string                  `json:"env"`
	Mounts                 []rustCodexSpawnPlanMount `json:"mounts"`
}

type rustCodexSpawnPlanMount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type rustCodexSpawnSpecRequest struct {
	rustCodexSpawnPlanRequest
	Command string
	Env     []string
	Labels  []string
	Ports   []string
}

type rustCodexSpawnSpec struct {
	Image         string                     `json:"image"`
	Name          string                     `json:"name"`
	Network       string                     `json:"network"`
	RestartPolicy string                     `json:"restart_policy"`
	WorkingDir    string                     `json:"working_dir"`
	Command       []string                   `json:"command"`
	Env           []rustCodexSpawnSpecEnv    `json:"env"`
	BindMounts    []rustCodexSpawnPlanMount  `json:"bind_mounts"`
	VolumeMounts  []rustCodexSpawnSpecVolume `json:"volume_mounts"`
}

type rustCodexSpawnSpecEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type rustCodexSpawnSpecVolume struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type rustCodexRemoveArtifacts struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	Slug          string `json:"slug"`
	CodexVolume   string `json:"codex_volume"`
	GHVolume      string `json:"gh_volume"`
}

type rustCodexRemoveResult struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	ProfileID     string `json:"profile_id"`
	CodexVolume   string `json:"codex_volume"`
	GHVolume      string `json:"gh_volume"`
	Output        string `json:"output"`
}

type rustCodexContainerActionResult struct {
	Action        string `json:"action"`
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	Output        string `json:"output"`
}

type rustCodexCloneResult struct {
	Name          string `json:"name"`
	Repo          string `json:"repo"`
	ContainerName string `json:"container_name"`
	Output        string `json:"output"`
}

type rustCodexStatusRead struct {
	Source            string  `json:"source,omitempty"`
	Raw               string  `json:"raw,omitempty"`
	Model             string  `json:"model,omitempty"`
	ReasoningEffort   string  `json:"reasoning_effort,omitempty"`
	AccountEmail      string  `json:"account_email,omitempty"`
	AccountPlan       string  `json:"account_plan,omitempty"`
	FiveHourLeftPct   float64 `json:"five_hour_left_pct,omitempty"`
	FiveHourReset     string  `json:"five_hour_reset,omitempty"`
	FiveHourRemaining int     `json:"five_hour_remaining_minutes,omitempty"`
	WeeklyLeftPct     float64 `json:"weekly_left_pct,omitempty"`
	WeeklyReset       string  `json:"weekly_reset,omitempty"`
	WeeklyRemaining   int     `json:"weekly_remaining_minutes,omitempty"`
}

type rustCodexRespawnPlan struct {
	EffectiveName string   `json:"effective_name"`
	ProfileID     string   `json:"profile_id,omitempty"`
	RemoveTargets []string `json:"remove_targets"`
}

type rustCodexTmuxPlan struct {
	SessionName   string `json:"session_name"`
	Target        string `json:"target"`
	LaunchCommand string `json:"launch_command"`
	ResumeCommand string `json:"resume_command,omitempty"`
}

type rustCodexTmuxCommand struct {
	Container     string `json:"container"`
	LaunchCommand string `json:"launch_command"`
}

type rustCodexPromptSegment struct {
	Prompt string   `json:"prompt"`
	Lines  []string `json:"lines"`
	Raw    []string `json:"raw"`
}

type rustCodexReportParseResult struct {
	Segments []rustCodexPromptSegment `json:"segments"`
	Report   string                   `json:"report"`
}

type rustDyadSpawnPlanRequest struct {
	Name                    string
	Role                    string
	ActorImage              string
	CriticImage             string
	CodexModel              string
	CodexEffortActor        string
	CodexEffortCritic       string
	CodexModelLow           string
	CodexModelMedium        string
	CodexModelHigh          string
	CodexEffortLow          string
	CodexEffortMedium       string
	CodexEffortHigh         string
	Workspace               string
	Configs                 string
	VaultEnvFile            string
	CodexVolume             string
	SkillsVolume            string
	Network                 string
	ForwardPorts            string
	DockerSocket            bool
	ProfileID               string
	ProfileName             string
	LoopEnabled             *bool
	LoopGoal                string
	LoopSeedPrompt          string
	LoopMaxTurns            *int
	LoopSleepSeconds        *int
	LoopStartupDelaySeconds *int
	LoopTurnTimeoutSeconds  *int
	LoopRetryMax            *int
	LoopRetryBaseSeconds    *int
	LoopPromptLines         *int
	LoopAllowMCPStartup     *bool
	LoopTmuxCapture         string
	LoopPausePollSeconds    *int
}

type rustDyadSpawnPlan struct {
	Dyad          string             `json:"dyad"`
	Role          string             `json:"role"`
	NetworkName   string             `json:"network_name"`
	WorkspaceHost string             `json:"workspace_host"`
	ConfigsHost   string             `json:"configs_host"`
	CodexVolume   string             `json:"codex_volume"`
	SkillsVolume  string             `json:"skills_volume"`
	ForwardPorts  string             `json:"forward_ports"`
	DockerSocket  bool               `json:"docker_socket"`
	Actor         rustDyadMemberPlan `json:"actor"`
	Critic        rustDyadMemberPlan `json:"critic"`
}

type rustDyadMemberPlan struct {
	Member        string                    `json:"member"`
	ContainerName string                    `json:"container_name"`
	Image         string                    `json:"image"`
	Workdir       string                    `json:"workdir,omitempty"`
	Env           []string                  `json:"env"`
	BindMounts    []rustCodexSpawnPlanMount `json:"bind_mounts"`
	Command       []string                  `json:"command"`
}

type rustDyadListEntry struct {
	Dyad   string `json:"dyad"`
	Role   string `json:"role"`
	Actor  string `json:"actor"`
	Critic string `json:"critic"`
}

type rustDyadStatus struct {
	Dyad   string                      `json:"dyad"`
	Found  bool                        `json:"found"`
	Actor  *rustDyadContainerStatusRef `json:"actor,omitempty"`
	Critic *rustDyadContainerStatusRef `json:"critic,omitempty"`
}

type rustDyadContainerStatusRef struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

type rustDyadPeekPlan struct {
	Dyad                string `json:"dyad"`
	Member              string `json:"member"`
	ActorContainerName  string `json:"actor_container_name"`
	CriticContainerName string `json:"critic_container_name"`
	ActorSessionName    string `json:"actor_session_name"`
	CriticSessionName   string `json:"critic_session_name"`
	PeekSessionName     string `json:"peek_session_name"`
	ActorAttachCommand  string `json:"actor_attach_command"`
	CriticAttachCommand string `json:"critic_attach_command"`
}

type rustVaultTrustLookup struct {
	Found               bool   `json:"found"`
	Matches             bool   `json:"matches"`
	RepoRoot            string `json:"repo_root"`
	File                string `json:"file"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
	StoredFingerprint   string `json:"stored_fingerprint,omitempty"`
	TrustedAt           string `json:"trusted_at,omitempty"`
}

type rustWarmupMarkerState struct {
	Disabled         bool `json:"disabled"`
	AutostartPresent bool `json:"autostart_present"`
}

type rustWarmupAutostartDecision struct {
	Requested bool   `json:"requested"`
	Reason    string `json:"reason"`
}

type rustFortSessionClassification struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type rustFortRuntimeAgentState struct {
	ProfileID   string `json:"profile_id"`
	PID         int    `json:"pid"`
	CommandPath string `json:"command_path,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type rustFortSessionTransition struct {
	State          fortProfileSessionState       `json:"state"`
	Classification rustFortSessionClassification `json:"classification"`
}

func maybeLoadRustCodexFortBootstrap(sessionStatePath string, profileID string, accessTokenPath string, refreshTokenPath string, accessTokenContainerPath string, refreshTokenContainerPath string) (*codexFortBootstrap, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{
		"fort", "session-state", "bootstrap-view",
		"--path", strings.TrimSpace(sessionStatePath),
		"--access-token-path", strings.TrimSpace(accessTokenPath),
		"--refresh-token-path", strings.TrimSpace(refreshTokenPath),
		"--access-token-container-path", strings.TrimSpace(accessTokenContainerPath),
		"--refresh-token-container-path", strings.TrimSpace(refreshTokenContainerPath),
		"--format", "json",
	}
	if value := strings.TrimSpace(profileID); value != "" {
		args = append(args, "--profile-id", value)
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var boot codexFortBootstrap
	if err := json.Unmarshal(output, &boot); err != nil {
		return nil, false, fmt.Errorf("decode rust fort bootstrap view: %w", err)
	}
	return &boot, true, nil
}

func runVersionCommand() error {
	delegated, err := maybeDispatchRustCLIReadOnly("version")
	if err != nil {
		return err
	}
	if delegated {
		return nil
	}
	printVersion()
	return nil
}

func runHelpCommand(args []string) error {
	if len(args) <= 1 {
		delegated, err := maybeDispatchRustCLIReadOnly("help", args...)
		if err != nil {
			return err
		}
		if delegated {
			return nil
		}
	}
	usage()
	return nil
}

func runProvidersCharacteristicsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("providers", append([]string{"characteristics"}, args...)...)
}

func runCloudflareContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("cloudflare", append([]string{"context", "list"}, args...)...)
}

func runCloudflareContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("cloudflare", append([]string{"context", "current"}, args...)...)
}

func runCloudflareContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runCloudflareContextListCommand(args[1:])
	case "current":
		return runCloudflareContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runCloudflareAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("cloudflare", append([]string{"auth", "status"}, args...)...)
}

func runCloudflareAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return runCloudflareAuthStatusCommand(args[1:])
	default:
		return false, nil
	}
}

func runCloudflareCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runCloudflareAuthCommand(args[1:])
	case "context":
		return runCloudflareContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runAppleCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "appstore":
		return runAppleAppStoreCommand(args[1:])
	default:
		return false, nil
	}
}

func runAppleAppStoreContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("apple", append([]string{"appstore", "context", "list"}, args...)...)
}

func runAppleAppStoreCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runAppleAppStoreAuthCommand(args[1:])
	case "context":
		return runAppleAppStoreContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runAppleAppStoreContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("apple", append([]string{"appstore", "context", "current"}, args...)...)
}

func runAppleAppStoreContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runAppleAppStoreContextListCommand(args[1:])
	case "current":
		return runAppleAppStoreContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runAppleAppStoreAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		return runAppleAppStoreAuthStatusCommand(args[1:])
	}
	return false, nil
}

func runAppleAppStoreAuthStatusCommand(args []string) (bool, error) {
	if appleAppStoreAuthStatusVerifyEnabled(args) {
		return false, nil
	}
	return maybeDispatchRustCLIReadOnly("apple", append([]string{"appstore", "auth", "status"}, args...)...)
}

func appleAppStoreAuthStatusVerifyEnabled(args []string) bool {
	verify := true
	for idx := 0; idx < len(args); idx++ {
		arg := strings.TrimSpace(args[idx])
		switch {
		case arg == "--verify":
			if idx+1 < len(args) {
				next := strings.TrimSpace(args[idx+1])
				if next == "false" || next == "0" {
					verify = false
					idx++
				}
			}
		case arg == "--verify=false", arg == "--verify=0":
			verify = false
		case arg == "--verify=true", arg == "--verify=1":
			verify = true
		case strings.HasPrefix(arg, "--verify="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--verify="))
			verify = value != "false" && value != "0"
		}
	}
	return verify
}

func runAWSContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("aws", append([]string{"context", "list"}, args...)...)
}

func runAWSCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runAWSAuthCommand(args[1:])
	case "context":
		return runAWSContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runAWSContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("aws", append([]string{"context", "current"}, args...)...)
}

func runAWSContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runAWSContextListCommand(args[1:])
	case "current":
		return runAWSContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runAWSAuthCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("aws", append([]string{"auth"}, args...)...)
}

func runAWSAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("aws", append([]string{"auth", "status"}, args...)...)
}

func runGCPAuthCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("gcp", append([]string{"auth"}, args...)...)
}

func runGCPCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runGCPAuthCommand(args[1:])
	case "context":
		return runGCPContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runGCPContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("gcp", append([]string{"context", "list"}, args...)...)
}

func runGCPContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("gcp", append([]string{"context", "current"}, args...)...)
}

func runGCPContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGCPContextListCommand(args[1:])
	case "current":
		return runGCPContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runGCPAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("gcp", append([]string{"auth", "status"}, args...)...)
}

func runGooglePlacesContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("google", append([]string{"places", "context", "list"}, args...)...)
}

func runGoogleCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "places":
		return runGooglePlacesCommand(args[1:])
	default:
		return false, nil
	}
}

func runGooglePlacesCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runGooglePlacesAuthCommand(args[1:])
	case "context":
		return runGooglePlacesContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runGooglePlacesContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("google", append([]string{"places", "context", "current"}, args...)...)
}

func runGooglePlacesContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGooglePlacesContextListCommand(args[1:])
	case "current":
		return runGooglePlacesContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runGooglePlacesAuthCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("google", append([]string{"places", "auth"}, args...)...)
}

func runGooglePlacesAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("google", append([]string{"places", "auth", "status"}, args...)...)
}

func runOpenAICommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runOpenAIAuthCommand(args[1:])
	case "context":
		return runOpenAIContextCommand(args[1:])
	case "model", "models":
		return runOpenAIModelCommand(args[1:])
	case "project", "projects":
		return runOpenAIProjectCommand(args[1:])
	case "key", "keys", "admin-key", "admin-keys":
		return runOpenAIKeyCommand(args[1:])
	case "usage":
		return runOpenAIUsageCommand(args[1:])
	case "monitor", "monitoring":
		return runOpenAIMonitorCommand(args[1:])
	case "codex":
		return runOpenAICodexCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return runOpenAIAuthStatusCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"context", "list"}, args...)...)
}

func runOpenAIContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"context", "current"}, args...)...)
}

func runOpenAIContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIContextListCommand(args[1:])
	case "current":
		return runOpenAIContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"auth", "status"}, args...)...)
}

func runOpenAIModelCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"model"}, args...)...)
}

func runOpenAIModelListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"model", "list"}, args...)...)
}

func runOpenAIModelGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"model", "get"}, args...)...)
}

func runOpenAIUsageCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"usage"}, args...)...)
}

func runOpenAIUsageMetricCommand(metric string, args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"usage", metric}, args...)...)
}

func runOpenAIMonitorCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"monitor"}, args...)...)
}

func runOpenAICodexCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"codex"}, args...)...)
}

func runOpenAICodexUsageCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"codex", "usage"}, args...)...)
}

func runOpenAIKeyListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"key", "list"}, args...)...)
}

func runOpenAIKeyGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"key", "get"}, args...)...)
}

func runOpenAIKeyCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIKeyListCommand(args[1:])
	case "get", "retrieve":
		return runOpenAIKeyGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIProjectListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "list"}, args...)...)
}

func runOpenAIProjectGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "get"}, args...)...)
}

func runOpenAIProjectAPIKeyListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "api-key", "list"}, args...)...)
}

func runOpenAIProjectAPIKeyGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "api-key", "get"}, args...)...)
}

func runOpenAIProjectServiceAccountListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "service-account", "list"}, args...)...)
}

func runOpenAIProjectServiceAccountGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "service-account", "get"}, args...)...)
}

func runOpenAIProjectRateLimitListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("openai", append([]string{"project", "rate-limit", "list"}, args...)...)
}

func runOpenAIProjectRateLimitCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIProjectRateLimitListCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIProjectAPIKeyCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIProjectAPIKeyListCommand(args[1:])
	case "get", "retrieve":
		return runOpenAIProjectAPIKeyGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIProjectServiceAccountCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIProjectServiceAccountListCommand(args[1:])
	case "get", "retrieve":
		return runOpenAIProjectServiceAccountGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runOpenAIProjectCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOpenAIProjectListCommand(args[1:])
	case "get", "retrieve":
		return runOpenAIProjectGetCommand(args[1:])
	case "rate-limit", "rate-limits":
		return runOpenAIProjectRateLimitCommand(args[1:])
	case "api-key", "api-keys":
		return runOpenAIProjectAPIKeyCommand(args[1:])
	case "service-account", "service-accounts":
		return runOpenAIProjectServiceAccountCommand(args[1:])
	default:
		return false, nil
	}
}

func runOCIContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("oci", append([]string{"context", "list"}, args...)...)
}

func runOCIContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("oci", append([]string{"context", "current"}, args...)...)
}

func runOCIContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runOCIContextListCommand(args[1:])
	case "current":
		return runOCIContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runOCIAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		return runOCIAuthStatusCommand(args[1:])
	}
	return false, nil
}

func runOCIAuthStatusCommand(args []string) (bool, error) {
	if ociAuthStatusVerifyEnabled(args) {
		return false, nil
	}
	return maybeDispatchRustCLIReadOnly("oci", append([]string{"auth", "status"}, args...)...)
}

func runOCIOracularCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "tenancy":
		return runOCIOracularTenancyCommand(args[1:])
	default:
		return false, nil
	}
}

func runOCICommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runOCIAuthCommand(args[1:])
	case "context":
		return runOCIContextCommand(args[1:])
	case "oracular":
		return runOCIOracularCommand(args[1:])
	default:
		return false, nil
	}
}

func runOCIOracularTenancyCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("oci", append([]string{"oracular", "tenancy"}, args...)...)
}

func ociAuthStatusVerifyEnabled(args []string) bool {
	verify := true
	for idx := 0; idx < len(args); idx++ {
		arg := strings.TrimSpace(args[idx])
		switch {
		case arg == "--verify":
			if idx+1 < len(args) {
				next := strings.TrimSpace(args[idx+1])
				if next == "false" || next == "0" {
					verify = false
					idx++
				}
			}
		case arg == "--verify=false", arg == "--verify=0":
			verify = false
		case arg == "--verify=true", arg == "--verify=1":
			verify = true
		case strings.HasPrefix(arg, "--verify="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--verify="))
			verify = value != "false" && value != "0"
		}
	}
	return verify
}

func runStripeContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("stripe", append([]string{"context", "list"}, args...)...)
}

func runStripeContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("stripe", append([]string{"context", "current"}, args...)...)
}

func runStripeContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runStripeContextListCommand(args[1:])
	case "current":
		return runStripeContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runStripeAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("stripe", append([]string{"auth", "status"}, args...)...)
}

func runStripeAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return runStripeAuthStatusCommand(args[1:])
	default:
		return false, nil
	}
}

func runStripeCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runStripeAuthCommand(args[1:])
	case "context":
		return runStripeContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runWorkOSContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("workos", append([]string{"context", "list"}, args...)...)
}

func runWorkOSContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("workos", append([]string{"context", "current"}, args...)...)
}

func runWorkOSContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runWorkOSContextListCommand(args[1:])
	case "current":
		return runWorkOSContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runWorkOSAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("workos", append([]string{"auth", "status"}, args...)...)
}

func runWorkOSAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return runWorkOSAuthStatusCommand(args[1:])
	default:
		return false, nil
	}
}

func runWorkOSCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runWorkOSAuthCommand(args[1:])
	case "context":
		return runWorkOSContextCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubContextListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"context", "list"}, args...)...)
}

func runGitHubContextCurrentCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"context", "current"}, args...)...)
}

func runGitHubContextCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubContextListCommand(args[1:])
	case "current":
		return runGitHubContextCurrentCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubBranchListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"branch", "list"}, args...)...)
}

func runGitHubBranchGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"branch", "get"}, args...)...)
}

func runGitHubBranchCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubBranchListCommand(args[1:])
	case "get":
		return runGitHubBranchGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubAuthStatusCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"auth", "status"}, args...)...)
}

func runGitHubAuthCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return runGitHubAuthStatusCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubReleaseListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"release", "list"}, args...)...)
}

func runGitHubReleaseGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"release", "get"}, args...)...)
}

func runGitHubReleaseCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubReleaseListCommand(args[1:])
	case "get":
		return runGitHubReleaseGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubRepoListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"repo", "list"}, args...)...)
}

func runGitHubRepoGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"repo", "get"}, args...)...)
}

func runGitHubRepoCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubRepoListCommand(args[1:])
	case "get":
		return runGitHubRepoGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubProjectListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"project", "list"}, args...)...)
}

func runGitHubProjectGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"project", "get"}, args...)...)
}

func runGitHubProjectFieldsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"project", "fields"}, args...)...)
}

func runGitHubProjectItemsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"project", "items"}, args...)...)
}

func runGitHubProjectUpdateCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "update"}, args...)...)
}

func runGitHubProjectItemAddCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-add"}, args...)...)
}

func runGitHubProjectItemSetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-set"}, args...)...)
}

func runGitHubProjectItemClearCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-clear"}, args...)...)
}

func runGitHubProjectItemArchiveCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-archive"}, args...)...)
}

func runGitHubProjectItemUnarchiveCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-unarchive"}, args...)...)
}

func runGitHubProjectItemDeleteCommand(args []string) (bool, error) {
	return maybeDispatchRustCLICompat("github", append([]string{"project", "item-delete"}, args...)...)
}

func runGitHubProjectCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubProjectListCommand(args[1:])
	case "get":
		return runGitHubProjectGetCommand(args[1:])
	case "fields":
		return runGitHubProjectFieldsCommand(args[1:])
	case "items":
		return runGitHubProjectItemsCommand(args[1:])
	case "update":
		return runGitHubProjectUpdateCommand(args[1:])
	case "item-add":
		return runGitHubProjectItemAddCommand(args[1:])
	case "item-set":
		return runGitHubProjectItemSetCommand(args[1:])
	case "item-clear":
		return runGitHubProjectItemClearCommand(args[1:])
	case "item-archive":
		return runGitHubProjectItemArchiveCommand(args[1:])
	case "item-unarchive":
		return runGitHubProjectItemUnarchiveCommand(args[1:])
	case "item-delete":
		return runGitHubProjectItemDeleteCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubWorkflowListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"workflow", "list"}, args...)...)
}

func runGitHubWorkflowRunsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"workflow", "runs"}, args...)...)
}

func runGitHubWorkflowRunGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"workflow", "run", "get"}, args...)...)
}

func runGitHubWorkflowLogsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"workflow", "logs"}, args...)...)
}

func runGitHubWorkflowRunCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get":
		return runGitHubWorkflowRunGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubWorkflowCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubWorkflowListCommand(args[1:])
	case "runs":
		return runGitHubWorkflowRunsCommand(args[1:])
	case "run":
		return runGitHubWorkflowRunCommand(args[1:])
	case "logs":
		return runGitHubWorkflowLogsCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubGitCredentialGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"git", "credential", "get"}, args...)...)
}

func runGitHubGitCredentialCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return runGitHubGitCredentialGetCommand(args)
	}
	first := strings.ToLower(strings.TrimSpace(args[0]))
	if strings.HasPrefix(first, "-") {
		return runGitHubGitCredentialGetCommand(args)
	}
	switch first {
	case "get":
		return runGitHubGitCredentialGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubGitCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "credential":
		return runGitHubGitCredentialCommand(args[1:])
	default:
		return false, nil
	}
}

func rustCLIFlagValue(args []string, name string) (string, bool) {
	for idx := 0; idx < len(args); idx++ {
		raw := strings.TrimSpace(args[idx])
		if raw == name {
			if idx+1 >= len(args) {
				return "", false
			}
			return strings.TrimSpace(args[idx+1]), true
		}
		prefix := name + "="
		if strings.HasPrefix(raw, prefix) {
			return strings.TrimSpace(raw[len(prefix):]), true
		}
	}
	return "", false
}

func runGitHubRawCommand(args []string) (bool, error) {
	method := "GET"
	if value, ok := rustCLIFlagValue(args, "--method"); ok && strings.TrimSpace(value) != "" {
		method = value
	}
	if !strings.EqualFold(strings.TrimSpace(method), "GET") {
		return false, nil
	}
	return maybeDispatchRustCLIReadOnly("github", append([]string{"raw"}, args...)...)
}

func runGitHubGraphQLCommand(args []string) (bool, error) {
	query, ok := rustCLIFlagValue(args, "--query")
	if !ok || strings.TrimSpace(query) == "" {
		return false, nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(query)), "mutation") {
		return false, nil
	}
	return maybeDispatchRustCLIReadOnly("github", append([]string{"graphql"}, args...)...)
}

func runGitHubIssueListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"issue", "list"}, args...)...)
}

func runGitHubIssueGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"issue", "get"}, args...)...)
}

func runGitHubIssueCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubIssueListCommand(args[1:])
	case "get":
		return runGitHubIssueGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubPRListCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"pr", "list"}, args...)...)
}

func runGitHubPRGetCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("github", append([]string{"pr", "get"}, args...)...)
}

func runGitHubPRCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return runGitHubPRListCommand(args[1:])
	case "get":
		return runGitHubPRGetCommand(args[1:])
	default:
		return false, nil
	}
}

func runGitHubCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "auth":
		return runGitHubAuthCommand(args[1:])
	case "context":
		return runGitHubContextCommand(args[1:])
	case "branch":
		return runGitHubBranchCommand(args[1:])
	case "git":
		return runGitHubGitCommand(args[1:])
	case "raw":
		return runGitHubRawCommand(args[1:])
	case "graphql":
		return runGitHubGraphQLCommand(args[1:])
	case "issue":
		return runGitHubIssueCommand(args[1:])
	case "pr":
		return runGitHubPRCommand(args[1:])
	case "project":
		return runGitHubProjectCommand(args[1:])
	case "workflow":
		return runGitHubWorkflowCommand(args[1:])
	case "repo":
		return runGitHubRepoCommand(args[1:])
	case "release":
		return runGitHubReleaseCommand(args[1:])
	default:
		return false, nil
	}
}

func maybeBuildRustCodexSpawnPlan(request rustCodexSpawnPlanRequest) (*rustCodexSpawnPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(buildRustCodexSpawnPlanArgs(request)...)
	if err != nil {
		return nil, false, err
	}
	var plan rustCodexSpawnPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust codex spawn plan: %w", err)
	}
	return &plan, true, nil
}

func maybeBuildRustCodexSpawnSpec(request rustCodexSpawnSpecRequest) (*rustCodexSpawnSpec, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(buildRustCodexSpawnSpecArgs(request)...)
	if err != nil {
		return nil, false, err
	}
	var spec rustCodexSpawnSpec
	if err := json.Unmarshal(output, &spec); err != nil {
		return nil, false, fmt.Errorf("decode rust codex spawn spec: %w", err)
	}
	return &spec, true, nil
}

func maybeBuildRustDyadSpawnPlan(request rustDyadSpawnPlanRequest) (*rustDyadSpawnPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(buildRustDyadSpawnPlanArgs(request)...)
	if err != nil {
		return nil, false, err
	}
	var plan rustDyadSpawnPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust dyad spawn plan: %w", err)
	}
	return &plan, true, nil
}

func maybeStartRustDyadSpawn(request rustDyadSpawnPlanRequest) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	if _, err := runRustCLIText(buildRustDyadSpawnStartArgs(request)...); err != nil {
		return false, err
	}
	return true, nil
}

func maybeRunRustDyadContainerAction(action string, dyad string) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", false, fmt.Errorf("rust dyad container action is required")
	}
	output, err := runRustCLIText("dyad", action, strings.TrimSpace(dyad))
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustDyadRemove(dyad string) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	output, err := runRustCLIText("dyad", "remove", strings.TrimSpace(dyad))
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustDyadExec(dyad string, member string, tty bool, cmd []string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return false, err
	}
	args := []string{
		"dyad", "exec", strings.TrimSpace(dyad),
		"--member", strings.TrimSpace(member),
		"--tty=" + strconv.FormatBool(tty),
		"--",
	}
	for _, item := range cmd {
		item = strings.TrimSpace(item)
		if item != "" {
			args = append(args, item)
		}
	}
	command := rustCLIExecCommand(bin, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("run rust si cli %q: %w", strings.Join(args, " "), err)
	}
	return true, nil
}

func maybeRunRustDyadCleanup() (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	output, err := runRustCLIText("dyad", "cleanup")
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustDyadLogs(dyad string, member string, tail int, jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	output, err := runRustCLIText(
		"dyad", "logs", strings.TrimSpace(dyad),
		"--member", strings.TrimSpace(member),
		"--tail", strconv.Itoa(tail),
		"--format", format,
	)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustDyadList(jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	output, err := runRustCLIText("dyad", "list", "--format", format)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustDyadStatus(dyad string, jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	output, err := runRustCLIText("dyad", "status", strings.TrimSpace(dyad), "--format", format)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustWarmupStatus(jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	output, err := runRustCLIText("warmup", "status", "--format", format)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeReadRustDyadStatus(dyad string) (*rustDyadStatus, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON("dyad", "status", strings.TrimSpace(dyad), "--format", "json")
	if err != nil {
		return nil, false, err
	}
	var status rustDyadStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, false, fmt.Errorf("decode rust dyad status: %w", err)
	}
	return &status, true, nil
}

func maybeReadRustDyadPeekPlan(dyad string, member string, session string) (*rustDyadPeekPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{
		"dyad", "peek-plan", strings.TrimSpace(dyad),
		"--member", strings.TrimSpace(member),
		"--format", "json",
	}
	if value := strings.TrimSpace(session); value != "" {
		args = append(args, "--session", value)
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var plan rustDyadPeekPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust dyad peek plan: %w", err)
	}
	return &plan, true, nil
}

func maybeStartRustCodexSpawn(request rustCodexSpawnSpecRequest) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	output, err := runRustCLIText(buildRustCodexSpawnStartArgs(request)...)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(output), true, nil
}

func maybeBuildRustCodexRemoveArtifacts(name string) (*rustCodexRemoveArtifacts, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON("codex", "remove-plan", strings.TrimSpace(name), "--format", "json")
	if err != nil {
		return nil, false, err
	}
	var artifacts rustCodexRemoveArtifacts
	if err := json.Unmarshal(output, &artifacts); err != nil {
		return nil, false, fmt.Errorf("decode rust codex remove plan: %w", err)
	}
	return &artifacts, true, nil
}

func maybeRunRustCodexRemove(name string, removeVolumes bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	args := []string{"codex", "remove", strings.TrimSpace(name)}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	output, err := runRustCLIText(args...)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(output), true, nil
}

func maybeRunRustCodexContainerActionResult(action string, name string) (*rustCodexContainerActionResult, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON("codex", strings.TrimSpace(action), strings.TrimSpace(name), "--format", "json")
	if err != nil {
		return nil, false, err
	}
	var result rustCodexContainerActionResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, false, fmt.Errorf("decode rust codex %s result: %w", strings.TrimSpace(action), err)
	}
	return &result, true, nil
}

func maybeRunRustCodexRemoveResult(name string, removeVolumes bool) (*rustCodexRemoveResult, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{"codex", "remove", strings.TrimSpace(name), "--format", "json"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var result rustCodexRemoveResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, false, fmt.Errorf("decode rust codex remove result: %w", err)
	}
	return &result, true, nil
}

func maybeRunRustCodexLogs(name string, tail string, follow bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	subcommand := "logs"
	if follow {
		subcommand = "tail"
	}
	output, err := runRustCLIText("codex", subcommand, strings.TrimSpace(name), "--tail", strings.TrimSpace(tail))
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustCodexCloneResult(name string, repo string, ghPAT string) (*rustCodexCloneResult, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{"codex", "clone", strings.TrimSpace(name), strings.TrimSpace(repo), "--format", "json"}
	if value := strings.TrimSpace(ghPAT); value != "" {
		args = append(args, "--gh-pat", value)
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var result rustCodexCloneResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, false, fmt.Errorf("decode rust codex clone result: %w", err)
	}
	return &result, true, nil
}

func maybeRunRustCodexExec(name string, workdir string, interactive bool, tty bool, env []string, cmd []string) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	args := []string{
		"codex",
		"exec",
		strings.TrimSpace(name),
		"--interactive=" + strconv.FormatBool(interactive),
		"--tty=" + strconv.FormatBool(tty),
	}
	if value := strings.TrimSpace(workdir); value != "" {
		args = append(args, "--workdir", value)
	}
	for _, value := range env {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, "--env", value)
		}
	}
	args = append(args, "--")
	for _, value := range cmd {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, value)
		}
	}
	output, err := runRustCLIText(args...)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeRunRustCodexList(jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	output, err := runRustCLIText("codex", "list", "--format", format)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeReadRustCodexStatus(name string, raw bool) (*rustCodexStatusRead, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{"codex", "status-read", strings.TrimSpace(name), "--format", "json"}
	if raw {
		args = append(args, "--raw")
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var status rustCodexStatusRead
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, false, fmt.Errorf("decode rust codex status: %w", err)
	}
	return &status, true, nil
}

func maybeRunRustCodexStatus(name string, raw bool, jsonOut bool) (string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", false, nil
	}
	format := "text"
	if jsonOut {
		format = "json"
	}
	args := []string{"codex", "status-read", strings.TrimSpace(name), "--format", format}
	if raw {
		args = append(args, "--raw")
	}
	output, err := runRustCLIText(args...)
	if err != nil {
		return "", false, err
	}
	return output, true, nil
}

func maybeBuildRustCodexRespawnPlan(name string, profileID string, profileContainers []string) (*rustCodexRespawnPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{"codex", "respawn-plan", strings.TrimSpace(name), "--format", "json"}
	if value := strings.TrimSpace(profileID); value != "" {
		args = append(args, "--profile-id", value)
	}
	for _, item := range profileContainers {
		item = strings.TrimSpace(item)
		if item != "" {
			args = append(args, "--profile-container", item)
		}
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var plan rustCodexRespawnPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust codex respawn plan: %w", err)
	}
	return &plan, true, nil
}

func maybeReadRustCodexTmuxPlan(name string, startDir string, resumeSessionID string, resumeProfile string) (*rustCodexTmuxPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{
		"codex", "tmux-plan", strings.TrimSpace(name),
		"--format", "json",
	}
	if value := strings.TrimSpace(startDir); value != "" {
		args = append(args, "--start-dir", value)
	}
	if value := strings.TrimSpace(resumeSessionID); value != "" {
		args = append(args, "--resume-session-id", value)
	}
	if value := strings.TrimSpace(resumeProfile); value != "" {
		args = append(args, "--resume-profile", value)
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var plan rustCodexTmuxPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust codex tmux plan: %w", err)
	}
	return &plan, true, nil
}

func maybeReadRustCodexTmuxCommand(container string) (*rustCodexTmuxCommand, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"codex", "tmux-command",
		"--container", strings.TrimSpace(container),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	var command rustCodexTmuxCommand
	if err := json.Unmarshal(output, &command); err != nil {
		return nil, false, fmt.Errorf("decode rust codex tmux command: %w", err)
	}
	return &command, true, nil
}

func maybeParseRustCodexReportCapture(clean string, raw string, promptIndex int, ansi bool) (*rustCodexReportParseResult, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	payload, err := json.Marshal(map[string]any{
		"clean":        clean,
		"raw":          raw,
		"prompt_index": promptIndex,
		"ansi":         ansi,
	})
	if err != nil {
		return nil, false, fmt.Errorf("encode rust codex report parse input: %w", err)
	}
	output, err := runRustCLIJSONWithStdin(payload, "codex", "report-parse", "--format", "json")
	if err != nil {
		return nil, false, err
	}
	var result rustCodexReportParseResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, false, fmt.Errorf("decode rust codex report parse: %w", err)
	}
	return &result, true, nil
}

func maybeRunRustVaultTrustLookup(storePath string, repoRoot string, file string, fingerprint string) (*rustVaultTrustLookup, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	args := []string{
		"vault", "trust", "lookup",
		"--path", strings.TrimSpace(storePath),
		"--repo-root", strings.TrimSpace(repoRoot),
		"--file", strings.TrimSpace(file),
		"--fingerprint", strings.TrimSpace(fingerprint),
		"--format", "json",
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var lookup rustVaultTrustLookup
	if err := json.Unmarshal(output, &lookup); err != nil {
		return nil, false, fmt.Errorf("decode rust vault trust lookup: %w", err)
	}
	return &lookup, true, nil
}

func maybeLoadRustWarmupState(path string) (warmWeeklyState, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return warmWeeklyState{}, false, nil
	}
	output, err := runRustCLIJSON(
		"warmup", "status",
		"--path", strings.TrimSpace(path),
		"--format", "json",
	)
	if err != nil {
		return warmWeeklyState{}, false, err
	}
	var state warmWeeklyState
	if err := json.Unmarshal(output, &state); err != nil {
		return warmWeeklyState{}, false, fmt.Errorf("decode rust warmup state: %w", err)
	}
	return state, true, nil
}

func maybeSaveRustWarmupState(path string, state warmWeeklyState) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("encode rust warmup state: %w", err)
	}
	if _, err := runRustCLIText(
		"warmup", "state", "write",
		"--path", strings.TrimSpace(path),
		"--state-json", string(raw),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeReadRustWarmupMarkerState(autostartPath string, disabledPath string) (*rustWarmupMarkerState, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"warmup", "marker", "show",
		"--autostart-path", strings.TrimSpace(autostartPath),
		"--disabled-path", strings.TrimSpace(disabledPath),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	var state rustWarmupMarkerState
	if err := json.Unmarshal(output, &state); err != nil {
		return nil, false, fmt.Errorf("decode rust warmup marker state: %w", err)
	}
	return &state, true, nil
}

func maybeWriteRustWarmupAutostartMarker(path string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	if _, err := runRustCLIText(
		"warmup", "marker", "write-autostart",
		"--path", strings.TrimSpace(path),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeSetRustWarmupDisabled(path string, disabled bool) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	if _, err := runRustCLIText(
		"warmup", "marker", "set-disabled",
		"--path", strings.TrimSpace(path),
		"--disabled="+strconv.FormatBool(disabled),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeReadRustWarmupAutostartDecision(statePath string, autostartPath string, disabledPath string) (*rustWarmupAutostartDecision, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"warmup", "autostart-decision",
		"--state-path", strings.TrimSpace(statePath),
		"--autostart-path", strings.TrimSpace(autostartPath),
		"--disabled-path", strings.TrimSpace(disabledPath),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	var decision rustWarmupAutostartDecision
	if err := json.Unmarshal(output, &decision); err != nil {
		return nil, false, fmt.Errorf("decode rust warmup autostart decision: %w", err)
	}
	return &decision, true, nil
}

func maybeLoadRustFortSessionState(path string) (fortProfileSessionState, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return fortProfileSessionState{}, false, nil
	}
	output, err := runRustCLIJSON("fort", "session-state", "show", "--path", strings.TrimSpace(path), "--format", "json")
	if err != nil {
		return fortProfileSessionState{}, false, err
	}
	var state fortProfileSessionState
	if err := json.Unmarshal(output, &state); err != nil {
		return fortProfileSessionState{}, false, fmt.Errorf("decode rust fort session state: %w", err)
	}
	return state, true, nil
}

func maybeClassifyRustFortSessionState(path string, nowUnix int64) (*rustFortSessionClassification, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"fort", "session-state", "classify",
		"--path", strings.TrimSpace(path),
		"--now-unix", strconv.FormatInt(nowUnix, 10),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	classification, err := decodeRustFortSessionClassification(output)
	if err != nil {
		return nil, false, err
	}
	return classification, true, nil
}

func maybeLoadRustFortRuntimeAgentState(path string) (fortProfileRuntimeAgentState, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return fortProfileRuntimeAgentState{}, false, nil
	}
	output, err := runRustCLIJSON("fort", "runtime-agent-state", "show", "--path", strings.TrimSpace(path), "--format", "json")
	if err != nil {
		return fortProfileRuntimeAgentState{}, false, err
	}
	var state rustFortRuntimeAgentState
	if err := json.Unmarshal(output, &state); err != nil {
		return fortProfileRuntimeAgentState{}, false, fmt.Errorf("decode rust fort runtime agent state: %w", err)
	}
	return fortProfileRuntimeAgentState{
		ProfileID:   strings.TrimSpace(state.ProfileID),
		PID:         state.PID,
		CommandPath: strings.TrimSpace(state.CommandPath),
		StartedAt:   strings.TrimSpace(state.StartedAt),
		UpdatedAt:   strings.TrimSpace(state.UpdatedAt),
	}, true, nil
}

func maybeSaveRustFortSessionState(path string, state fortProfileSessionState) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("encode rust fort session state: %w", err)
	}
	if _, err := runRustCLIText(
		"fort", "session-state", "write",
		"--path", strings.TrimSpace(path),
		"--state-json", string(raw),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeSaveRustFortRuntimeAgentState(path string, state fortProfileRuntimeAgentState) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("encode rust fort runtime agent state: %w", err)
	}
	if _, err := runRustCLIText(
		"fort", "runtime-agent-state", "write",
		"--path", strings.TrimSpace(path),
		"--state-json", string(raw),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeClearRustFortSessionState(path string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	if _, err := runRustCLIText(
		"fort", "session-state", "clear",
		"--path", strings.TrimSpace(path),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeClearRustFortRuntimeAgentState(path string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	if _, err := runRustCLIText(
		"fort", "runtime-agent-state", "clear",
		"--path", strings.TrimSpace(path),
	); err != nil {
		return false, err
	}
	return true, nil
}

func maybeApplyRustFortSessionRefreshOutcome(path string, refreshed fortSessionRefreshResult, now time.Time) (*rustFortSessionTransition, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	accessExpiry, err := time.Parse(time.RFC3339, strings.TrimSpace(refreshed.AccessExpiresAt))
	if err != nil {
		return nil, false, fmt.Errorf("parse rust fort access expiry: %w", err)
	}
	args := []string{
		"fort", "session-state", "refresh-outcome",
		"--path", strings.TrimSpace(path),
		"--outcome", "success",
		"--now-unix", strconv.FormatInt(now.UTC().Unix(), 10),
		"--access-expires-at-unix", strconv.FormatInt(accessExpiry.UTC().Unix(), 10),
		"--format", "json",
	}
	output, err := runRustCLIJSON(args...)
	if err != nil {
		return nil, false, err
	}
	var transition rustFortSessionTransition
	if err := json.Unmarshal(output, &transition); err != nil {
		return nil, false, fmt.Errorf("decode rust fort refresh outcome: %w", err)
	}
	return &transition, true, nil
}

func maybeApplyRustFortUnauthorizedRefreshOutcome(path string, now time.Time) (*rustFortSessionTransition, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"fort", "session-state", "refresh-outcome",
		"--path", strings.TrimSpace(path),
		"--outcome", "unauthorized",
		"--now-unix", strconv.FormatInt(now.UTC().Unix(), 10),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	var transition rustFortSessionTransition
	if err := json.Unmarshal(output, &transition); err != nil {
		return nil, false, fmt.Errorf("decode rust fort unauthorized refresh outcome: %w", err)
	}
	return &transition, true, nil
}

func maybeRunRustFortSessionTeardown(path string, now time.Time) (*rustFortSessionClassification, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(
		"fort", "session-state", "teardown",
		"--path", strings.TrimSpace(path),
		"--now-unix", strconv.FormatInt(now.UTC().Unix(), 10),
		"--format", "json",
	)
	if err != nil {
		return nil, false, err
	}
	var classification rustFortSessionClassification
	if err := json.Unmarshal(output, &classification); err != nil {
		return nil, false, fmt.Errorf("decode rust fort teardown classification: %w", err)
	}
	return &classification, true, nil
}

func decodeRustFortSessionClassification(raw []byte) (*rustFortSessionClassification, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode rust fort session classification: %w", err)
	}
	switch value := decoded.(type) {
	case string:
		return &rustFortSessionClassification{State: normalizeRustFortSessionVariant(value)}, nil
	case map[string]any:
		for key, inner := range value {
			out := &rustFortSessionClassification{State: normalizeRustFortSessionVariant(key)}
			if strings.EqualFold(key, "Revoked") {
				if innerMap, ok := inner.(map[string]any); ok {
					out.Reason = strings.TrimSpace(fmt.Sprint(innerMap["reason"]))
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("decode rust fort session classification: unexpected payload")
}

func normalizeRustFortSessionVariant(value string) string {
	switch strings.TrimSpace(value) {
	case "BootstrapRequired":
		return "bootstrap_required"
	case "Resumable":
		return "resumable"
	case "Refreshing":
		return "refreshing"
	case "Revoked":
		return "revoked"
	case "TeardownPending":
		return "teardown_pending"
	case "Closed":
		return "closed"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func maybeDispatchRustCLICompat(command string, args ...string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return false, err
	}
	cmd := rustCLIExecCommand(bin, append([]string{command}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("run rust si cli %q: %w", command, err)
	}
	return true, nil
}

func maybeDispatchRustCLIReadOnly(command string, args ...string) (bool, error) {
	return maybeDispatchRustCLICompat(command, args...)
}

func runRustCLIJSON(args ...string) ([]byte, error) {
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return nil, err
	}
	cmd := rustCLIExecCommand(bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return nil, fmt.Errorf("run rust si cli %q: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return nil, fmt.Errorf("run rust si cli %q: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func runRustCLIJSONWithStdin(stdin []byte, args ...string) ([]byte, error) {
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return nil, err
	}
	cmd := rustCLIExecCommand(bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return nil, fmt.Errorf("run rust si cli %q: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return nil, fmt.Errorf("run rust si cli %q: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func runRustCLIText(args ...string) (string, error) {
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return "", err
	}
	cmd := rustCLIExecCommand(bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return "", fmt.Errorf("run rust si cli %q: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return "", fmt.Errorf("run rust si cli %q: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

func buildRustCodexSpawnPlanArgs(request rustCodexSpawnPlanRequest) []string {
	args := []string{
		"codex",
		"spawn-plan",
		"--format",
		"json",
		"--workspace",
		strings.TrimSpace(request.Workspace),
		"--docker-socket=" + strconv.FormatBool(request.DockerSocket),
		"--detach=" + strconv.FormatBool(request.Detach),
		"--clean-slate=" + strconv.FormatBool(request.CleanSlate),
		"--include-host-si=" + strconv.FormatBool(request.IncludeHostSI),
	}
	if value := strings.TrimSpace(request.Name); value != "" {
		args = append(args, "--name", value)
	}
	if value := strings.TrimSpace(request.ProfileID); value != "" {
		args = append(args, "--profile-id", value)
	}
	if value := strings.TrimSpace(request.Workdir); value != "" {
		args = append(args, "--workdir", value)
	}
	if value := strings.TrimSpace(request.CodexVolume); value != "" {
		args = append(args, "--codex-volume", value)
	}
	if value := strings.TrimSpace(request.SkillsVolume); value != "" {
		args = append(args, "--skills-volume", value)
	}
	if value := strings.TrimSpace(request.GHVolume); value != "" {
		args = append(args, "--gh-volume", value)
	}
	if value := strings.TrimSpace(request.Repo); value != "" {
		args = append(args, "--repo", value)
	}
	if value := strings.TrimSpace(request.GHPAT); value != "" {
		args = append(args, "--gh-pat", value)
	}
	if value := strings.TrimSpace(request.Image); value != "" {
		args = append(args, "--image", value)
	}
	if value := strings.TrimSpace(request.Network); value != "" {
		args = append(args, "--network", value)
	}
	if value := strings.TrimSpace(request.VaultEnvFile); value != "" {
		args = append(args, "--vault-env-file", value)
	}
	return args
}

func buildRustDyadSpawnPlanArgs(request rustDyadSpawnPlanRequest) []string {
	return buildRustDyadSpawnArgs(request, "spawn-plan", true)
}

func buildRustDyadSpawnStartArgs(request rustDyadSpawnPlanRequest) []string {
	return buildRustDyadSpawnArgs(request, "spawn-start", false)
}

func buildRustDyadSpawnArgs(request rustDyadSpawnPlanRequest, subcommand string, includeFormat bool) []string {
	args := []string{
		"dyad", subcommand,
		"--name", strings.TrimSpace(request.Name),
		"--role", strings.TrimSpace(request.Role),
		"--actor-image", strings.TrimSpace(request.ActorImage),
		"--critic-image", strings.TrimSpace(request.CriticImage),
		"--codex-model", strings.TrimSpace(request.CodexModel),
		"--codex-effort-actor", strings.TrimSpace(request.CodexEffortActor),
		"--codex-effort-critic", strings.TrimSpace(request.CodexEffortCritic),
		"--codex-model-low", strings.TrimSpace(request.CodexModelLow),
		"--codex-model-medium", strings.TrimSpace(request.CodexModelMedium),
		"--codex-model-high", strings.TrimSpace(request.CodexModelHigh),
		"--codex-effort-low", strings.TrimSpace(request.CodexEffortLow),
		"--codex-effort-medium", strings.TrimSpace(request.CodexEffortMedium),
		"--codex-effort-high", strings.TrimSpace(request.CodexEffortHigh),
		"--workspace", strings.TrimSpace(request.Workspace),
		"--configs", strings.TrimSpace(request.Configs),
		"--vault-env-file", strings.TrimSpace(request.VaultEnvFile),
		"--codex-volume", strings.TrimSpace(request.CodexVolume),
		"--skills-volume", strings.TrimSpace(request.SkillsVolume),
		"--network", strings.TrimSpace(request.Network),
		"--forward-ports", strings.TrimSpace(request.ForwardPorts),
		"--docker-socket=" + strconv.FormatBool(request.DockerSocket),
		"--profile-id", strings.TrimSpace(request.ProfileID),
		"--profile-name", strings.TrimSpace(request.ProfileName),
		"--loop-goal", strings.TrimSpace(request.LoopGoal),
		"--loop-seed-prompt", strings.TrimSpace(request.LoopSeedPrompt),
		"--loop-tmux-capture", strings.TrimSpace(request.LoopTmuxCapture),
	}
	if includeFormat {
		args = append(args, "--format", "json")
	}
	if request.LoopEnabled != nil {
		args = append(args, "--loop-enabled="+strconv.FormatBool(*request.LoopEnabled))
	}
	if request.LoopAllowMCPStartup != nil {
		args = append(args, "--loop-allow-mcp-startup="+strconv.FormatBool(*request.LoopAllowMCPStartup))
	}
	if request.LoopMaxTurns != nil {
		args = append(args, "--loop-max-turns", strconv.Itoa(*request.LoopMaxTurns))
	}
	if request.LoopSleepSeconds != nil {
		args = append(args, "--loop-sleep-seconds", strconv.Itoa(*request.LoopSleepSeconds))
	}
	if request.LoopStartupDelaySeconds != nil {
		args = append(args, "--loop-startup-delay-seconds", strconv.Itoa(*request.LoopStartupDelaySeconds))
	}
	if request.LoopTurnTimeoutSeconds != nil {
		args = append(args, "--loop-turn-timeout-seconds", strconv.Itoa(*request.LoopTurnTimeoutSeconds))
	}
	if request.LoopRetryMax != nil {
		args = append(args, "--loop-retry-max", strconv.Itoa(*request.LoopRetryMax))
	}
	if request.LoopRetryBaseSeconds != nil {
		args = append(args, "--loop-retry-base-seconds", strconv.Itoa(*request.LoopRetryBaseSeconds))
	}
	if request.LoopPromptLines != nil {
		args = append(args, "--loop-prompt-lines", strconv.Itoa(*request.LoopPromptLines))
	}
	if request.LoopPausePollSeconds != nil {
		args = append(args, "--loop-pause-poll-seconds", strconv.Itoa(*request.LoopPausePollSeconds))
	}
	return filterEmptyFlagPairs(args)
}

func filterEmptyFlagPairs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		current := strings.TrimSpace(args[i])
		if current == "" {
			continue
		}
		if i+1 < len(args) && strings.HasPrefix(current, "--") && !strings.Contains(current, "=") {
			next := strings.TrimSpace(args[i+1])
			if next == "" {
				i++
				continue
			}
			filtered = append(filtered, current, next)
			i++
			continue
		}
		filtered = append(filtered, current)
	}
	return filtered
}

func buildRustCodexSpawnSpecArgs(request rustCodexSpawnSpecRequest) []string {
	args := buildRustCodexSpawnPlanArgs(request.rustCodexSpawnPlanRequest)
	args[1] = "spawn-spec"
	if value := strings.TrimSpace(request.Command); value != "" {
		args = append(args, "--cmd", value)
	}
	for _, value := range request.Env {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, "--env", value)
		}
	}
	for _, value := range request.Labels {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, "--label", value)
		}
	}
	for _, value := range request.Ports {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, "--port", value)
		}
	}
	return args
}

func buildRustCodexSpawnStartArgs(request rustCodexSpawnSpecRequest) []string {
	args := buildRustCodexSpawnSpecArgs(request)
	args[1] = "spawn-start"
	return args
}

func shouldUseExperimentalRustCLI() bool {
	if strings.TrimSpace(os.Getenv(siRustCLIBinEnv)) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(siExperimentalRustCLIEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func resolveRustCLIBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(siRustCLIBinEnv)); explicit != "" {
		path, err := resolveExecutablePath(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", siRustCLIBinEnv, err)
		}
		return path, nil
	}

	if root, err := rustCLIRepoRoot(); err == nil {
		candidate := filepath.Join(root, ".artifacts", "cargo-target", "debug", "si-rs")
		if path, err := resolveExecutablePath(candidate); err == nil {
			return path, nil
		}
	}

	path, err := rustCLILookPath("si-rs")
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf(
		"experimental Rust CLI enabled but no si-rs binary found; set %s or build rust/crates/si-cli",
		siRustCLIBinEnv,
	)
}

func resolveExecutablePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", abs)
	}
	return abs, nil
}
