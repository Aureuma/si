package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	shared "si/agents/shared/docker"
)

func cmdDyad(args []string) {
	if len(args) == 0 {
		if !isInteractiveTerminal() {
			printUsage("usage: si dyad <spawn|list|remove|recreate|status|exec|run|logs|restart|cleanup|copy-login|login>")
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
	case "exec":
		cmdDyadExec(args[1:])
	case "logs":
		cmdDyadLogs(args[1:])
	case "restart":
		cmdDyadRestart(args[1:])
	case "cleanup":
		cmdDyadCleanup(args[1:])
	case "copy-login":
		cmdDyadCopyLogin(args[1:])
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
			printUsage("usage: si dyad spawn <name> [role] [department]")
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
	}

	actorID, criticID, err := client.EnsureDyad(ctx, opts)
	if err != nil {
		fatal(err)
	}
	if identity, ok := hostGitIdentity(); ok {
		seedGitIdentity(ctx, client, actorID, "root", "/root", identity)
		seedGitIdentity(ctx, client, criticID, "root", "/root", identity)
	}
	successf("dyad %s ready (role=%s dept=%s)", name, role, dept)
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

func cmdDyadList(args []string) {
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
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad remove <name>")
			}
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
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad recreate <name> [role] [department]")
			}
			return
		}
		args = append([]string{selected}, args...)
	}
	name := args[0]
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
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad status <name>")
			}
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
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad exec [--member actor|critic] [--tty] <dyad> -- <cmd...>")
			}
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
	fs := flag.NewFlagSet("dyad logs", flag.ExitOnError)
	member := fs.String("member", "critic", "actor or critic")
	tail := fs.Int("tail", 200, "lines to tail")
	fs.Parse(args)
	dyad := ""
	if fs.NArg() > 0 {
		dyad = strings.TrimSpace(fs.Arg(0))
	}
	if dyad == "" {
		selected, ok := selectDyadName("logs")
		if !ok {
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad logs [--member actor|critic] [--tail N] <dyad>")
			}
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

func cmdDyadRestart(args []string) {
	name := ""
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if name == "" {
		selected, ok := selectDyadName("restart")
		if !ok {
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad restart <name>")
			}
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

func cmdDyadCleanup(args []string) {
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
	_ = args
}

func cmdDyadCopyLogin(args []string) {
	sourceProvided := flagProvided(args, "source")
	memberProvided := flagProvided(args, "member")
	fs := flag.NewFlagSet("dyad copy-login", flag.ExitOnError)
	source := fs.String("source", envOr("SI_CODEX_SOURCE", "codex-status"), "si-codex container name or suffix")
	member := fs.String("member", "actor", "target member (actor or critic)")
	sourceHome := fs.String("source-home", "/home/si", "home dir in source container")
	targetHome := fs.String("target-home", "/root", "home dir in target container")
	fs.Parse(args)

	dyad := ""
	if fs.NArg() > 0 {
		dyad = strings.TrimSpace(fs.Arg(0))
	}
	if dyad == "" {
		selected, ok := selectDyadName("copy-login")
		if !ok {
			if !isInteractiveTerminal() {
				printUsage("usage: si dyad copy-login [--member actor|critic] [--source codex-status] <dyad>")
			}
			return
		}
		dyad = selected
	}
	if err := validateSlug(dyad); err != nil {
		fatal(err)
	}
	memberVal, err := normalizeDyadMember(*member, "actor")
	if err != nil {
		fatal(err)
	}
	if !memberProvided && isInteractiveTerminal() {
		selected, ok := selectDyadMember("copy-login", memberVal)
		if !ok {
			return
		}
		memberVal = selected
	}
	if !sourceProvided && isInteractiveTerminal() {
		selected, ok := selectCodexContainer("dyad copy-login", true)
		if !ok {
			return
		}
		*source = selected
	}
	sourceName := codexContainerName(strings.TrimSpace(*source))
	if sourceName == "" {
		fatal(errors.New("source container required"))
	}
	targetName := shared.DyadContainerName(dyad, memberVal)
	if targetName == "" {
		fatal(errors.New("target container required"))
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	if id, _, err := client.ContainerByName(ctx, sourceName); err != nil || id == "" {
		if err != nil {
			fatal(err)
		}
		fatal(fmt.Errorf("source container not found: %s", sourceName))
	}
	if id, _, err := client.ContainerByName(ctx, targetName); err != nil || id == "" {
		if err != nil {
			fatal(err)
		}
		fatal(fmt.Errorf("target container not found: %s", targetName))
	}

	pipeline := fmt.Sprintf(
		"docker exec %s tar -C %s -cf - .codex | docker exec -i %s tar -C %s -xf -",
		shellQuote(sourceName),
		shellQuote(*sourceHome),
		shellQuote(targetName),
		shellQuote(*targetHome),
	)
	cmd := exec.Command("bash", "-lc", pipeline)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
	successf("copied codex login from %s to %s (%s)", sourceName, targetName, memberVal)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
