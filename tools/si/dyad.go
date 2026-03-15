package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"

	shared "si/agents/shared/docker"
)

const dyadUsageText = "usage: si dyad <spawn|list|remove|recreate|status|peek|exec|run|logs|start|stop|restart|cleanup>"

type dyadContainerStatus struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

type dyadStatusResult struct {
	Dyad   string               `json:"dyad"`
	Found  bool                 `json:"found"`
	Actor  *dyadContainerStatus `json:"actor,omitempty"`
	Critic *dyadContainerStatus `json:"critic,omitempty"`
}

type dyadLogsResult struct {
	Dyad   string `json:"dyad"`
	Member string `json:"member"`
	Tail   int    `json:"tail"`
	Logs   string `json:"logs"`
}

func cmdDyad(args []string) {
	if len(args) > 0 {
		switch strings.TrimSpace(args[0]) {
		case "help", "-h", "--help":
			printUsage(dyadUsageText)
			return
		}
	}
	if len(args) == 0 {
		if !isInteractiveTerminal() {
			printUsage(dyadUsageText)
			return
		}
		selected, ok := selectDyadAction()
		if !ok {
			return
		}
		args = []string{selected}
	}
	switch normalizeDyadCommand(args[0]) {
	case "spawn":
		cmdDyadSpawn(args[1:])
	case "list":
		cmdDyadList(args[1:])
	case "remove":
		cmdDyadRemove(args[1:])
	case "recreate":
		cmdDyadRecreate(args[1:])
	case "status":
		cmdDyadStatus(args[1:])
	case "peek":
		cmdDyadPeek(args[1:])
	case "exec":
		cmdDyadExec(args[1:])
	case "logs":
		cmdDyadLogs(args[1:])
	case "start":
		cmdDyadStart(args[1:])
	case "stop":
		cmdDyadStop(args[1:])
	case "restart":
		cmdDyadRestart(args[1:])
	case "cleanup":
		cmdDyadCleanup(args[1:])
	default:
		printUnknown("dyad", args[0])
	}
}

func cmdDyadSpawn(args []string) {
	workspaceSet := flagProvided(args, "workspace")
	configsSet := flagProvided(args, "configs")
	roleProvided := flagProvided(args, "role")
	fs := flag.NewFlagSet("dyad spawn", flag.ExitOnError)
	roleFlag := fs.String("role", "", "dyad role")
	actorImage := fs.String("actor-image", envOr("ACTOR_IMAGE", "aureuma/si:local"), "actor image")
	criticImage := fs.String("critic-image", envOr("CRITIC_IMAGE", "aureuma/si:local"), "critic image")
	codexModel := fs.String("codex-model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	codexEffortActor := fs.String("codex-effort-actor", envOr("CODEX_ACTOR_EFFORT", ""), "codex effort for actor")
	codexEffortCritic := fs.String("codex-effort-critic", envOr("CODEX_CRITIC_EFFORT", ""), "codex effort for critic")
	codexModelLow := fs.String("codex-model-low", envOr("CODEX_MODEL_LOW", ""), "codex model low")
	codexModelMedium := fs.String("codex-model-medium", envOr("CODEX_MODEL_MEDIUM", ""), "codex model medium")
	codexModelHigh := fs.String("codex-model-high", envOr("CODEX_MODEL_HIGH", ""), "codex model high")
	codexEffortLow := fs.String("codex-effort-low", envOr("CODEX_REASONING_EFFORT_LOW", ""), "codex effort low")
	codexEffortMedium := fs.String("codex-effort-medium", envOr("CODEX_REASONING_EFFORT_MEDIUM", ""), "codex effort medium")
	codexEffortHigh := fs.String("codex-effort-high", envOr("CODEX_REASONING_EFFORT_HIGH", ""), "codex effort high")
	skillsVolume := fs.String("skills-volume", envOr("SI_CODEX_SKILLS_VOLUME", ""), "shared codex skills volume name")
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace (repo root)")
	configsHost := fs.String("configs", envOr("SI_CONFIGS_HOST", ""), "host path to configs")
	forwardPorts := fs.String("forward-ports", envOr("SI_DYAD_FORWARD_PORTS", ""), "actor forward ports (default 1455-1465)")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in dyad containers")
	profileKey := fs.String("profile", "", "codex profile name/email/id")
	skipAuth := fs.Bool("skip-auth", false, "skip codex profile auth requirement (for offline/testing use)")
	autopilot := fs.Bool("autopilot", false, "enable dyad autopilot (claims taskboard prompt when --prompt is empty)")
	prompt := fs.String("prompt", "", "initial critic prompt")
	nameArg, filtered := splitDyadSpawnArgs(args)
	if err := fs.Parse(filtered); err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()

	if !flagProvided(args, "actor-image") && strings.TrimSpace(settings.Dyad.ActorImage) != "" {
		*actorImage = strings.TrimSpace(settings.Dyad.ActorImage)
	}
	if !flagProvided(args, "critic-image") && strings.TrimSpace(settings.Dyad.CriticImage) != "" {
		*criticImage = strings.TrimSpace(settings.Dyad.CriticImage)
	}
	if !flagProvided(args, "codex-model") && strings.TrimSpace(settings.Dyad.CodexModel) != "" {
		*codexModel = strings.TrimSpace(settings.Dyad.CodexModel)
	}
	if !flagProvided(args, "codex-effort-actor") && strings.TrimSpace(settings.Dyad.CodexEffortActor) != "" {
		*codexEffortActor = strings.TrimSpace(settings.Dyad.CodexEffortActor)
	}
	if !flagProvided(args, "codex-effort-critic") && strings.TrimSpace(settings.Dyad.CodexEffortCritic) != "" {
		*codexEffortCritic = strings.TrimSpace(settings.Dyad.CodexEffortCritic)
	}
	if !flagProvided(args, "codex-model-low") && strings.TrimSpace(settings.Dyad.CodexModelLow) != "" {
		*codexModelLow = strings.TrimSpace(settings.Dyad.CodexModelLow)
	}
	if !flagProvided(args, "codex-model-medium") && strings.TrimSpace(settings.Dyad.CodexModelMedium) != "" {
		*codexModelMedium = strings.TrimSpace(settings.Dyad.CodexModelMedium)
	}
	if !flagProvided(args, "codex-model-high") && strings.TrimSpace(settings.Dyad.CodexModelHigh) != "" {
		*codexModelHigh = strings.TrimSpace(settings.Dyad.CodexModelHigh)
	}
	if !flagProvided(args, "codex-effort-low") && strings.TrimSpace(settings.Dyad.CodexEffortLow) != "" {
		*codexEffortLow = strings.TrimSpace(settings.Dyad.CodexEffortLow)
	}
	if !flagProvided(args, "codex-effort-medium") && strings.TrimSpace(settings.Dyad.CodexEffortMedium) != "" {
		*codexEffortMedium = strings.TrimSpace(settings.Dyad.CodexEffortMedium)
	}
	if !flagProvided(args, "codex-effort-high") && strings.TrimSpace(settings.Dyad.CodexEffortHigh) != "" {
		*codexEffortHigh = strings.TrimSpace(settings.Dyad.CodexEffortHigh)
	}
	if !flagProvided(args, "skills-volume") && strings.TrimSpace(settings.Dyad.SkillsVolume) != "" {
		*skillsVolume = strings.TrimSpace(settings.Dyad.SkillsVolume)
	} else if !flagProvided(args, "skills-volume") && strings.TrimSpace(settings.Codex.SkillsVolume) != "" {
		*skillsVolume = strings.TrimSpace(settings.Codex.SkillsVolume)
	}
	if !flagProvided(args, "forward-ports") && strings.TrimSpace(settings.Dyad.ForwardPorts) != "" {
		*forwardPorts = strings.TrimSpace(settings.Dyad.ForwardPorts)
	}
	if !flagProvided(args, "docker-socket") && settings.Dyad.DockerSocket != nil {
		*dockerSocket = *settings.Dyad.DockerSocket
	}

	name := strings.TrimSpace(nameArg)
	if name == "" && fs.NArg() > 0 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	if name == "" {
		if !isInteractiveTerminal() {
			printUsage("usage: si dyad spawn <name> [role] [--profile <profile>] [--autopilot] [--prompt <text>]")
			return
		}
		var ok bool
		name, ok = promptRequired("Dyad name:")
		if !ok {
			return
		}
	}
	if err := validateSlug(name); err != nil {
		fatal(err)
	}

	role := strings.TrimSpace(*roleFlag)
	if role == "" && fs.NArg() > 0 {
		role = fs.Arg(0)
	}
	if role == "" && !roleProvided && isInteractiveTerminal() {
		selected, ok := selectDyadRole("generic")
		if !ok {
			return
		}
		role = selected
	}
	if role == "" {
		role = "generic"
	}
	if roleProvided {
		if err := validateDyadSpawnOptionValue("role", role); err != nil {
			fatal(err)
		}
	}

	roleLower := strings.ToLower(role)
	if strings.TrimSpace(*codexEffortActor) == "" || strings.TrimSpace(*codexEffortCritic) == "" {
		actorEffort, criticEffort := defaultEffort(roleLower)
		if strings.TrimSpace(*codexEffortActor) == "" {
			*codexEffortActor = actorEffort
		}
		if strings.TrimSpace(*codexEffortCritic) == "" {
			*codexEffortCritic = criticEffort
		}
	}

	envWorkspace := strings.TrimSpace(os.Getenv("SI_WORKSPACE_HOST"))
	envConfigs := strings.TrimSpace(os.Getenv("SI_CONFIGS_HOST"))
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	resolvedWorkspace, err := resolveWorkspaceDirectory(
		workspaceScopeDyad,
		workspaceSet,
		strings.TrimSpace(*workspaceHost),
		envWorkspace,
		&settings,
		cwd,
	)
	if err != nil {
		fatal(err)
	}
	if resolvedWorkspace.StaleSettings {
		warnf("saved default dyad workspace no longer exists; using %s", resolvedWorkspace.Path)
	}
	*workspaceHost = resolvedWorkspace.Path
	maybePersistWorkspaceDefault(workspaceScopeDyad, &settings, strings.TrimSpace(*workspaceHost), isInteractiveTerminal())
	resolvedConfigs, err := resolveDyadConfigsDirectory(
		configsSet,
		strings.TrimSpace(*configsHost),
		envConfigs,
		&settings,
		strings.TrimSpace(*workspaceHost),
	)
	if err != nil {
		fatal(err)
	}
	if resolvedConfigs.StaleSettings {
		warnf("saved default dyad configs no longer exist; using %s", resolvedConfigs.Path)
	}
	*configsHost = resolvedConfigs.Path
	maybePersistDyadConfigsDefault(&settings, strings.TrimSpace(*configsHost), isInteractiveTerminal())
	if strings.TrimSpace(*forwardPorts) == "" {
		*forwardPorts = "1455-1465"
	}

	seedPrompt := strings.TrimSpace(*prompt)
	if *autopilot && seedPrompt == "" {
		fatal(fmt.Errorf("dyad autopilot now requires --prompt"))
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	var profile *codexProfile
	if !*skipAuth {
		resolved, err := resolveDyadSpawnProfile(strings.TrimSpace(*profileKey))
		if err != nil {
			fatal(err)
		}
		if resolved == nil {
			return
		}
		status := codexProfileAuthStatus(*resolved)
		if !status.Exists {
			fatal(fmt.Errorf("profile %s is not logged in; run `si login %s` first", resolved.ID, resolved.ID))
		}
		profile = resolved
	}
	opts := shared.DyadOptions{
		Dyad:              name,
		Role:              role,
		ActorImage:        *actorImage,
		CriticImage:       *criticImage,
		CodexModel:        *codexModel,
		CodexEffortActor:  *codexEffortActor,
		CodexEffortCritic: *codexEffortCritic,
		CodexModelLow:     *codexModelLow,
		CodexModelMedium:  *codexModelMedium,
		CodexModelHigh:    *codexModelHigh,
		CodexEffortLow:    *codexEffortLow,
		CodexEffortMedium: *codexEffortMedium,
		CodexEffortHigh:   *codexEffortHigh,
		WorkspaceHost:     *workspaceHost,
		ConfigsHost:       *configsHost,
		VaultEnvFile:      vaultContainerEnvFileMountPath(settings),
		SkillsVolume:      strings.TrimSpace(*skillsVolume),
		ForwardPorts:      *forwardPorts,
		Network:           shared.DefaultNetwork,
		DockerSocket:      *dockerSocket,
		ProfileID:         strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_ID")),
		ProfileName:       strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_NAME")),
		LoopEnabled:       dyadLoopEnabledSetting(settings),
		LoopGoal:          dyadLoopStringSetting("DYAD_LOOP_GOAL", settings.Dyad.Loop.Goal),
		LoopSeedPrompt:    dyadLoopStringSetting("DYAD_LOOP_SEED_CRITIC_PROMPT", settings.Dyad.Loop.SeedCriticPrompt),
		LoopMaxTurns:      dyadLoopIntSetting("DYAD_LOOP_MAX_TURNS", settings.Dyad.Loop.MaxTurns),
		LoopSleepSeconds:  dyadLoopIntSetting("DYAD_LOOP_SLEEP_SECONDS", settings.Dyad.Loop.SleepSeconds),
		LoopStartupDelay:  dyadLoopIntSetting("DYAD_LOOP_STARTUP_DELAY_SECONDS", settings.Dyad.Loop.StartupDelaySeconds),
		LoopTurnTimeout:   dyadLoopIntSetting("DYAD_LOOP_TURN_TIMEOUT_SECONDS", settings.Dyad.Loop.TurnTimeoutSeconds),
		LoopRetryMax:      dyadLoopIntSetting("DYAD_LOOP_RETRY_MAX", settings.Dyad.Loop.RetryMax),
		LoopRetryBase:     dyadLoopIntSetting("DYAD_LOOP_RETRY_BASE_SECONDS", settings.Dyad.Loop.RetryBaseSeconds),
		LoopPromptLines:   dyadLoopIntSetting("DYAD_LOOP_PROMPT_LINES", settings.Dyad.Loop.PromptLines),
		LoopAllowMCP:      dyadLoopAllowMCPSetting(settings),
		LoopTmuxCapture:   dyadLoopStringSetting("DYAD_LOOP_TMUX_CAPTURE", settings.Dyad.Loop.TmuxCapture),
		LoopPausePoll:     dyadLoopIntSetting("DYAD_LOOP_PAUSE_POLL_SECONDS", settings.Dyad.Loop.PausePollSeconds),
	}
	if profile != nil {
		opts.ProfileID = profile.ID
		opts.ProfileName = profile.Name
	}
	if seedPrompt != "" {
		opts.LoopSeedPrompt = seedPrompt
	}
	if *autopilot {
		opts.LoopEnabled = boolPtr(true)
	}
	if err := maybeApplyRustDyadSpawnPlan(&opts); err != nil {
		fatal(err)
	}

	actorID, criticID, usedRustStart, err := maybeEnsureDyadSpawnWithRust(ctx, client, opts)
	if err != nil {
		fatal(err)
	}
	if !usedRustStart {
		actorID, criticID, err = client.EnsureDyad(ctx, opts)
		if err != nil {
			fatal(err)
		}
	}
	ensureDyadContainerSiHomeOwnership(ctx, client, actorID)
	ensureDyadContainerSiHomeOwnership(ctx, client, criticID)
	if profile != nil {
		seedDyadProfileAuth(ctx, client, actorID, *profile)
		seedDyadProfileAuth(ctx, client, criticID, *profile)
	}
	if identity, ok := hostGitIdentity(); ok {
		seedGitIdentity(ctx, client, actorID, "si", "/home/si", identity)
		seedGitIdentity(ctx, client, criticID, "si", "/home/si", identity)
	}
	successf("dyad %s ready (role=%s)", name, role)
}

func maybeEnsureDyadSpawnWithRust(ctx context.Context, client *shared.Client, opts shared.DyadOptions) (string, string, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return "", "", false, nil
	}
	actorName := shared.DyadContainerName(opts.Dyad, "actor")
	criticName := shared.DyadContainerName(opts.Dyad, "critic")
	actorID, _, err := client.ContainerByName(ctx, actorName)
	if err != nil {
		return "", "", false, err
	}
	criticID, _, err := client.ContainerByName(ctx, criticName)
	if err != nil {
		return "", "", false, err
	}
	if actorID != "" || criticID != "" {
		return "", "", false, nil
	}
	delegated, err := maybeStartRustDyadSpawn(rustDyadSpawnPlanRequest{
		Name:                    opts.Dyad,
		Role:                    opts.Role,
		ActorImage:              opts.ActorImage,
		CriticImage:             opts.CriticImage,
		CodexModel:              opts.CodexModel,
		CodexEffortActor:        opts.CodexEffortActor,
		CodexEffortCritic:       opts.CodexEffortCritic,
		CodexModelLow:           opts.CodexModelLow,
		CodexModelMedium:        opts.CodexModelMedium,
		CodexModelHigh:          opts.CodexModelHigh,
		CodexEffortLow:          opts.CodexEffortLow,
		CodexEffortMedium:       opts.CodexEffortMedium,
		CodexEffortHigh:         opts.CodexEffortHigh,
		Workspace:               opts.WorkspaceHost,
		Configs:                 opts.ConfigsHost,
		VaultEnvFile:            opts.VaultEnvFile,
		CodexVolume:             opts.CodexVolume,
		SkillsVolume:            opts.SkillsVolume,
		Network:                 opts.Network,
		ForwardPorts:            opts.ForwardPorts,
		DockerSocket:            opts.DockerSocket,
		ProfileID:               opts.ProfileID,
		ProfileName:             opts.ProfileName,
		LoopEnabled:             opts.LoopEnabled,
		LoopGoal:                opts.LoopGoal,
		LoopSeedPrompt:          opts.LoopSeedPrompt,
		LoopMaxTurns:            intPtrValue(opts.LoopMaxTurns),
		LoopSleepSeconds:        intPtrValue(opts.LoopSleepSeconds),
		LoopStartupDelaySeconds: intPtrValue(opts.LoopStartupDelay),
		LoopTurnTimeoutSeconds:  intPtrValue(opts.LoopTurnTimeout),
		LoopRetryMax:            intPtrValue(opts.LoopRetryMax),
		LoopRetryBaseSeconds:    intPtrValue(opts.LoopRetryBase),
		LoopPromptLines:         intPtrValue(opts.LoopPromptLines),
		LoopAllowMCPStartup:     opts.LoopAllowMCP,
		LoopTmuxCapture:         opts.LoopTmuxCapture,
		LoopPausePollSeconds:    intPtrValue(opts.LoopPausePoll),
	})
	if err != nil {
		return "", "", false, err
	}
	if !delegated {
		return "", "", false, nil
	}
	actorID, _, err = client.ContainerByName(ctx, actorName)
	if err != nil {
		return "", "", true, err
	}
	criticID, _, err = client.ContainerByName(ctx, criticName)
	if err != nil {
		return "", "", true, err
	}
	if actorID == "" || criticID == "" {
		return "", "", true, fmt.Errorf("rust dyad spawn did not create both actor and critic containers")
	}
	return actorID, criticID, true, nil
}

func cmdDyadPeek(args []string) {
	fs := flag.NewFlagSet("dyad peek", flag.ExitOnError)
	member := fs.String("member", "both", "actor, critic, or both")
	detached := fs.Bool("detached", false, "create host tmux session but do not attach")
	hostSession := fs.String("session", "", "host tmux session name (default: si-dyad-peek-<dyad>)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	dyad := ""
	if fs.NArg() > 0 {
		dyad = strings.TrimSpace(fs.Arg(0))
	}
	if dyad == "" {
		selected, ok := selectDyadName("peek")
		if !ok {
			return
		}
		dyad = selected
	}

	memberVal := strings.ToLower(strings.TrimSpace(*member))
	if memberVal == "" {
		memberVal = "both"
	}
	switch memberVal {
	case "both", "actor", "critic":
	default:
		fatal(fmt.Errorf("invalid member %q (expected actor, critic, or both)", memberVal))
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	actorContainer := shared.DyadContainerName(dyad, "actor")
	criticContainer := shared.DyadContainerName(dyad, "critic")
	actorID, _, err := client.ContainerByName(ctx, actorContainer)
	if err != nil {
		fatal(err)
	}
	criticID, _, err := client.ContainerByName(ctx, criticContainer)
	if err != nil {
		fatal(err)
	}
	if actorID == "" && criticID == "" {
		fatal(fmt.Errorf("dyad not found: %s", dyad))
	}

	suffix := sanitizeDyadTmuxSuffix(dyad)
	actorSession := fmt.Sprintf("si-dyad-%s-actor", suffix)
	criticSession := fmt.Sprintf("si-dyad-%s-critic", suffix)

	peekSession := strings.TrimSpace(*hostSession)
	if peekSession == "" {
		peekSession = fmt.Sprintf("si-dyad-peek-%s", suffix)
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		fatal(fmt.Errorf("tmux not found in PATH: %w", err))
	}

	actorCmd := dyadPeekAttachCmd(actorContainer, actorSession)
	criticCmd := dyadPeekAttachCmd(criticContainer, criticSession)

	// Always create (or replace) the host peek session for predictable behavior.
	_ = dyadTmuxRun("kill-session", "-t", peekSession)

	var first string
	switch memberVal {
	case "actor":
		first = actorCmd
	case "critic":
		first = criticCmd
	default:
		first = actorCmd
	}
	if err := dyadTmuxRun("new-session", "-d", "-s", peekSession, "bash", "-lc", first); err != nil {
		fatal(err)
	}
	dyadApplyTmuxSessionDefaults(peekSession)
	// Make pane titles visible and consistent.
	_ = dyadTmuxRun("rename-window", "-t", peekSession+":0", dyadPeekWindowTitle(dyad))
	_ = dyadTmuxRun("set-option", "-t", peekSession, "pane-border-status", "top")
	_ = dyadTmuxRun("set-option", "-t", peekSession, "pane-border-format", "#{pane_title}")

	if memberVal == "both" {
		if err := dyadTmuxRun("split-window", "-h", "-t", peekSession+":0", "bash", "-lc", criticCmd); err != nil {
			_ = dyadTmuxRun("kill-session", "-t", peekSession)
			fatal(err)
		}
		_, _ = dyadTmuxOutput("select-layout", "-t", peekSession, "even-horizontal")
	}
	// Name panes so the user can immediately tell which dyad member they're driving.
	switch memberVal {
	case "actor":
		_ = dyadTmuxRun("select-pane", "-t", peekSession+":0.0", "-T", dyadPeekPaneTitle(dyad, "actor"))
	case "critic":
		_ = dyadTmuxRun("select-pane", "-t", peekSession+":0.0", "-T", dyadPeekPaneTitle(dyad, "critic"))
	default:
		_ = dyadTmuxRun("select-pane", "-t", peekSession+":0.0", "-T", dyadPeekPaneTitle(dyad, "actor"))
		_ = dyadTmuxRun("select-pane", "-t", peekSession+":0.1", "-T", dyadPeekPaneTitle(dyad, "critic"))
	}

	if *detached {
		successf("dyad peek session ready: %s", peekSession)
		return
	}
	if !isInteractiveTerminal() {
		fatal(errors.New("dyad peek requires an interactive terminal (or use --detached)"))
	}
	if err := dyadTmuxAttach(peekSession); err != nil {
		fatal(err)
	}
}

func dyadTmuxRun(args ...string) error {
	if err := validateTmuxArgs(args); err != nil {
		return err
	}
	// #nosec G204 -- fixed binary with validated tmux argument list.
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

func dyadTmuxOutput(args ...string) ([]byte, error) {
	if err := validateTmuxArgs(args); err != nil {
		return nil, err
	}
	// #nosec G204 -- fixed binary with validated tmux argument list.
	cmd := exec.Command("tmux", args...)
	return cmd.Output()
}

func dyadTmuxAttach(session string) error {
	args := []string{"attach-session", "-t", session}
	if err := validateTmuxArgs(args); err != nil {
		return err
	}
	// #nosec G204 -- fixed binary with validated tmux argument list.
	cmd := exec.Command("tmux", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func dyadApplyTmuxSessionDefaults(session string) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	_, _ = dyadTmuxOutput("set-option", "-t", session, "remain-on-exit", "off")
	_, _ = dyadTmuxOutput("set-option", "-t", session, "mouse", "on")
	_, _ = dyadTmuxOutput("set-option", "-t", session, "history-limit", siTmuxHistoryLimit)
}

func validateTmuxArgs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tmux args required")
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("invalid tmux argument: contains NUL byte")
		}
	}
	return nil
}

func dyadPeekWindowTitle(dyad string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		dyad = "unknown"
	}
	return "🪢 " + dyad
}

func dyadPeekPaneTitle(dyad string, member string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		dyad = "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(member)) {
	case "actor":
		return "🪢 " + dyad + " 🛩️ actor"
	case "critic":
		return "🪢 " + dyad + " 🧠 critic"
	default:
		return "🪢 " + dyad
	}
}

func dyadPeekAttachCmd(container, session string) string {
	// Keep the command shell-parseable but safe: dyad/container/session names are slug-validated.
	container = strings.TrimSpace(container)
	session = strings.TrimSpace(session)
	if container == "" || session == "" {
		return "echo missing dyad peek target; sleep 3"
	}
	return strings.TrimSpace(fmt.Sprintf(`
set -e
while ! docker exec %s tmux has-session -t %s >/dev/null 2>&1; do
  sleep 0.2
done
exec docker exec -it %s tmux attach -t %s
`, container, session, container, session))
}

func sanitizeDyadTmuxSuffix(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	return out
}

func defaultEffort(role string) (string, string) {
	switch role {
	case "infra":
		return "xhigh", "xhigh"
	case "research":
		return "high", "high"
	case "webdev", "web":
		return "medium", "high"
	default:
		return "medium", "medium"
	}
}

func dyadLoopEnabledSetting(settings Settings) *bool {
	if val, ok := dyadLoopBoolEnv("DYAD_LOOP_ENABLED"); ok {
		return boolPtr(val)
	}
	return settings.Dyad.Loop.Enabled
}

func dyadLoopAllowMCPSetting(settings Settings) *bool {
	if val, ok := dyadLoopBoolEnv("DYAD_LOOP_ALLOW_MCP_STARTUP"); ok {
		return boolPtr(val)
	}
	return settings.Dyad.Loop.AllowMCPStartup
}

func dyadLoopStringSetting(envKey string, fallback string) string {
	val := strings.TrimSpace(os.Getenv(strings.TrimSpace(envKey)))
	if val != "" {
		return val
	}
	return strings.TrimSpace(fallback)
}

func maybeApplyRustDyadSpawnPlan(opts *shared.DyadOptions) error {
	if opts == nil {
		return nil
	}
	request := rustDyadSpawnPlanRequest{
		Name:                strings.TrimSpace(opts.Dyad),
		Role:                strings.TrimSpace(opts.Role),
		ActorImage:          strings.TrimSpace(opts.ActorImage),
		CriticImage:         strings.TrimSpace(opts.CriticImage),
		CodexModel:          strings.TrimSpace(opts.CodexModel),
		CodexEffortActor:    strings.TrimSpace(opts.CodexEffortActor),
		CodexEffortCritic:   strings.TrimSpace(opts.CodexEffortCritic),
		CodexModelLow:       strings.TrimSpace(opts.CodexModelLow),
		CodexModelMedium:    strings.TrimSpace(opts.CodexModelMedium),
		CodexModelHigh:      strings.TrimSpace(opts.CodexModelHigh),
		CodexEffortLow:      strings.TrimSpace(opts.CodexEffortLow),
		CodexEffortMedium:   strings.TrimSpace(opts.CodexEffortMedium),
		CodexEffortHigh:     strings.TrimSpace(opts.CodexEffortHigh),
		Workspace:           strings.TrimSpace(opts.WorkspaceHost),
		Configs:             strings.TrimSpace(opts.ConfigsHost),
		VaultEnvFile:        strings.TrimSpace(opts.VaultEnvFile),
		CodexVolume:         strings.TrimSpace(opts.CodexVolume),
		SkillsVolume:        strings.TrimSpace(opts.SkillsVolume),
		Network:             strings.TrimSpace(opts.Network),
		ForwardPorts:        strings.TrimSpace(opts.ForwardPorts),
		DockerSocket:        opts.DockerSocket,
		ProfileID:           strings.TrimSpace(opts.ProfileID),
		ProfileName:         strings.TrimSpace(opts.ProfileName),
		LoopEnabled:         opts.LoopEnabled,
		LoopGoal:            strings.TrimSpace(opts.LoopGoal),
		LoopSeedPrompt:      strings.TrimSpace(opts.LoopSeedPrompt),
		LoopAllowMCPStartup: opts.LoopAllowMCP,
		LoopTmuxCapture:     strings.TrimSpace(opts.LoopTmuxCapture),
	}
	request.LoopMaxTurns = intPtrValue(opts.LoopMaxTurns)
	request.LoopSleepSeconds = intPtrValue(opts.LoopSleepSeconds)
	request.LoopStartupDelaySeconds = intPtrValue(opts.LoopStartupDelay)
	request.LoopTurnTimeoutSeconds = intPtrValue(opts.LoopTurnTimeout)
	request.LoopRetryMax = intPtrValue(opts.LoopRetryMax)
	request.LoopRetryBaseSeconds = intPtrValue(opts.LoopRetryBase)
	request.LoopPromptLines = intPtrValue(opts.LoopPromptLines)
	request.LoopPausePollSeconds = intPtrValue(opts.LoopPausePoll)

	plan, delegated, err := maybeBuildRustDyadSpawnPlan(request)
	if err != nil {
		return err
	}
	if !delegated {
		return nil
	}
	opts.Role = strings.TrimSpace(plan.Role)
	opts.ActorImage = strings.TrimSpace(plan.Actor.Image)
	opts.CriticImage = strings.TrimSpace(plan.Critic.Image)
	opts.WorkspaceHost = strings.TrimSpace(plan.WorkspaceHost)
	opts.ConfigsHost = strings.TrimSpace(plan.ConfigsHost)
	opts.CodexVolume = strings.TrimSpace(plan.CodexVolume)
	opts.SkillsVolume = strings.TrimSpace(plan.SkillsVolume)
	opts.Network = strings.TrimSpace(plan.NetworkName)
	opts.ForwardPorts = strings.TrimSpace(plan.ForwardPorts)
	opts.DockerSocket = plan.DockerSocket
	return nil
}

func intPtrValue(value int) *int {
	return &value
}

func dyadLoopIntSetting(envKey string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(strings.TrimSpace(envKey)))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func dyadLoopBoolEnv(envKey string) (bool, bool) {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(strings.TrimSpace(envKey))))
	if val == "" {
		return false, false
	}
	switch val {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func cmdDyadList(args []string) {
	fs := flag.NewFlagSet("dyad list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si dyad list [--json]")
		return
	}
	if output, delegated, err := maybeRunRustDyadList(*jsonOut); err != nil {
		fatal(err)
	} else if delegated {
		if strings.TrimSpace(output) != "" {
			fmt.Print(output)
		}
		return
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	containers, err := client.ListContainers(context.Background(), true, map[string]string{shared.LabelApp: shared.DyadAppLabel})
	if err != nil {
		fatal(err)
	}
	rows := buildDyadRows(containers)
	if *jsonOut {
		printJSON(rows)
		return
	}
	if len(rows) == 0 {
		infof("no dyads found")
		return
	}
	printDyadRows(rows, false)
}

func cmdDyadRemove(args []string) {
	fs := flag.NewFlagSet("dyad remove", flag.ExitOnError)
	all := fs.Bool("all", false, "remove all dyads (prompts for confirmation)")
	_ = fs.Parse(args)
	name := ""
	if fs.NArg() > 0 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	if *all {
		if name != "" || fs.NArg() > 0 {
			printUsage("usage: si dyad remove [--all] <name>")
			return
		}
		containers, err := client.ListContainers(ctx, true, map[string]string{shared.LabelApp: shared.DyadAppLabel})
		if err != nil {
			fatal(err)
		}
		rows := buildDyadRows(containers)
		if len(rows) == 0 {
			infof("no dyads found")
			return
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			if strings.TrimSpace(row.Dyad) != "" {
				names = append(names, row.Dyad)
			}
		}
		confirmed, ok := confirmYN(fmt.Sprintf("Remove ALL dyads (%d): %s?", len(names), strings.Join(names, ", ")), false)
		if !ok || !confirmed {
			infof("canceled")
			return
		}
		removed := 0
		for _, dyad := range names {
			if err := client.RemoveDyad(ctx, dyad, true); err != nil {
				warnf("remove dyad %s failed: %v", dyad, err)
				continue
			}
			removed++
		}
		successf("removed %d dyads", removed)
		return
	}

	if name == "" {
		selected, ok := selectDyadName("remove")
		if !ok {
			return
		}
		name = selected
	}
	if output, delegated, err := removeDyadWithCompatibility(ctx, client, name); err != nil {
		fatal(err)
	} else if delegated {
		if strings.TrimSpace(output) != "" {
			fmt.Print(output)
		}
		successf("dyad %s removed", name)
		return
	}
	if err := client.RemoveDyad(ctx, name, true); err != nil {
		fatal(err)
	}
	successf("dyad %s removed", name)
}

func cmdDyadRecreate(args []string) {
	if len(args) < 1 || strings.TrimSpace(args[0]) == "" {
		selected, ok := selectDyadName("recreate")
		if !ok {
			return
		}
		args = append([]string{selected}, args...)
	}
	name := args[0]
	profileArg, hasProfileArg := dyadProfileArg(args[1:])
	if !hasProfileArg {
		profileArg = ""
	}
	skipAuth := dyadSkipAuthArg(args[1:])
	if !skipAuth {
		resolved, err := resolveDyadSpawnProfile(profileArg)
		if err != nil {
			fatal(err)
		}
		if resolved == nil {
			return
		}
		status := codexProfileAuthStatus(*resolved)
		if !status.Exists {
			fatal(fmt.Errorf("profile %s is not logged in; run `si login %s` first", resolved.ID, resolved.ID))
		}
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	if output, delegated, err := removeDyadWithCompatibility(context.Background(), client, name); err != nil {
		fatal(err)
	} else if delegated && strings.TrimSpace(output) != "" {
		fmt.Print(output)
	}
	cmdDyadSpawn(args)
}

func removeDyadWithCompatibility(ctx context.Context, client *shared.Client, name string) (string, bool, error) {
	if output, delegated, err := maybeRunRustDyadRemove(name); err != nil {
		return "", false, err
	} else if delegated {
		return output, true, nil
	}
	if client == nil {
		return "", false, fmt.Errorf("dyad client required")
	}
	return "", false, client.RemoveDyad(ctx, name, true)
}

func dyadSkipAuthArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		raw := strings.TrimSpace(args[i])
		if raw == "" {
			continue
		}
		if raw == "--skip-auth" {
			if i+1 < len(args) {
				val := strings.TrimSpace(args[i+1])
				if isBoolLiteral(val) {
					val = strings.ToLower(strings.TrimSpace(val))
					return val == "1" || val == "true" || val == "t"
				}
			}
			return true
		}
		if strings.HasPrefix(raw, "--skip-auth=") {
			val := strings.TrimSpace(strings.TrimPrefix(raw, "--skip-auth="))
			if !isBoolLiteral(val) {
				continue
			}
			val = strings.ToLower(strings.TrimSpace(val))
			return val == "1" || val == "true" || val == "t"
		}
	}
	return false
}

func cmdDyadStatus(args []string) {
	fs := flag.NewFlagSet("dyad status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 1 {
		printUsage("usage: si dyad status [--json] <name>")
		return
	}
	name := ""
	if fs.NArg() > 0 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	if name == "" {
		selected, ok := selectDyadName("status")
		if !ok {
			return
		}
		name = selected
	}
	if rustResult, delegated, err := maybeReadRustDyadStatus(name); err != nil {
		fatal(err)
	} else if delegated {
		result := dyadStatusResult{
			Dyad:  strings.TrimSpace(rustResult.Dyad),
			Found: rustResult.Found,
		}
		if rustResult.Actor != nil {
			result.Actor = &dyadContainerStatus{
				Name:   strings.TrimSpace(rustResult.Actor.Name),
				ID:     strings.TrimSpace(rustResult.Actor.ID),
				Status: strings.TrimSpace(rustResult.Actor.Status),
			}
		}
		if rustResult.Critic != nil {
			result.Critic = &dyadContainerStatus{
				Name:   strings.TrimSpace(rustResult.Critic.Name),
				ID:     strings.TrimSpace(rustResult.Critic.ID),
				Status: strings.TrimSpace(rustResult.Critic.Status),
			}
		}
		renderDyadStatusResult(result, *jsonOut)
		return
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	actorName := shared.DyadContainerName(name, "actor")
	criticName := shared.DyadContainerName(name, "critic")
	actorID, actorInfo, err := client.ContainerByName(ctx, actorName)
	if err != nil {
		fatal(err)
	}
	criticID, criticInfo, err := client.ContainerByName(ctx, criticName)
	if err != nil {
		fatal(err)
	}
	renderDyadStatusResult(buildDyadStatusResult(name, actorID, actorInfo, criticID, criticInfo), *jsonOut)
}

func renderDyadStatusResult(result dyadStatusResult, jsonOut bool) {
	if !result.Found {
		if jsonOut {
			printJSON(result)
			return
		}
		fmt.Printf("%s %s\n", styleError("dyad not found:"), styleCmd(result.Dyad))
		return
	}
	if jsonOut {
		printJSON(result)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("dyad"), styleCmd(result.Dyad))
	if result.Actor != nil {
		fmt.Printf(" %s %s (%s)\n", styleSection("actor:"), shortContainerID(result.Actor.ID), styleStatus(result.Actor.Status))
	} else {
		fmt.Printf(" %s %s\n", styleSection("actor:"), styleError("missing"))
	}
	if result.Critic != nil {
		fmt.Printf(" %s %s (%s)\n", styleSection("critic:"), shortContainerID(result.Critic.ID), styleStatus(result.Critic.Status))
	} else {
		fmt.Printf(" %s %s\n", styleSection("critic:"), styleError("missing"))
	}
}

func buildDyadStatusResult(name string, actorID string, actorInfo *types.ContainerJSON, criticID string, criticInfo *types.ContainerJSON) dyadStatusResult {
	out := dyadStatusResult{
		Dyad: strings.TrimSpace(name),
	}
	actorID = strings.TrimSpace(actorID)
	if actorInfo != nil && actorID != "" {
		out.Actor = &dyadContainerStatus{
			Name:   shared.DyadContainerName(name, "actor"),
			ID:     actorID,
			Status: dyadContainerState(actorInfo),
		}
	}
	criticID = strings.TrimSpace(criticID)
	if criticInfo != nil && criticID != "" {
		out.Critic = &dyadContainerStatus{
			Name:   shared.DyadContainerName(name, "critic"),
			ID:     criticID,
			Status: dyadContainerState(criticInfo),
		}
	}
	out.Found = out.Actor != nil || out.Critic != nil
	return out
}

func shortContainerID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func dyadContainerState(info *types.ContainerJSON) string {
	if info == nil || info.State == nil {
		return ""
	}
	return strings.TrimSpace(info.State.Status)
}

func cmdDyadExec(args []string) {
	memberProvided := flagProvided(args, "member")
	fs := flag.NewFlagSet("dyad exec", flag.ExitOnError)
	member := fs.String("member", "actor", "actor or critic")
	tty := fs.Bool("tty", false, "allocate TTY")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	dyad := ""
	if fs.NArg() > 0 {
		dyad = strings.TrimSpace(fs.Arg(0))
	}
	if dyad == "" {
		selected, ok := selectDyadName("exec")
		if !ok {
			return
		}
		dyad = selected
	}
	memberVal, err := normalizeDyadMember(*member, "actor")
	if err != nil {
		fatal(err)
	}
	if !memberProvided && isInteractiveTerminal() {
		selected, ok := selectDyadMember("exec", memberVal)
		if !ok {
			return
		}
		memberVal = selected
	}

	rest := fs.Args()
	cmd := []string{}
	if len(rest) > 1 {
		cmd = rest[1:]
	}
	if len(cmd) > 0 && cmd[0] == "--" {
		cmd = cmd[1:]
	}
	if len(cmd) == 0 {
		if !isInteractiveTerminal() {
			printUsage("usage: si dyad exec [--member actor|critic] [--tty] <dyad> -- <cmd...>")
			return
		}
		line, ok := promptWithDefault("Command:", "bash")
		if !ok {
			return
		}
		line = strings.TrimSpace(line)
		if strings.ContainsAny(line, " \t") {
			cmd = []string{"bash", "-lc", line}
		} else {
			cmd = []string{line}
		}
	}
	if err := execInDyad(dyad, memberVal, cmd, *tty); err != nil {
		fatal(err)
	}
}

func execInDyad(dyad, member string, cmd []string, tty bool) error {
	if len(cmd) == 0 {
		return errors.New("command required")
	}
	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()
	containerName := shared.DyadContainerName(dyad, member)
	id, info, err := client.ContainerByName(context.Background(), containerName)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("container not found: %s", containerName)
	}
	if !shared.HasHostSiMount(info, "/home/si") {
		return fmt.Errorf("dyad container %s is missing host ~/.si mount required for full `si vault` support; run `si dyad recreate %s`", containerName, strings.TrimSpace(dyad))
	}
	if !shared.HasHostSSHDirMount(info, "/home/si") {
		return fmt.Errorf("dyad container %s is missing host ~/.ssh mount required for git+ssh workflows; run `si dyad recreate %s`", containerName, strings.TrimSpace(dyad))
	}
	requiredVaultFile := vaultContainerEnvFileMountPath(loadSettingsOrDefault())
	if strings.TrimSpace(requiredVaultFile) != "" && !shared.HasHostVaultEnvFileMount(info, requiredVaultFile) {
		return fmt.Errorf("dyad container %s is missing the host vault env file mount required for `si vault`; run `si dyad recreate %s`", containerName, strings.TrimSpace(dyad))
	}
	if delegated, err := maybeRunRustDyadExec(dyad, member, tty, cmd); err != nil {
		return err
	} else if delegated {
		return nil
	}
	opts := shared.ExecOptions{
		TTY:  tty,
		User: "si",
	}
	return client.Exec(context.Background(), id, cmd, opts, os.Stdin, os.Stdout, os.Stderr)
}

func cmdDyadLogs(args []string) {
	memberProvided := flagProvided(args, "member")
	dyadArg, filtered := splitDyadLogsNameAndFlags(args)
	fs := flag.NewFlagSet("dyad logs", flag.ExitOnError)
	member := fs.String("member", "critic", "actor or critic")
	tail := fs.Int("tail", 200, "lines to tail")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(filtered); err != nil {
		fatal(err)
	}
	dyad := strings.TrimSpace(dyadArg)
	rest := fs.Args()
	if dyad == "" && len(rest) > 0 {
		dyad = strings.TrimSpace(rest[0])
		rest = rest[1:]
	}
	if len(rest) > 0 {
		printUsage("usage: si dyad logs [--member actor|critic] [--tail N] <dyad>")
		return
	}
	if dyad == "" {
		selected, ok := selectDyadName("logs")
		if !ok {
			return
		}
		dyad = selected
	}
	memberVal, err := normalizeDyadMember(*member, "critic")
	if err != nil {
		fatal(err)
	}
	if !memberProvided && isInteractiveTerminal() {
		selected, ok := selectDyadMember("logs", memberVal)
		if !ok {
			return
		}
		memberVal = selected
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	containerName := shared.DyadContainerName(dyad, memberVal)
	id, _, err := client.ContainerByName(context.Background(), containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("container not found: %s", containerName))
	}
	out := ""
	if output, delegated, err := maybeRunRustDyadLogs(dyad, memberVal, *tail); err != nil {
		fatal(err)
	} else if delegated {
		out = output
	} else {
		out, err = client.Logs(context.Background(), id, shared.LogsOptions{Tail: *tail})
		if err != nil {
			fatal(err)
		}
	}
	if *jsonOut {
		printJSON(dyadLogsResult{
			Dyad:   dyad,
			Member: memberVal,
			Tail:   *tail,
			Logs:   out,
		})
		return
	}
	fmt.Print(out)
}

func splitDyadLogsNameAndFlags(args []string) (string, []string) {
	return splitNameAndFlags(args, map[string]bool{
		"member": false,
		"tail":   false,
		"json":   true,
	})
}

func cmdDyadRestart(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("restart")
		if !ok {
			return
		}
		name = selected
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	if output, delegated, err := maybeRunRustDyadContainerAction("restart", name); err != nil {
		fatal(err)
	} else if delegated {
		if strings.TrimSpace(output) != "" {
			fmt.Print(output)
		}
		successf("dyad %s restarted", name)
		return
	}
	if err := client.RestartDyad(context.Background(), name); err != nil {
		fatal(err)
	}
	successf("dyad %s restarted", name)
}

func cmdDyadStart(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("start")
		if !ok {
			return
		}
		name = selected
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	targets, err := dyadContainerTargets(ctx, client, name)
	if err != nil {
		fatal(err)
	}
	if len(targets) == 0 {
		fmt.Printf("%s %s\n", styleError("dyad not found:"), styleCmd(name))
		return
	}
	if output, delegated, err := maybeRunRustDyadContainerAction("start", name); err != nil {
		fatal(err)
	} else if delegated {
		if strings.TrimSpace(output) != "" {
			fmt.Print(output)
		}
		successf("dyad %s started", name)
		return
	}
	if err := execDockerCLI(append([]string{"start"}, targets...)...); err != nil {
		fatal(err)
	}
	successf("dyad %s started", name)
}

func cmdDyadStop(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("stop")
		if !ok {
			return
		}
		name = selected
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	targets, err := dyadContainerTargets(ctx, client, name)
	if err != nil {
		fatal(err)
	}
	if len(targets) == 0 {
		fmt.Printf("%s %s\n", styleError("dyad not found:"), styleCmd(name))
		return
	}
	if output, delegated, err := maybeRunRustDyadContainerAction("stop", name); err != nil {
		fatal(err)
	} else if delegated {
		if strings.TrimSpace(output) != "" {
			fmt.Print(output)
		}
		successf("dyad %s stopped", name)
		return
	}
	if err := execDockerCLI(append([]string{"stop"}, targets...)...); err != nil {
		fatal(err)
	}
	successf("dyad %s stopped", name)
}

func dyadContainerTargets(ctx context.Context, client *shared.Client, dyad string) ([]string, error) {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return nil, errors.New("dyad required")
	}
	actorName := shared.DyadContainerName(dyad, "actor")
	criticName := shared.DyadContainerName(dyad, "critic")
	actorID, _, err := client.ContainerByName(ctx, actorName)
	if err != nil {
		return nil, err
	}
	criticID, _, err := client.ContainerByName(ctx, criticName)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, 2)
	if strings.TrimSpace(actorID) != "" {
		targets = append(targets, actorName)
	}
	if strings.TrimSpace(criticID) != "" {
		targets = append(targets, criticName)
	}
	return targets, nil
}

func cmdDyadCleanup(args []string) {
	if len(args) > 0 {
		printUsage("usage: si dyad cleanup")
		return
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	containers, err := client.ListContainers(ctx, true, map[string]string{shared.LabelApp: shared.DyadAppLabel})
	if err != nil {
		fatal(err)
	}
	removed := 0
	if output, delegated, err := maybeRunRustDyadCleanup(); err != nil {
		fatal(err)
	} else if delegated {
		trimmed := strings.TrimSpace(output)
		if strings.HasPrefix(trimmed, "removed=") {
			if count, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "removed="))); parseErr == nil {
				successf("removed %d stopped dyad containers", count)
				return
			}
		}
		if trimmed != "" {
			fmt.Print(output)
		}
		return
	}
	for _, c := range containers {
		if c.State == "running" {
			continue
		}
		if err := client.RemoveContainer(ctx, c.ID, true); err == nil {
			removed++
		}
	}
	successf("removed %d stopped dyad containers", removed)
}

func resolveDyadSpawnProfile(profileKey string) (*codexProfile, error) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey != "" {
		profile, err := requireCodexProfile(profileKey)
		if err != nil {
			return nil, err
		}
		return &profile, nil
	}

	defaultKey := codexDefaultProfileKey(loadSettingsOrDefault())
	if strings.TrimSpace(defaultKey) != "" {
		if profile, ok := codexProfileByKey(defaultKey); ok {
			if codexProfileAuthStatus(profile).Exists {
				return &profile, nil
			}
		}
	}

	loggedIn := loggedInProfiles()
	if len(loggedIn) == 1 {
		profile := loggedIn[0]
		return &profile, nil
	}

	if isInteractiveTerminal() {
		if len(codexProfileSummaries()) == 0 {
			return nil, errors.New("no codex profiles configured; run `si login` first")
		}
		selected, ok := selectCodexProfile("dyad spawn", defaultKey)
		if !ok {
			return nil, nil
		}
		return &selected, nil
	}

	if len(loggedIn) == 0 {
		return nil, errors.New("no logged-in profiles found; run `si login` first")
	}
	return nil, fmt.Errorf("multiple logged-in profiles found; use `--profile <profile>`")
}

func dyadProfileArg(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "--profile=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--profile=")), true
		}
		if arg == "--profile" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1]), true
		}
	}
	return "", false
}

func seedDyadProfileAuth(ctx context.Context, client *shared.Client, containerID string, profile codexProfile) {
	if client == nil || strings.TrimSpace(containerID) == "" || strings.TrimSpace(profile.ID) == "" {
		return
	}
	hostPath, err := codexProfileAuthPath(profile)
	if err != nil {
		warnf("dyad auth copy skipped: %v", err)
		return
	}
	data, err := readLocalFile(hostPath)
	if err != nil {
		if !os.IsNotExist(err) {
			warnf("dyad auth copy skipped: %v", err)
		}
		return
	}
	const destPath = "/home/si/.codex/auth.json"
	_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", "/home/si/.codex"}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
		warnf("dyad auth copy failed: %v", err)
		return
	}
	_ = client.Exec(ctx, containerID, []string{"chown", "si:si", destPath}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
}

func ensureDyadContainerSiHomeOwnership(ctx context.Context, client *shared.Client, containerID string) {
	if client == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	// Keep codex home writable for host-mapped "si" users in migrated dyad containers.
	_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", "/home/si/.codex", "/home/si/.codex/skills"}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	_ = client.Exec(ctx, containerID, []string{"chown", "-R", "si:si", "/home/si/.codex"}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
