package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"golang.org/x/term"
)

const (
	codexLabelKey   = "si.component"
	codexLabelValue = "codex"
)

func cmdCodex(args []string) {
	if len(args) == 0 {
		printUsage("usage: si codex <spawn|respawn|list|ps|status|report|login|exec|logs|tail|clone|remove|stop|start>")
		return
	}
	switch args[0] {
	case "spawn":
		cmdCodexSpawn(args[1:])
	case "respawn":
		cmdCodexRespawn(args[1:])
	case "list", "ps":
		cmdCodexList(args[1:])
	case "status":
		cmdCodexStatus(args[1:])
	case "report":
		cmdCodexReport(args[1:])
	case "login":
		cmdCodexLogin(args[1:])
	case "exec":
		cmdCodexExec(args[1:])
	case "logs":
		cmdCodexLogs(args[1:])
	case "tail":
		cmdCodexTail(args[1:])
	case "clone":
		cmdCodexClone(args[1:])
	case "remove", "rm", "delete":
		cmdCodexRemove(args[1:])
	case "stop":
		cmdCodexStop(args[1:])
	case "start":
		cmdCodexStart(args[1:])
	default:
		printUnknown("codex", args[0])
	}
}

type codexSpawnFlags struct {
	image         *string
	workspaceHost *string
	networkName   *string
	repo          *string
	ghPat         *string
	cmdStr        *string
	workdir       *string
	codexVolume   *string
	ghVolume      *string
	detach        *bool
	cleanSlate    *bool
	envs          *multiFlag
	ports         *multiFlag
}

func addCodexSpawnFlags(fs *flag.FlagSet) *codexSpawnFlags {
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "silexa/si-codex:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SILEXA_WORKSPACE_HOST", ""), "host path to workspace")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	repo := fs.String("repo", "", "github repo in Org/Repo form")
	ghPat := fs.String("gh-pat", envOr("GH_PAT", envOr("GH_TOKEN", envOr("GITHUB_TOKEN", ""))), "github PAT for cloning")
	cmdStr := fs.String("cmd", "", "command to run in the container")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	codexVolume := fs.String("codex-volume", "", "override codex volume name")
	ghVolume := fs.String("gh-volume", "", "override github config volume name")
	detach := fs.Bool("detach", true, "run container in background")
	cleanSlate := fs.Bool("clean-slate", false, "skip copying host ~/.codex/config.toml into container")
	envs := multiFlag{}
	ports := multiFlag{}
	fs.Var(&envs, "env", "env var (repeatable KEY=VALUE)")
	fs.Var(&ports, "port", "port mapping (repeatable HOST:CONTAINER)")
	return &codexSpawnFlags{
		image:         image,
		workspaceHost: workspaceHost,
		networkName:   networkName,
		repo:          repo,
		ghPat:         ghPat,
		cmdStr:        cmdStr,
		workdir:       workdir,
		codexVolume:   codexVolume,
		ghVolume:      ghVolume,
		detach:        detach,
		cleanSlate:    cleanSlate,
		envs:          &envs,
		ports:         &ports,
	}
}

func cmdCodexSpawn(args []string) {
	workdirSet := flagProvided(args, "workdir")
	fs := flag.NewFlagSet("codex spawn", flag.ExitOnError)
	flags := addCodexSpawnFlags(fs)
	nameArg, filtered := splitNameAndFlags(args, codexSpawnBoolFlags())
	fs.Parse(filtered)

	name := nameArg
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		printUsage("usage: si codex spawn <name> [--repo Org/Repo] [--gh-pat TOKEN]")
		return
	}
	if err := validateSlug(name); err != nil {
		fatal(err)
	}
	containerName := codexContainerName(name)
	if strings.TrimSpace(*flags.workspaceHost) == "" {
		*flags.workspaceHost = mustRepoRoot()
	}

	workspaceTarget := "/workspace"
	if target, ok := shared.InferWorkspaceTarget(*flags.workspaceHost); ok {
		workspaceTarget = target
	}
	if !workdirSet && *flags.workdir == "/workspace" {
		*flags.workdir = workspaceTarget
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	if strings.TrimSpace(*flags.networkName) != "" {
		_, _ = client.EnsureNetwork(ctx, *flags.networkName, map[string]string{codexLabelKey: codexLabelValue})
	}

	codexVol := strings.TrimSpace(*flags.codexVolume)
	if codexVol == "" {
		codexVol = "si-codex-" + name
	}
	ghVol := strings.TrimSpace(*flags.ghVolume)
	if ghVol == "" {
		ghVol = "si-gh-" + name
	}
	_, _ = client.EnsureVolume(ctx, codexVol, map[string]string{codexLabelKey: codexLabelValue})
	_, _ = client.EnsureVolume(ctx, ghVol, map[string]string{codexLabelKey: codexLabelValue})

	labels := map[string]string{
		codexLabelKey: codexLabelValue,
		"si.name":     name,
	}

	env := []string{
		"HOME=/home/si",
		"CODEX_HOME=/home/si/.codex",
	}
	if strings.TrimSpace(*flags.repo) != "" {
		env = append(env, "SI_REPO="+strings.TrimSpace(*flags.repo))
	}
	if strings.TrimSpace(*flags.ghPat) != "" {
		env = append(env, "SI_GH_PAT="+strings.TrimSpace(*flags.ghPat))
		env = append(env, "GH_TOKEN="+strings.TrimSpace(*flags.ghPat))
		env = append(env, "GITHUB_TOKEN="+strings.TrimSpace(*flags.ghPat))
	}
	env = append(env, (*flags.envs)...)

	cmd := []string{"bash", "-lc", "sleep infinity"}
	if strings.TrimSpace(*flags.cmdStr) != "" {
		cmd = []string{"bash", "-lc", *flags.cmdStr}
	}

	exposed, bindings, err := parsePortBindings(*flags.ports)
	if err != nil {
		fatal(err)
	}

	cfg := &container.Config{
		Image:        strings.TrimSpace(*flags.image),
		Env:          filterEnv(env),
		Labels:       labels,
		ExposedPorts: exposed,
		WorkingDir:   *flags.workdir,
		Cmd:          cmd,
	}
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: codexVol, Target: "/home/si/.codex"},
			{Type: mount.TypeVolume, Source: ghVol, Target: "/home/si/.config/gh"},
			{Type: mount.TypeBind, Source: *flags.workspaceHost, Target: workspaceTarget},
		},
		PortBindings: bindings,
	}
	netCfg := &network.NetworkingConfig{}
	if strings.TrimSpace(*flags.networkName) != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				*flags.networkName: {Aliases: []string{containerName}},
			},
		}
	}

	existingID, info, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if existingID != "" {
		if info != nil && info.State != nil && !info.State.Running {
			if err := client.StartContainer(ctx, existingID); err != nil {
				fatal(err)
			}
		}
		if !*flags.cleanSlate {
			if identity, ok := hostGitIdentity(); ok {
				seedGitIdentity(ctx, client, existingID, "si", "/home/si", identity)
			}
		}
		infof("codex container %s already exists", containerName)
		return
	}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, containerName)
	if err != nil {
		fatal(err)
	}
	if err := client.StartContainer(ctx, id); err != nil {
		fatal(err)
	}
	seedCodexConfig(ctx, client, id, *flags.cleanSlate)
	if !*flags.cleanSlate {
		if identity, ok := hostGitIdentity(); ok {
			seedGitIdentity(ctx, client, id, "si", "/home/si", identity)
		}
	}
	successf("codex container %s started", containerName)
	if !*flags.detach {
		_ = execDockerCLI("attach", containerName)
	}
}

func cmdCodexRespawn(args []string) {
	fs := flag.NewFlagSet("codex respawn", flag.ExitOnError)
	addCodexSpawnFlags(fs)
	removeVolumes := fs.Bool("volumes", false, "remove codex/gh volumes too")
	nameArg, filtered := splitNameAndFlags(args, codexRespawnBoolFlags())
	_ = fs.Parse(filtered)
	if nameArg == "" {
		printUsage("usage: si codex respawn <name> [--volumes] [spawn flags]")
		return
	}
	name := nameArg
	removeArgs := []string{name}
	if *removeVolumes {
		removeArgs = append([]string{"--volumes"}, removeArgs...)
	}
	cmdCodexRemove(removeArgs)
	spawnArgs := append(stripFlag(filtered, "volumes"), name)
	cmdCodexSpawn(spawnArgs)
}

func cmdCodexList(args []string) {
	fs := flag.NewFlagSet("codex list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	containers, err := client.ListContainers(ctx, true, map[string]string{codexLabelKey: codexLabelValue})
	if err != nil {
		fatal(err)
	}
	if len(containers) == 0 {
		infof("no codex containers found")
		return
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Names[0] < containers[j].Names[0]
	})
	if *jsonOut {
		type codexItem struct {
			Name      string            `json:"name"`
			State     string            `json:"state"`
			Status    string            `json:"status"`
			Image     string            `json:"image"`
			CreatedAt string            `json:"created_at"`
			Labels    map[string]string `json:"labels,omitempty"`
		}
		items := make([]codexItem, 0, len(containers))
		for _, c := range containers {
			name := strings.TrimPrefix(c.Names[0], "/")
			created := time.Unix(c.Created, 0).UTC().Format(time.RFC3339)
			items = append(items, codexItem{
				Name:      name,
				State:     c.State,
				Status:    c.Status,
				Image:     c.Image,
				CreatedAt: created,
				Labels:    c.Labels,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s %s\n",
		padRightANSI(styleHeading("CONTAINER"), 28),
		padRightANSI(styleHeading("STATE"), 10),
		padRightANSI(styleHeading("IMAGE"), 20),
	)
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		fmt.Printf("%s %s %s\n",
			padRightANSI(name, 28),
			padRightANSI(styleStatus(c.State), 10),
			padRightANSI(c.Image, 20),
		)
	}
}

func cmdCodexExec(args []string) {
	fs := flag.NewFlagSet("codex exec", flag.ExitOnError)
	oneOff := fs.Bool("one-off", false, "run a one-off codex exec in an isolated container")
	promptFlag := fs.String("prompt", "", "prompt to execute (one-off mode)")
	outputOnly := fs.Bool("output-only", false, "print only the Codex response (one-off mode)")
	noMcp := fs.Bool("no-mcp", false, "disable MCP servers (one-off mode)")
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "silexa/si-codex:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SILEXA_WORKSPACE_HOST", ""), "host path to workspace")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	codexVolume := fs.String("codex-volume", envOr("SI_CODEX_EXEC_VOLUME", ""), "codex volume name")
	ghVolume := fs.String("gh-volume", "", "gh config volume name")
	model := fs.String("model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	effort := fs.String("effort", envOr("CODEX_REASONING_EFFORT", "medium"), "codex reasoning effort")
	keep := fs.Bool("keep", false, "keep the one-off container after execution")
	envs := multiFlag{}
	fs.Var(&envs, "env", "env var (repeatable KEY=VALUE)")
	_ = fs.Parse(args)

	prompt := strings.TrimSpace(*promptFlag)
	rest := fs.Args()
	if prompt == "" && len(rest) == 1 && !isValidSlug(rest[0]) {
		prompt = rest[0]
		rest = nil
	}

	if *oneOff || prompt != "" || *outputOnly || *noMcp {
		if prompt == "" && len(rest) > 0 {
			prompt = strings.Join(rest, " ")
		}
		if strings.TrimSpace(prompt) == "" {
			printUsage("usage: si codex exec --prompt \"...\" [--output-only] [--no-mcp]")
			fmt.Println(styleDim("   or: si codex exec \"...\" [--output-only] [--no-mcp]"))
			return
		}
		opts := codexExecOneOffOptions{
			Prompt:        prompt,
			Image:         strings.TrimSpace(*image),
			WorkspaceHost: strings.TrimSpace(*workspaceHost),
			Workdir:       strings.TrimSpace(*workdir),
			Network:       strings.TrimSpace(*networkName),
			CodexVolume:   strings.TrimSpace(*codexVolume),
			GHVolume:      strings.TrimSpace(*ghVolume),
			Env:           envs,
			Model:         strings.TrimSpace(*model),
			Effort:        strings.TrimSpace(*effort),
			DisableMCP:    *noMcp,
			OutputOnly:    *outputOnly,
			KeepContainer: *keep,
		}
		if err := runCodexExecOneOff(opts); err != nil {
			fatal(err)
		}
		return
	}

	if len(rest) < 1 {
		printUsage("usage: si codex exec <name> [--] <command>")
		fmt.Println(styleDim("   or: si codex exec --prompt \"...\" [--output-only] [--no-mcp]"))
		return
	}
	name := rest[0]
	containerName := codexContainerName(name)
	cmd := rest[1:]
	if len(cmd) == 0 {
		cmd = []string{"bash", "-lc", `printf '\033]0;%s\007' "${SI_TERM_TITLE:-}"; exec bash`}
	}
	execArgs := []string{"exec"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		execArgs = append(execArgs, "-it")
	} else {
		execArgs = append(execArgs, "-i")
	}
	execArgs = append(execArgs, "-e", "SI_TERM_TITLE="+name)
	execArgs = append(execArgs, containerName)
	execArgs = append(execArgs, cmd...)
	if err := execDockerCLI(execArgs...); err != nil {
		fatal(err)
	}
}

func cmdCodexLogin(args []string) {
	fs := flag.NewFlagSet("codex login", flag.ExitOnError)
	deviceAuth := fs.Bool("device-auth", true, "use device auth flow")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si codex login <name>")
		return
	}
	name := fs.Arg(0)
	containerName := codexContainerName(name)

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("codex container %s not found", containerName))
	}

	execArgs := []string{"exec"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		execArgs = append(execArgs, "-it")
	} else {
		execArgs = append(execArgs, "-i")
	}
	execArgs = append(execArgs,
		"-e", "HOME=/home/si",
		"-e", "CODEX_HOME=/home/si/.codex",
		"-e", "TERM=xterm-256color",
		"-e", "COLORTERM=truecolor",
		"-e", "CLICOLOR_FORCE=1",
		"-e", "FORCE_COLOR=1",
	)
	execArgs = append(execArgs, containerName, "codex", "login")
	if *deviceAuth {
		execArgs = append(execArgs, "--device-auth")
	}
	if err := execDockerCLI(execArgs...); err != nil {
		fatal(err)
	}
}

func cmdCodexLogs(args []string) {
	if len(args) < 1 {
		printUsage("usage: si codex logs <name> [--tail N]")
		return
	}
	name := args[0]
	tail := parseTail(args[1:], "200")
	containerName := codexContainerName(name)
	if err := execDockerCLI("logs", "--tail", tail, containerName); err != nil {
		fatal(err)
	}
}

func cmdCodexTail(args []string) {
	if len(args) < 1 {
		printUsage("usage: si codex tail <name> [--tail N]")
		return
	}
	name := args[0]
	tail := parseTail(args[1:], "200")
	containerName := codexContainerName(name)
	if err := execDockerCLI("logs", "-f", "--tail", tail, containerName); err != nil {
		fatal(err)
	}
}

func cmdCodexClone(args []string) {
	if len(args) < 2 {
		printUsage("usage: si codex clone <name> <Org/Repo> [--gh-pat TOKEN]")
		return
	}
	name := args[0]
	repo := strings.TrimSpace(args[1])
	fs := flag.NewFlagSet("codex clone", flag.ExitOnError)
	ghPat := fs.String("gh-pat", envOr("GH_PAT", envOr("GH_TOKEN", envOr("GITHUB_TOKEN", ""))), "github PAT for cloning")
	_ = fs.Parse(args[2:])

	if repo == "" {
		fatal(fmt.Errorf("repo required"))
	}
	containerName := codexContainerName(name)
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("codex container %s not found", containerName))
	}

	execArgs := []string{"exec"}
	execArgs = append(execArgs, "-e", "SI_REPO="+repo)
	if strings.TrimSpace(*ghPat) != "" {
		execArgs = append(execArgs, "-e", "SI_GH_PAT="+strings.TrimSpace(*ghPat))
		execArgs = append(execArgs, "-e", "GH_TOKEN="+strings.TrimSpace(*ghPat))
		execArgs = append(execArgs, "-e", "GITHUB_TOKEN="+strings.TrimSpace(*ghPat))
	}
	execArgs = append(execArgs, containerName, "/usr/local/bin/si-entrypoint", "bash", "-lc", "true")
	if err := execDockerCLI(execArgs...); err != nil {
		fatal(err)
	}
	successf("repo %s cloned in %s", repo, containerName)
}

func cmdCodexRemove(args []string) {
	fs := flag.NewFlagSet("codex remove", flag.ExitOnError)
	removeVolumes := fs.Bool("volumes", false, "remove codex/gh volumes too")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si codex remove <name> [--volumes]")
		return
	}
	name := fs.Arg(0)
	containerName := codexContainerName(name)
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fmt.Printf("%s %s\n", styleError("codex container not found:"), containerName)
		return
	}
	if err := client.RemoveContainer(ctx, id, true); err != nil {
		fatal(err)
	}
	if *removeVolumes {
		codexVol := "si-codex-" + name
		ghVol := "si-gh-" + name
		if err := client.RemoveVolume(ctx, codexVol, true); err != nil {
			warnf("codex volume remove failed: %v", err)
		}
		if err := client.RemoveVolume(ctx, ghVol, true); err != nil {
			warnf("gh volume remove failed: %v", err)
		}
	}
	successf("codex container %s removed", containerName)
}

func cmdCodexStop(args []string) {
	if len(args) < 1 {
		printUsage("usage: si codex stop <name>")
		return
	}
	name := args[0]
	containerName := codexContainerName(name)
	if err := execDockerCLI("stop", containerName); err != nil {
		fatal(err)
	}
}

func cmdCodexStart(args []string) {
	if len(args) < 1 {
		printUsage("usage: si codex start <name>")
		return
	}
	name := args[0]
	containerName := codexContainerName(name)
	if err := execDockerCLI("start", containerName); err != nil {
		fatal(err)
	}
}

func codexContainerName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "si-codex-") {
		return name
	}
	return "si-codex-" + name
}

func seedCodexConfig(ctx context.Context, client *shared.Client, containerID string, cleanSlate bool) {
	if cleanSlate {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return
	}
	hostPath := filepath.Join(home, ".codex", "config.toml")
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		warnf("codex config copy skipped: %v", err)
		return
	}
	const destPath = "/home/si/.codex/config.toml"
	if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
		warnf("codex config copy failed: %v", err)
		return
	}
	_ = client.Exec(ctx, containerID, []string{"chown", "si:si", destPath}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
}

func flagProvided(args []string, name string) bool {
	short := "-" + name
	long := "--" + name
	shortEq := short + "="
	longEq := long + "="
	for _, arg := range args {
		if arg == short || arg == long || strings.HasPrefix(arg, shortEq) || strings.HasPrefix(arg, longEq) {
			return true
		}
	}
	return false
}

func codexSpawnBoolFlags() map[string]bool {
	return map[string]bool{
		"detach":      true,
		"clean-slate": true,
	}
}

func codexRespawnBoolFlags() map[string]bool {
	flags := codexSpawnBoolFlags()
	flags["volumes"] = true
	return flags
}

func splitNameAndFlags(args []string, boolFlags map[string]bool) (string, []string) {
	name := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if name == "" {
				name = arg
				continue
			}
			out = append(out, arg)
			continue
		}
		out = append(out, arg)
		flagName := strings.TrimLeft(arg, "-")
		if idx := strings.Index(flagName, "="); idx != -1 {
			flagName = flagName[:idx]
			continue
		}
		if boolFlags[flagName] {
			continue
		}
		if i+1 < len(args) {
			out = append(out, args[i+1])
			i++
		}
	}
	return name, out
}

func stripFlag(args []string, name string) []string {
	short := "-" + name
	long := "--" + name
	shortEq := short + "="
	longEq := long + "="
	out := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case arg == short, arg == long:
			continue
		case strings.HasPrefix(arg, shortEq), strings.HasPrefix(arg, longEq):
			continue
		default:
			out = append(out, arg)
		}
	}
	return out
}

func parsePortBindings(values []string) (nat.PortSet, map[nat.Port][]nat.PortBinding, error) {
	exposed := nat.PortSet{}
	bindings := map[nat.Port][]nat.PortBinding{}
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host, containerPort, err := splitPortMapping(raw)
		if err != nil {
			return nil, nil, err
		}
		port := nat.Port(containerPort + "/tcp")
		exposed[port] = struct{}{}
		bindings[port] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: host}}
	}
	return exposed, bindings, nil
}

func splitPortMapping(raw string) (string, string, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid port mapping: %s", raw)
	}
	hostPort := strings.TrimSpace(parts[0])
	containerPort := strings.TrimSpace(parts[1])
	if hostPort == "" || containerPort == "" {
		return "", "", fmt.Errorf("invalid port mapping: %s", raw)
	}
	if _, err := strconv.Atoi(containerPort); err != nil {
		return "", "", fmt.Errorf("invalid container port: %s", containerPort)
	}
	return hostPort, containerPort, nil
}

func parseTail(args []string, def string) string {
	tail := def
	if len(args) > 1 && args[0] == "--tail" {
		tail = args[1]
	} else if len(args) > 0 && strings.HasPrefix(args[0], "--tail=") {
		tail = strings.TrimPrefix(args[0], "--tail=")
	}
	return tail
}
