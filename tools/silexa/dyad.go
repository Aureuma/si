package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	shared "silexa/agents/shared/docker"
)

func cmdDyad(args []string) {
	if len(args) == 0 {
		printUsage("usage: si dyad <spawn|list|remove|recreate|status|exec|logs|restart|cleanup|copy-login>")
		return
	}
	switch args[0] {
	case "spawn":
		cmdDyadSpawn(args[1:])
	case "list":
		cmdDyadList(args[1:])
	case "remove", "teardown", "destroy":
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
	case "copy-login", "codex-login-copy":
		cmdDyadCopyLogin(args[1:])
	default:
		printUnknown("dyad", args[0])
	}
}

func cmdDyadSpawn(args []string) {
	workspaceSet := flagProvided(args, "workspace")
	fs := flag.NewFlagSet("dyad spawn", flag.ExitOnError)
	roleFlag := fs.String("role", "", "dyad role")
	deptFlag := fs.String("department", "", "dyad department")
	actorImage := fs.String("actor-image", envOr("ACTOR_IMAGE", "silexa/actor:local"), "actor image")
	criticImage := fs.String("critic-image", envOr("CRITIC_IMAGE", "silexa/critic:local"), "critic image")
	codexModel := fs.String("codex-model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	codexEffortActor := fs.String("codex-effort-actor", envOr("CODEX_ACTOR_EFFORT", ""), "codex effort for actor")
	codexEffortCritic := fs.String("codex-effort-critic", envOr("CODEX_CRITIC_EFFORT", ""), "codex effort for critic")
	codexModelLow := fs.String("codex-model-low", envOr("CODEX_MODEL_LOW", ""), "codex model low")
	codexModelMedium := fs.String("codex-model-medium", envOr("CODEX_MODEL_MEDIUM", ""), "codex model medium")
	codexModelHigh := fs.String("codex-model-high", envOr("CODEX_MODEL_HIGH", ""), "codex model high")
	codexEffortLow := fs.String("codex-effort-low", envOr("CODEX_REASONING_EFFORT_LOW", ""), "codex effort low")
	codexEffortMedium := fs.String("codex-effort-medium", envOr("CODEX_REASONING_EFFORT_MEDIUM", ""), "codex effort medium")
	codexEffortHigh := fs.String("codex-effort-high", envOr("CODEX_REASONING_EFFORT_HIGH", ""), "codex effort high")
	workspaceHost := fs.String("workspace", envOr("SILEXA_WORKSPACE_HOST", ""), "host path to workspace (repo root)")
	configsHost := fs.String("configs", envOr("SILEXA_CONFIGS_HOST", ""), "host path to configs")
	forwardPorts := fs.String("forward-ports", envOr("SILEXA_DYAD_FORWARD_PORTS", ""), "actor forward ports (default 1455-1465)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		printUsage("usage: si dyad spawn <name> [role] [department]")
		return
	}
	name := fs.Arg(0)
	if err := validateSlug(name); err != nil {
		fatal(err)
	}

	role := strings.TrimSpace(*roleFlag)
	if role == "" && fs.NArg() > 1 {
		role = fs.Arg(1)
	}
	if role == "" {
		role = "generic"
	}
	dept := strings.TrimSpace(*deptFlag)
	if dept == "" && fs.NArg() > 2 {
		dept = fs.Arg(2)
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
		*configsHost = root + string(os.PathSeparator) + "configs"
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
	ctx := context.Background()

	containers, err := client.ListContainers(ctx, true, map[string]string{shared.LabelApp: shared.DyadAppLabel})
	if err != nil {
		fatal(err)
	}
	type row struct {
		Dyad   string
		Role   string
		Dept   string
		Actor  string
		Critic string
	}
	rows := map[string]*row{}
	for _, c := range containers {
		dyad := c.Labels[shared.LabelDyad]
		if dyad == "" {
			continue
		}
		item, ok := rows[dyad]
		if !ok {
			item = &row{
				Dyad: dyad,
				Role: c.Labels[shared.LabelRole],
				Dept: c.Labels[shared.LabelDept],
			}
			rows[dyad] = item
		}
		member := c.Labels[shared.LabelMember]
		state := c.State
		if member == "actor" {
			item.Actor = state
		} else if member == "critic" {
			item.Critic = state
		}
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		infof("no dyads found")
		return
	}
	widths := map[string]int{"dyad": 4, "role": 4, "dept": 4, "actor": 5, "critic": 6}
	for _, k := range keys {
		r := rows[k]
		widths["dyad"] = max(widths["dyad"], len(r.Dyad))
		widths["role"] = max(widths["role"], len(r.Role))
		widths["dept"] = max(widths["dept"], len(r.Dept))
		widths["actor"] = max(widths["actor"], len(r.Actor))
		widths["critic"] = max(widths["critic"], len(r.Critic))
	}
	fmt.Printf("%s  %s  %s  %s  %s\n",
		padRightANSI(styleHeading("DYAD"), widths["dyad"]),
		padRightANSI(styleHeading("ROLE"), widths["role"]),
		padRightANSI(styleHeading("DEPT"), widths["dept"]),
		padRightANSI(styleHeading("ACTOR"), widths["actor"]),
		padRightANSI(styleHeading("CRITIC"), widths["critic"]),
	)
	for _, k := range keys {
		r := rows[k]
		fmt.Printf("%s  %s  %s  %s  %s\n",
			padRightANSI(r.Dyad, widths["dyad"]),
			padRightANSI(r.Role, widths["role"]),
			padRightANSI(r.Dept, widths["dept"]),
			padRightANSI(styleStatus(r.Actor), widths["actor"]),
			padRightANSI(styleStatus(r.Critic), widths["critic"]),
		)
	}
	_ = args
}

func cmdDyadRemove(args []string) {
	if len(args) < 1 {
		printUsage("usage: si dyad remove <name>")
		return
	}
	name := args[0]
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
	if len(args) < 1 {
		printUsage("usage: si dyad recreate <name> [role] [department]")
		return
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
	if len(args) < 1 {
		printUsage("usage: si dyad status <name>")
		return
	}
	name := args[0]
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
	fs := flag.NewFlagSet("dyad exec", flag.ExitOnError)
	member := fs.String("member", "actor", "actor or critic")
	tty := fs.Bool("tty", false, "allocate TTY")
	fs.Parse(args)
	if fs.NArg() < 2 {
		printUsage("usage: si dyad exec <dyad> [--member actor|critic] -- <cmd...>")
		return
	}
	dyad := fs.Arg(0)
	cmd := fs.Args()[1:]
	if len(cmd) > 0 && cmd[0] == "--" {
		cmd = cmd[1:]
	}
	if err := execInDyad(dyad, *member, cmd, *tty); err != nil {
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
	fs := flag.NewFlagSet("dyad logs", flag.ExitOnError)
	member := fs.String("member", "critic", "actor or critic")
	tail := fs.Int("tail", 200, "lines to tail")
	fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si dyad logs <dyad> [--member actor|critic] [--tail N]")
		return
	}
	dyad := fs.Arg(0)
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	containerName := shared.DyadContainerName(dyad, *member)
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
	if len(args) < 1 {
		printUsage("usage: si dyad restart <name>")
		return
	}
	name := args[0]
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
	fs := flag.NewFlagSet("dyad copy-login", flag.ExitOnError)
	source := fs.String("source", envOr("SI_CODEX_SOURCE", "codex-status"), "si-codex container name or suffix")
	member := fs.String("member", "actor", "target member (actor or critic)")
	sourceHome := fs.String("source-home", "/home/si", "home dir in source container")
	targetHome := fs.String("target-home", "/root", "home dir in target container")
	fs.Parse(args)

	if fs.NArg() < 1 {
		printUsage("usage: si dyad copy-login <dyad> [--member actor|critic] [--source codex-status]")
		return
	}
	dyad := fs.Arg(0)
	if err := validateSlug(dyad); err != nil {
		fatal(err)
	}
	memberVal := strings.ToLower(strings.TrimSpace(*member))
	if memberVal == "" {
		memberVal = "actor"
	}
	if memberVal != "actor" && memberVal != "critic" {
		fatal(fmt.Errorf("invalid member %q (expected actor or critic)", memberVal))
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
