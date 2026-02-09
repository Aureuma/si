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

	shared "si/agents/shared/docker"
)

const dyadUsageText = "usage: si dyad <spawn|list|remove|recreate|status|peek|exec|run|logs|start|stop|restart|cleanup>"

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
	roleProvided := flagProvided(args, "role")
	deptProvided := flagProvided(args, "department")
	fs := flag.NewFlagSet("dyad spawn", flag.ExitOnError)
	roleFlag := fs.String("role", "", "dyad role")
	deptFlag := fs.String("department", "", "dyad department")
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
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace (repo root)")
	configsHost := fs.String("configs", envOr("SI_CONFIGS_HOST", ""), "host path to configs")
	forwardPorts := fs.String("forward-ports", envOr("SI_DYAD_FORWARD_PORTS", ""), "actor forward ports (default 1455-1465)")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in dyad containers")
	profileKey := fs.String("profile", "", "codex profile name/email/id")
	skipAuth := fs.Bool("skip-auth", false, "skip codex profile auth requirement (for offline/testing use)")
	nameArg, filtered := splitDyadSpawnArgs(args)
	fs.Parse(filtered)
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
	if !workspaceSet && strings.TrimSpace(settings.Dyad.Workspace) != "" {
		*workspaceHost = strings.TrimSpace(settings.Dyad.Workspace)
		workspaceSet = true
	}
	if !flagProvided(args, "configs") && strings.TrimSpace(settings.Dyad.Configs) != "" {
		*configsHost = strings.TrimSpace(settings.Dyad.Configs)
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
			printUsage("usage: si dyad spawn <name> [role] [department] [--profile <profile>]")
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
	dept := strings.TrimSpace(*deptFlag)
	if dept == "" && fs.NArg() > 1 {
		dept = fs.Arg(1)
	}
	if dept == "" && !deptProvided && isInteractiveTerminal() {
		selected, ok := promptWithDefault("Department:", role)
		if !ok {
			return
		}
		dept = strings.TrimSpace(selected)
	}
	if dept == "" {
		dept = role
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

	root := ""
	if !workspaceSet {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		*workspaceHost = cwd
	}
	if strings.TrimSpace(*workspaceHost) == "" {
		root = mustRepoRoot()
		*workspaceHost = root
	} else {
		root = *workspaceHost
	}
	if strings.TrimSpace(*configsHost) == "" {
		resolved, err := resolveConfigsHost(root)
		if err != nil {
			fatal(err)
		}
		*configsHost = resolved
	}
	if strings.TrimSpace(*forwardPorts) == "" {
		*forwardPorts = "1455-1465"
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
		Department:        dept,
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
		ForwardPorts:      *forwardPorts,
		Network:           shared.DefaultNetwork,
		DockerSocket:      *dockerSocket,
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

	actorID, criticID, err := client.EnsureDyad(ctx, opts)
	if err != nil {
		fatal(err)
	}
	if profile != nil {
		seedDyadProfileAuth(ctx, client, actorID, *profile)
		seedDyadProfileAuth(ctx, client, criticID, *profile)
	}
	if identity, ok := hostGitIdentity(); ok {
		seedGitIdentity(ctx, client, actorID, "root", "/root", identity)
		seedGitIdentity(ctx, client, criticID, "root", "/root", identity)
	}
	successf("dyad %s ready (role=%s dept=%s)", name, role, dept)
}

func cmdDyadPeek(args []string) {
	fs := flag.NewFlagSet("dyad peek", flag.ExitOnError)
	member := fs.String("member", "both", "actor, critic, or both")
	detached := fs.Bool("detached", false, "create host tmux session but do not attach")
	hostSession := fs.String("session", "", "host tmux session name (default: si-dyad-peek-<dyad>)")
	fs.Parse(args)

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
	_ = exec.Command("tmux", "kill-session", "-t", peekSession).Run()

	var first string
	switch memberVal {
	case "actor":
		first = actorCmd
	case "critic":
		first = criticCmd
	default:
		first = actorCmd
	}
	if err := exec.Command("tmux", "new-session", "-d", "-s", peekSession, "bash", "-lc", first).Run(); err != nil {
		fatal(err)
	}
	_, _ = exec.Command("tmux", "set-option", "-t", peekSession, "remain-on-exit", "off").Output()

	if memberVal == "both" {
		if err := exec.Command("tmux", "split-window", "-h", "-t", peekSession+":0", "bash", "-lc", criticCmd).Run(); err != nil {
			_ = exec.Command("tmux", "kill-session", "-t", peekSession).Run()
			fatal(err)
		}
		_, _ = exec.Command("tmux", "select-layout", "-t", peekSession, "even-horizontal").Output()
	}

	if *detached {
		successf("dyad peek session ready: %s", peekSession)
		return
	}
	if !isInteractiveTerminal() {
		fatal(errors.New("dyad peek requires an interactive terminal (or use --detached)"))
	}
	tmuxCmd := exec.Command("tmux", "attach-session", "-t", peekSession)
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr
	tmuxCmd.Stdin = os.Stdin
	if err := tmuxCmd.Run(); err != nil {
		fatal(err)
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
	if len(args) > 0 {
		printUsage("usage: si dyad list")
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
	if len(rows) == 0 {
		infof("no dyads found")
		return
	}
	printDyadRows(rows)
	_ = args
}

func cmdDyadRemove(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("remove")
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
	if err := client.RemoveDyad(context.Background(), name, true); err != nil {
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
	profile, err := resolveDyadSpawnProfile(profileArg)
	if err != nil {
		fatal(err)
	}
	if profile == nil {
		return
	}
	status := codexProfileAuthStatus(*profile)
	if !status.Exists {
		fatal(fmt.Errorf("profile %s is not logged in; run `si login %s` first", profile.ID, profile.ID))
	}
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	_ = client.RemoveDyad(context.Background(), name, true)
	cmdDyadSpawn(args)
}

func cmdDyadStatus(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("status")
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
	if actorID == "" && criticID == "" {
		fmt.Printf("%s %s\n", styleError("dyad not found:"), styleCmd(name))
		return
	}
	fmt.Printf("%s %s\n", styleHeading("dyad"), styleCmd(name))
	if actorInfo != nil {
		fmt.Printf(" %s %s (%s)\n", styleSection("actor:"), actorID[:12], styleStatus(actorInfo.State.Status))
	} else {
		fmt.Printf(" %s %s\n", styleSection("actor:"), styleError("missing"))
	}
	if criticInfo != nil {
		fmt.Printf(" %s %s (%s)\n", styleSection("critic:"), criticID[:12], styleStatus(criticInfo.State.Status))
	} else {
		fmt.Printf(" %s %s\n", styleSection("critic:"), styleError("missing"))
	}
}

func cmdDyadExec(args []string) {
	memberProvided := flagProvided(args, "member")
	fs := flag.NewFlagSet("dyad exec", flag.ExitOnError)
	member := fs.String("member", "actor", "actor or critic")
	tty := fs.Bool("tty", false, "allocate TTY")
	fs.Parse(args)
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
	id, _, err := client.ContainerByName(context.Background(), containerName)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("container not found: %s", containerName)
	}
	opts := shared.ExecOptions{TTY: tty}
	return client.Exec(context.Background(), id, cmd, opts, os.Stdin, os.Stdout, os.Stderr)
}

func cmdDyadLogs(args []string) {
	memberProvided := flagProvided(args, "member")
	dyadArg, filtered := splitDyadLogsNameAndFlags(args)
	fs := flag.NewFlagSet("dyad logs", flag.ExitOnError)
	member := fs.String("member", "critic", "actor or critic")
	tail := fs.Int("tail", 200, "lines to tail")
	fs.Parse(filtered)
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
	out, err := client.Logs(context.Background(), id, shared.LogsOptions{Tail: *tail})
	if err != nil {
		fatal(err)
	}
	fmt.Print(out)
}

func splitDyadLogsNameAndFlags(args []string) (string, []string) {
	return splitNameAndFlags(args, map[string]bool{
		"member": false,
		"tail":   false,
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
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if !os.IsNotExist(err) {
			warnf("dyad auth copy skipped: %v", err)
		}
		return
	}
	const destPath = "/root/.codex/auth.json"
	_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", "/root/.codex"}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
		warnf("dyad auth copy failed: %v", err)
		return
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
