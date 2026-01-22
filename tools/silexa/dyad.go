package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"
)

func cmdDyad(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si dyad <spawn|list|remove|recreate|status|exec|logs|restart|register|cleanup>")
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
	case "register":
		cmdDyadRegister(args[1:])
	case "cleanup":
		cmdDyadCleanup(args[1:])
	default:
		fmt.Println("unknown dyad command:", args[0])
	}
}

func cmdDyadSpawn(args []string) {
	fs := flag.NewFlagSet("dyad spawn", flag.ExitOnError)
	roleFlag := fs.String("role", "", "dyad role")
	deptFlag := fs.String("department", "", "dyad department")
	actorImage := fs.String("actor-image", envOr("ACTOR_IMAGE", "silexa/actor:local"), "actor image")
	criticImage := fs.String("critic-image", envOr("CRITIC_IMAGE", "silexa/critic:local"), "critic image")
	managerURL := fs.String("manager-url", envOr("MANAGER_URL", "http://localhost:9090"), "manager URL for registration")
	managerServiceURL := fs.String("manager-service-url", envOr("MANAGER_SERVICE_URL", "http://silexa-manager:9090"), "manager URL for containers")
	telegramURL := fs.String("telegram-url", envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"), "telegram notify URL for containers")
	telegramChat := fs.String("telegram-chat-id", envOr("TELEGRAM_CHAT_ID", ""), "telegram chat id for containers")
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
	approverToken := fs.String("approver-token", envOr("CREDENTIALS_APPROVER_TOKEN", ""), "credentials approver token (silexa-credentials)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("usage: si dyad spawn <name> [role] [department]")
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

	if strings.TrimSpace(*approverToken) == "" && name == "silexa-credentials" {
		if token, ok, err := readFileTrim(root + string(os.PathSeparator) + "secrets" + string(os.PathSeparator) + "credentials_mcp_token"); err == nil && ok {
			*approverToken = token
		}
	}

	if err := registerDyad(*managerURL, name, role, dept); err != nil {
		fatal(err)
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
		ManagerURL:        *managerServiceURL,
		TelegramURL:       *telegramURL,
		TelegramChatID:    *telegramChat,
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
		ApproverToken:     *approverToken,
		Network:           shared.DefaultNetwork,
	}

	_, _, err = client.EnsureDyad(ctx, opts)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("dyad %s ready (role=%s dept=%s)\n", name, role, dept)
}

func defaultEffort(role string) (string, string) {
	switch role {
	case "infra":
		return "xhigh", "xhigh"
	case "research":
		return "high", "high"
	case "program_manager", "pm":
		return "high", "xhigh"
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
		fmt.Println("no dyads found")
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
	fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s\n",
		widths["dyad"], "DYAD",
		widths["role"], "ROLE",
		widths["dept"], "DEPT",
		widths["actor"], "ACTOR",
		widths["critic"], "CRITIC",
	)
	for _, k := range keys {
		r := rows[k]
		fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s\n",
			widths["dyad"], r.Dyad,
			widths["role"], r.Role,
			widths["dept"], r.Dept,
			widths["actor"], r.Actor,
			widths["critic"], r.Critic,
		)
	}
	_ = args
}

func cmdDyadRemove(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si dyad remove <name>")
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
	fmt.Printf("dyad %s removed\n", name)
}

func cmdDyadRecreate(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si dyad recreate <name> [role] [department]")
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
		fmt.Println("usage: si dyad status <name>")
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
		fmt.Printf("dyad %s not found\n", name)
		return
	}
	fmt.Printf("dyad %s\n", name)
	if actorInfo != nil {
		fmt.Printf(" actor: %s (%s)\n", actorID[:12], actorInfo.State.Status)
	} else {
		fmt.Printf(" actor: missing\n")
	}
	if criticInfo != nil {
		fmt.Printf(" critic: %s (%s)\n", criticID[:12], criticInfo.State.Status)
	} else {
		fmt.Printf(" critic: missing\n")
	}
}

func cmdDyadExec(args []string) {
	fs := flag.NewFlagSet("dyad exec", flag.ExitOnError)
	member := fs.String("member", "actor", "actor or critic")
	tty := fs.Bool("tty", false, "allocate TTY")
	fs.Parse(args)
	if fs.NArg() < 2 {
		fmt.Println("usage: si dyad exec <dyad> [--member actor|critic] -- <cmd...>")
		return
	}
	dyad := fs.Arg(0)
	cmd := fs.Args()[1:]
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
		fmt.Println("usage: si dyad logs <dyad> [--member actor|critic] [--tail N]")
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
		fmt.Println("usage: si dyad restart <name>")
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
	fmt.Printf("dyad %s restarted\n", name)
}

func cmdDyadRegister(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si dyad register <name> [role] [department]")
		return
	}
	name := args[0]
	role := "generic"
	dept := ""
	if len(args) > 1 {
		role = args[1]
	}
	if len(args) > 2 {
		dept = args[2]
	}
	if dept == "" {
		dept = role
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	if err := registerDyad(managerURL, name, role, dept); err != nil {
		fatal(err)
	}
	fmt.Printf("registered dyad %s (role=%s dept=%s)\n", name, role, dept)
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
	fmt.Printf("removed %d stopped dyad containers\n", removed)
	_ = args
}

func registerDyad(managerURL, name, role, dept string) error {
	if err := validateSlug(name); err != nil {
		return err
	}
	if err := validateSlug(role); err != nil {
		return err
	}
	if err := validateSlug(dept); err != nil {
		return err
	}
	if strings.TrimSpace(managerURL) == "" {
		return errors.New("MANAGER_URL required")
	}
	payload := map[string]interface{}{
		"dyad":       name,
		"department": dept,
		"role":       role,
		"available":  true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return postJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyads", payload, nil)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func spawnDyadFromEnv(name, role, dept string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("dyad name required")
	}
	if role == "" {
		role = "generic"
	}
	if dept == "" {
		dept = role
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	managerServiceURL := envOr("MANAGER_SERVICE_URL", "http://silexa-manager:9090")
	if err := registerDyad(managerURL, name, role, dept); err != nil {
		return err
	}
	actorImage := envOr("ACTOR_IMAGE", "silexa/actor:local")
	criticImage := envOr("CRITIC_IMAGE", "silexa/critic:local")
	codexModel := envOr("CODEX_MODEL", "gpt-5.2-codex")
	codexEffortActor := envOr("CODEX_ACTOR_EFFORT", "")
	codexEffortCritic := envOr("CODEX_CRITIC_EFFORT", "")
	if codexEffortActor == "" || codexEffortCritic == "" {
		actorEffort, criticEffort := defaultEffort(strings.ToLower(role))
		if codexEffortActor == "" {
			codexEffortActor = actorEffort
		}
		if codexEffortCritic == "" {
			codexEffortCritic = criticEffort
		}
	}
	root := mustRepoRoot()
	workspaceHost := envOr("SILEXA_WORKSPACE_HOST", root)
	configsHost := envOr("SILEXA_CONFIGS_HOST", filepath.Join(root, "configs"))
	forwardPorts := envOr("SILEXA_DYAD_FORWARD_PORTS", "1455-1465")
	approverToken := envOr("CREDENTIALS_APPROVER_TOKEN", "")
	if approverToken == "" && name == "silexa-credentials" {
		if token, ok, err := readFileTrim(filepath.Join(root, "secrets", "credentials_mcp_token")); err == nil && ok {
			approverToken = token
		}
	}
	telegramURL := envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify")
	telegramChat := envOr("TELEGRAM_CHAT_ID", "")

	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()
	opts := shared.DyadOptions{
		Dyad:              name,
		Role:              role,
		Department:        dept,
		ActorImage:        actorImage,
		CriticImage:       criticImage,
		ManagerURL:        managerServiceURL,
		TelegramURL:       telegramURL,
		TelegramChatID:    telegramChat,
		CodexModel:        codexModel,
		CodexEffortActor:  codexEffortActor,
		CodexEffortCritic: codexEffortCritic,
		CodexModelLow:     envOr("CODEX_MODEL_LOW", ""),
		CodexModelMedium:  envOr("CODEX_MODEL_MEDIUM", ""),
		CodexModelHigh:    envOr("CODEX_MODEL_HIGH", ""),
		CodexEffortLow:    envOr("CODEX_REASONING_EFFORT_LOW", ""),
		CodexEffortMedium: envOr("CODEX_REASONING_EFFORT_MEDIUM", ""),
		CodexEffortHigh:   envOr("CODEX_REASONING_EFFORT_HIGH", ""),
		WorkspaceHost:     workspaceHost,
		ConfigsHost:       configsHost,
		ForwardPorts:      forwardPorts,
		ApproverToken:     approverToken,
		Network:           shared.DefaultNetwork,
	}
	_, _, err = client.EnsureDyad(context.Background(), opts)
	return err
}
