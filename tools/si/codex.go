package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	shared "si/agents/shared/docker"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"golang.org/x/term"
)

const (
	codexLabelKey          = "si.component"
	codexLabelValue        = "codex"
	codexTmuxSessionPrefix = "si-codex-pane-"
	codexTmuxCmdShaOpt     = "@si_codex_cmd_sha"
	codexTmuxHostCwdOpt    = "@si_codex_host_cwd"
	codexBrowserMCPName    = "si_browser"
)

func dispatchCodexCommand(cmd string, args []string) bool {
	maybeAutoRepairWarmupScheduler(cmd)

	switch cmd {
	case "spawn":
		cmdCodexSpawn(args)
	case "respawn":
		cmdCodexRespawn(args)
	case "list", "ps":
		cmdCodexList(args)
	case "status":
		cmdCodexStatus(args)
	case "report":
		cmdCodexReport(args)
	case "login":
		cmdCodexLogin(args)
	case "logout":
		cmdCodexLogout(args)
	case "swap":
		cmdCodexSwap(args)
	case "exec", "run":
		cmdCodexExec(args)
	case "logs":
		cmdCodexLogs(args)
	case "tail":
		cmdCodexTail(args)
	case "clone":
		cmdCodexClone(args)
	case "remove", "rm", "delete":
		cmdCodexRemove(args)
	case "stop":
		cmdCodexStop(args)
	case "start":
		cmdCodexStart(args)
	case "warmup":
		cmdWarmup(args)
	default:
		return false
	}
	return true
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
	skillsVolume  *string
	ghVolume      *string
	dockerSocket  *bool
	profile       *string
	detach        *bool
	cleanSlate    *bool
	envs          *multiFlag
	ports         *multiFlag
}

func addCodexSpawnFlags(fs *flag.FlagSet) *codexSpawnFlags {
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "aureuma/si:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	repo := fs.String("repo", "", "github repo in Org/Repo form")
	ghPat := fs.String("gh-pat", envOr("GH_PAT", envOr("GH_TOKEN", envOr("GITHUB_TOKEN", ""))), "github PAT for cloning")
	cmdStr := fs.String("cmd", "", "command to run in the container")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	codexVolume := fs.String("codex-volume", "", "override codex volume name")
	skillsVolume := fs.String("skills-volume", "", "override shared codex skills volume name")
	ghVolume := fs.String("gh-volume", "", "override github config volume name")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in the container")
	profile := fs.String("profile", "", "codex profile name/email")
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
		skillsVolume:  skillsVolume,
		ghVolume:      ghVolume,
		dockerSocket:  dockerSocket,
		profile:       profile,
		detach:        detach,
		cleanSlate:    cleanSlate,
		envs:          &envs,
		ports:         &ports,
	}
}

func cmdCodexSpawn(args []string) {
	workdirSet := flagProvided(args, "workdir")
	workspaceSet := flagProvided(args, "workspace")
	explicitProfile := flagProvided(args, "profile")
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	fs := flag.NewFlagSet("spawn", flag.ExitOnError)
	flags := addCodexSpawnFlags(fs)
	nameArg, filtered := splitNameAndFlags(args, codexSpawnBoolFlags())
	if err := fs.Parse(filtered); err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	requiredVaultFile := vaultContainerEnvFileMountPath(settings)
	defaultProfileKey := codexDefaultProfileKey(settings)

	if !flagProvided(args, "image") && strings.TrimSpace(settings.Codex.Image) != "" {
		*flags.image = strings.TrimSpace(settings.Codex.Image)
	}
	if !workspaceSet && strings.TrimSpace(settings.Codex.Workspace) != "" {
		*flags.workspaceHost = strings.TrimSpace(settings.Codex.Workspace)
		workspaceSet = true
	}
	if !flagProvided(args, "network") && strings.TrimSpace(settings.Codex.Network) != "" {
		*flags.networkName = strings.TrimSpace(settings.Codex.Network)
	}
	if !flagProvided(args, "repo") && strings.TrimSpace(settings.Codex.Repo) != "" {
		*flags.repo = strings.TrimSpace(settings.Codex.Repo)
	}
	if !flagProvided(args, "docker-socket") && settings.Codex.DockerSocket != nil {
		*flags.dockerSocket = *settings.Codex.DockerSocket
	}
	if !flagProvided(args, "gh-pat") && strings.TrimSpace(settings.Codex.GHPAT) != "" {
		*flags.ghPat = strings.TrimSpace(settings.Codex.GHPAT)
	}
	if !workdirSet && strings.TrimSpace(settings.Codex.Workdir) != "" {
		*flags.workdir = strings.TrimSpace(settings.Codex.Workdir)
		workdirSet = true
	}
	if !flagProvided(args, "codex-volume") && strings.TrimSpace(settings.Codex.CodexVolume) != "" {
		*flags.codexVolume = strings.TrimSpace(settings.Codex.CodexVolume)
	}
	if !flagProvided(args, "skills-volume") && strings.TrimSpace(settings.Codex.SkillsVolume) != "" {
		*flags.skillsVolume = strings.TrimSpace(settings.Codex.SkillsVolume)
	}
	if !flagProvided(args, "gh-volume") && strings.TrimSpace(settings.Codex.GHVolume) != "" {
		*flags.ghVolume = strings.TrimSpace(settings.Codex.GHVolume)
	}
	if !flagProvided(args, "detach") && settings.Codex.Detach != nil {
		*flags.detach = *settings.Codex.Detach
	}
	if !flagProvided(args, "clean-slate") && settings.Codex.CleanSlate != nil {
		*flags.cleanSlate = *settings.Codex.CleanSlate
	}

	name := strings.TrimSpace(nameArg)
	if name == "" && fs.NArg() > 0 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	if !explicitProfile && strings.TrimSpace(*flags.profile) == "" && defaultProfileKey != "" && !interactive {
		*flags.profile = defaultProfileKey
		explicitProfile = true
	}
	if name == "" && strings.TrimSpace(*flags.profile) != "" {
		selected, err := requireCodexProfile(*flags.profile)
		if err != nil {
			fatal(err)
		}
		*flags.profile = selected.ID
		explicitProfile = true
		name = selected.ID
	}
	if name == "" {
		if !interactive {
			printUsage("usage: si spawn [name] [--profile <profile>] [--repo Org/Repo] [--gh-pat TOKEN]")
			return
		}
		if !explicitProfile && len(codexProfileSummaries()) > 0 {
			selected, ok := selectCodexProfile("spawn", defaultProfileKey)
			if !ok {
				return
			}
			*flags.profile = selected.ID
			explicitProfile = true
			name = selected.ID
		}
		if name == "" {
			fmt.Printf("%s ", styleDim("Container name (Esc to cancel):"))
			line, err := promptLine(os.Stdin)
			if err != nil {
				fatal(err)
			}
			name = strings.TrimSpace(line)
			if isEscCancelInput(name) {
				return
			}
			if name == "" {
				return
			}
		}
	}
	if !explicitProfile {
		if defaultProfileKey != "" && !interactive {
			*flags.profile = defaultProfileKey
		} else if len(codexProfileSummaries()) > 0 {
			selected, ok := selectCodexProfile("spawn", defaultProfileKey)
			if !ok {
				return
			}
			*flags.profile = selected.ID
		} else if !interactive && defaultProfileKey == "" {
			printUsage("usage: si spawn [name] [--profile <profile>] [--repo Org/Repo] [--gh-pat TOKEN]")
			return
		}
	}
	var profile *codexProfile
	if strings.TrimSpace(*flags.profile) != "" {
		parsed, err := requireCodexProfile(*flags.profile)
		if err != nil {
			fatal(err)
		}
		*flags.profile = parsed.ID
		profile = &parsed
		if name == "" {
			name = parsed.ID
		}
		if name != parsed.ID {
			warnf("profile %s selected; using container name %s", parsed.ID, parsed.ID)
			name = parsed.ID
		}
	}
	if name == "" {
		printUsage("usage: si spawn [name] [--profile <profile>] [--repo Org/Repo] [--gh-pat TOKEN]")
		return
	}
	if err := validateSlug(name); err != nil {
		fatal(err)
	}
	containerName := codexContainerName(name)
	if !workspaceSet {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		*flags.workspaceHost = cwd
	}
	if strings.TrimSpace(*flags.workspaceHost) == "" {
		*flags.workspaceHost = mustRepoRoot()
	}
	if abs, err := filepath.Abs(strings.TrimSpace(*flags.workspaceHost)); err == nil && strings.TrimSpace(abs) != "" {
		*flags.workspaceHost = abs
	}

	// Always mount the host workspace at /workspace (stable) and also at the same
	// absolute host path inside the container (for example
	// /home/<user>/Development/si). Default the container working dir to that
	// mirrored host path so `si run` and `si run --tmux` land in the expected
	// directory.
	workspaceTargetPrimary := "/workspace"
	workspaceTargetMirror := filepath.ToSlash(strings.TrimSpace(*flags.workspaceHost))
	if workspaceTargetMirror == "" || !strings.HasPrefix(workspaceTargetMirror, "/") {
		workspaceTargetMirror = workspaceTargetPrimary
	}
	if !workdirSet && (strings.TrimSpace(*flags.workdir) == "" || strings.TrimSpace(*flags.workdir) == "/workspace") {
		*flags.workdir = workspaceTargetMirror
	}
	desiredWorkspaceHost := filepath.Clean(strings.TrimSpace(*flags.workspaceHost))
	maybePersistWorkspaceDefault(workspaceScopeCodex, &settings, desiredWorkspaceHost, interactive)

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	if profile != nil {
		profileContainers, err := codexContainersByProfile(ctx, client, profile.ID)
		if err != nil {
			fatal(err)
		}
		if len(profileContainers) > 0 {
			choice := choosePreferredCodexContainer(profileContainers, containerName)
			if len(profileContainers) > 1 {
				names := make([]string, 0, len(profileContainers))
				for _, item := range profileContainers {
					names = append(names, item.Name)
				}
				warnf("multiple codex containers found for profile %s: %s", profile.ID, strings.Join(names, ", "))
			}
			existingID, info, err := client.ContainerByName(ctx, choice.Name)
			if err != nil {
				fatal(err)
			}
			if existingID != "" {
				if choice.Name != containerName {
					warnf("profile %s already bound to %s", profile.ID, choice.Name)
				}
				if info != nil && info.State != nil && !info.State.Running {
					if err := client.StartContainer(ctx, existingID); err != nil {
						fatal(err)
					}
				}
				// Ensure workspace mounts reflect the current host directory. If not, recreate.
				if info != nil && !codexContainerWorkspaceMatches(info, desiredWorkspaceHost, workspaceTargetMirror, requiredVaultFile) {
					warnf("codex container %s workspace/vault mounts differ from %s; recreating", choice.Name, desiredWorkspaceHost)
					if err := client.RemoveContainer(ctx, existingID, true); err != nil {
						fatal(err)
					}
					// Fall through to create a new container below.
				} else {
					if !*flags.cleanSlate {
						if identity, ok := hostGitIdentity(); ok {
							seedGitIdentity(ctx, client, existingID, "si", "/home/si", identity)
						}
					}
					infof("codex container %s already exists for profile %s", choice.Name, profile.ID)
					return
				}
			}
		}
	}

	existingID, info, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if existingID != "" {
		if profile != nil && info != nil && info.Config != nil {
			existingProfile := strings.TrimSpace(info.Config.Labels[codexProfileLabelKey])
			if existingProfile != "" && existingProfile != profile.ID {
				warnf("codex container %s already uses profile %s", containerName, existingProfile)
			}
		}
		if info != nil && info.State != nil && !info.State.Running {
			if err := client.StartContainer(ctx, existingID); err != nil {
				fatal(err)
			}
		}
		// Ensure workspace mounts reflect the current host directory. If not, recreate.
		if info != nil && !codexContainerWorkspaceMatches(info, desiredWorkspaceHost, workspaceTargetMirror, requiredVaultFile) {
			warnf("codex container %s workspace/vault mounts differ from %s; recreating", containerName, desiredWorkspaceHost)
			if err := client.RemoveContainer(ctx, existingID, true); err != nil {
				fatal(err)
			}
		} else {
			if !*flags.cleanSlate {
				if identity, ok := hostGitIdentity(); ok {
					seedGitIdentity(ctx, client, existingID, "si", "/home/si", identity)
				}
			}
			infof("codex container %s already exists", containerName)
			return
		}
	}

	if strings.TrimSpace(*flags.networkName) != "" {
		_, _ = client.EnsureNetwork(ctx, *flags.networkName, map[string]string{codexLabelKey: codexLabelValue})
	}

	codexVol := strings.TrimSpace(*flags.codexVolume)
	if codexVol == "" {
		codexVol = "si-codex-" + name
	}
	skillsVol := strings.TrimSpace(*flags.skillsVolume)
	if skillsVol == "" {
		skillsVol = shared.DefaultCodexSkillsVolume
	}
	ghVol := strings.TrimSpace(*flags.ghVolume)
	if ghVol == "" {
		ghVol = "si-gh-" + name
	}
	_, _ = client.EnsureVolume(ctx, codexVol, map[string]string{codexLabelKey: codexLabelValue})
	_, _ = client.EnsureVolume(ctx, skillsVol, map[string]string{codexLabelKey: codexLabelValue})
	_, _ = client.EnsureVolume(ctx, ghVol, map[string]string{codexLabelKey: codexLabelValue})

	labels := map[string]string{
		codexLabelKey: codexLabelValue,
		"si.name":     name,
	}
	if profile != nil {
		labels[codexProfileLabelKey] = profile.ID
	}

	env := []string{
		"HOME=/home/si",
		"CODEX_HOME=/home/si/.codex",
		"SI_WORKSPACE_PRIMARY=" + workspaceTargetPrimary,
		"SI_WORKSPACE_MIRROR=" + workspaceTargetMirror,
		"SI_WORKSPACE_HOST=" + strings.TrimSpace(*flags.workspaceHost),
	}
	env = append(env, hostUserEnv()...)
	if strings.TrimSpace(*flags.repo) != "" {
		env = append(env, "SI_REPO="+strings.TrimSpace(*flags.repo))
	}
	if strings.TrimSpace(*flags.ghPat) != "" {
		env = append(env, "SI_GH_PAT="+strings.TrimSpace(*flags.ghPat))
		env = append(env, "GH_TOKEN="+strings.TrimSpace(*flags.ghPat))
		env = append(env, "GITHUB_TOKEN="+strings.TrimSpace(*flags.ghPat))
	}
	if profile != nil {
		env = append(env, "SI_CODEX_PROFILE_ID="+profile.ID)
		env = append(env, "SI_CODEX_PROFILE_NAME="+profile.Name)
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
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: codexVol, Target: "/home/si/.codex"},
		{Type: mount.TypeVolume, Source: skillsVol, Target: "/home/si/.codex/skills"},
		{Type: mount.TypeVolume, Source: ghVol, Target: "/home/si/.config/gh"},
	}
	mounts = append(mounts, shared.BuildContainerCoreMounts(shared.ContainerCoreMountPlan{
		WorkspaceHost:          *flags.workspaceHost,
		WorkspacePrimaryTarget: workspaceTargetPrimary,
		WorkspaceMirrorTarget:  workspaceTargetMirror,
		ContainerHome:          "/home/si",
		IncludeHostSi:          true,
		HostVaultEnvFile:       requiredVaultFile,
	})...)
	if *flags.dockerSocket {
		if socketMount, ok := shared.DockerSocketMount(); ok {
			mounts = append(mounts, socketMount)
		}
	}
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        mounts,
		PortBindings:  bindings,
	}
	netCfg := &network.NetworkingConfig{}
	if strings.TrimSpace(*flags.networkName) != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				*flags.networkName: {Aliases: []string{containerName}},
			},
		}
	}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, containerName)
	if err != nil {
		fatal(err)
	}
	if err := client.StartContainer(ctx, id); err != nil {
		fatal(err)
	}
	seedCodexConfig(ctx, client, id, *flags.cleanSlate)
	if profile != nil {
		seedCodexAuth(ctx, client, id, *flags.cleanSlate, *profile)
	}
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
	fs := flag.NewFlagSet("respawn", flag.ExitOnError)
	flags := addCodexSpawnFlags(fs)
	removeVolumes := fs.Bool("volumes", false, "remove codex/gh volumes too")
	nameArg, filtered := splitNameAndFlags(args, codexRespawnBoolFlags())
	_ = fs.Parse(filtered)
	requestedName := strings.TrimSpace(nameArg)
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	defaultProfileKey := codexDefaultProfileKey(loadSettingsOrDefault())
	profileExplicit := flagProvided(args, "profile")
	profileInjected := false
	if !profileExplicit && strings.TrimSpace(*flags.profile) == "" && defaultProfileKey != "" && !interactive && requestedName == "" {
		*flags.profile = defaultProfileKey
	}
	profileKey := strings.TrimSpace(*flags.profile)
	if profileKey == "" && !profileExplicit && requestedName == "" {
		selected, ok := selectCodexProfile("respawn", defaultProfileKey)
		if !ok {
			return
		}
		profileKey = selected.ID
		filtered = append(filtered, "--profile", selected.ID)
		profileInjected = true
	}
	if profileKey != "" {
		selected, err := requireCodexProfile(profileKey)
		if err != nil {
			fatal(err)
		}
		profileKey = selected.ID
		if !profileExplicit && !profileInjected {
			filtered = append(filtered, "--profile", selected.ID)
		}
	}
	nameArg = requestedName
	if nameArg == "" && profileKey != "" {
		nameArg = profileKey
	}
	if profileKey != "" && nameArg != "" && nameArg != profileKey {
		warnf("profile %s selected; using container name %s", profileKey, profileKey)
		nameArg = profileKey
	}
	if nameArg == "" {
		selectedName, ok := selectCodexContainer("respawn", true)
		if !ok {
			return
		}
		nameArg = selectedName
	}
	name := nameArg
	removeTargets := map[string]struct{}{
		name: {},
	}
	if profileKey != "" {
		client, err := shared.NewClient()
		if err != nil {
			fatal(err)
		}
		refs, err := codexContainersByProfile(context.Background(), client, profileKey)
		_ = client.Close()
		if err != nil {
			fatal(err)
		}
		for _, ref := range refs {
			removeTargets[codexContainerSlug(ref.Name)] = struct{}{}
		}
	}
	targets := make([]string, 0, len(removeTargets))
	for target := range removeTargets {
		target = strings.TrimSpace(target)
		if target != "" {
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	for _, target := range targets {
		removeArgs := []string{target}
		if *removeVolumes {
			removeArgs = append([]string{"--volumes"}, removeArgs...)
		}
		cmdCodexRemove(removeArgs)
	}
	spawnArgs := append(stripFlag(filtered, "volumes"), name)
	cmdCodexSpawn(spawnArgs)
}

func cmdCodexList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
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
	headers := []string{
		styleHeading("CONTAINER"),
		styleHeading("STATE"),
		styleHeading("IMAGE"),
	}
	rows := make([][]string, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		image := codexImageDisplay(c.Image)
		rows = append(rows, []string{name, styleStatus(c.State), image})
	}
	printAlignedTable(headers, rows, 2)
}

func cmdCodexExec(args []string) {
	if flagProvided(args, "autopoietic") {
		fatal(fmt.Errorf("--autopoietic has been removed; dyads now provide the paired actor/critic workflow (use `si dyad ...`). You can still use `si run <name> --tmux` to attach to a codex container"))
	}
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	oneOff := fs.Bool("one-off", false, "run a one-off codex exec in an isolated container")
	tmuxAttach := fs.Bool("tmux", false, "force tmux attach for existing container mode")
	noTmux := fs.Bool("no-tmux", false, "disable tmux attach for existing container mode")
	promptFlag := fs.String("prompt", "", "prompt to execute (one-off mode)")
	outputOnly := fs.Bool("output-only", false, "print only the Codex response (one-off mode)")
	noMcp := fs.Bool("no-mcp", false, "disable MCP servers (one-off mode)")
	profileKey := fs.String("profile", "", "codex profile name/email (one-off mode)")
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "aureuma/si:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	codexVolume := fs.String("codex-volume", envOr("SI_CODEX_EXEC_VOLUME", ""), "codex volume name")
	skillsVolume := fs.String("skills-volume", envOr("SI_CODEX_SKILLS_VOLUME", ""), "shared codex skills volume name")
	ghVolume := fs.String("gh-volume", "", "gh config volume name")
	model := fs.String("model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	effort := fs.String("effort", envOr("CODEX_REASONING_EFFORT", "medium"), "codex reasoning effort")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in one-off containers")
	keep := fs.Bool("keep", false, "keep the one-off container after execution")
	envs := multiFlag{}
	fs.Var(&envs, "env", "env var (repeatable KEY=VALUE)")
	_ = fs.Parse(args)

	settings := loadSettingsOrDefault()
	requiredVaultFile := vaultContainerEnvFileMountPath(settings)
	if !flagProvided(args, "docker-socket") && settings.Codex.DockerSocket != nil {
		*dockerSocket = *settings.Codex.DockerSocket
	}
	if !flagProvided(args, "skills-volume") && strings.TrimSpace(settings.Codex.SkillsVolume) != "" {
		*skillsVolume = strings.TrimSpace(settings.Codex.SkillsVolume)
	}

	prompt := strings.TrimSpace(*promptFlag)
	rest := fs.Args()
	if prompt == "" && len(rest) == 1 && !isValidSlug(rest[0]) {
		prompt = rest[0]
		rest = nil
	}

	if *oneOff || prompt != "" || *outputOnly || *noMcp {
		if *tmuxAttach {
			fatal(fmt.Errorf("--tmux is only supported in existing container mode"))
		}
		if prompt == "" && len(rest) > 0 {
			prompt = strings.Join(rest, " ")
		}
		if strings.TrimSpace(prompt) == "" {
			printUsage("usage: si run --prompt \"...\" [--output-only] [--no-mcp]")
			fmt.Println(styleDim("   or: si run \"...\" [--output-only] [--no-mcp]"))
			return
		}
		var profile *codexProfile
		if strings.TrimSpace(*profileKey) != "" {
			parsed, err := requireCodexProfile(*profileKey)
			if err != nil {
				fatal(err)
			}
			profile = &parsed
		}
		opts := codexExecOneOffOptions{
			Prompt:        prompt,
			Image:         strings.TrimSpace(*image),
			WorkspaceHost: strings.TrimSpace(*workspaceHost),
			Workdir:       strings.TrimSpace(*workdir),
			Network:       strings.TrimSpace(*networkName),
			CodexVolume:   strings.TrimSpace(*codexVolume),
			SkillsVolume:  strings.TrimSpace(*skillsVolume),
			GHVolume:      strings.TrimSpace(*ghVolume),
			Env:           envs,
			Model:         strings.TrimSpace(*model),
			Effort:        strings.TrimSpace(*effort),
			DisableMCP:    *noMcp,
			OutputOnly:    *outputOnly,
			KeepContainer: *keep,
			DockerSocket:  *dockerSocket,
			VaultEnvFile:  requiredVaultFile,
			Profile:       profile,
		}
		if opts.SkillsVolume == "" {
			opts.SkillsVolume = shared.DefaultCodexSkillsVolume
		}
		if err := runCodexExecOneOff(opts); err != nil {
			fatal(err)
		}
		return
	}

	rest = consumeRunContainerModeFlags(rest, tmuxAttach, noTmux)
	if len(rest) < 1 {
		name, ok := selectCodexContainer("run", true)
		if !ok {
			return
		}
		rest = []string{name}
	}
	if *tmuxAttach && *noTmux {
		fatal(fmt.Errorf("--tmux and --no-tmux cannot be combined"))
	}
	name := rest[0]
	containerName := codexContainerName(name)
	cmd := rest[1:]
	tmuxMode := true
	if *noTmux {
		tmuxMode = false
	}
	if *tmuxAttach {
		tmuxMode = true
	}
	if err := validateRunTmuxArgs(tmuxMode, cmd); err != nil {
		fatal(err)
	}

	profileID := ""
	profileName := ""
	resumeProfileKey := codexResumeProfileKey("", containerName)
	resumeRecord := codexSessionRecord{}
	var containerID string
	var containerInfo *types.ContainerJSON
	client, clientErr := shared.NewClient()
	if clientErr == nil {
		defer client.Close()
		ctx := context.Background()
		id, info, err := client.ContainerByName(ctx, containerName)
		if err != nil {
			if tmuxMode {
				fatal(err)
			}
		} else if id != "" {
			containerID = id
			containerInfo = info
			if info != nil && info.Config != nil {
				profileID = strings.TrimSpace(info.Config.Labels[codexProfileLabelKey])
			}
			if profileID != "" {
				if prof, ok := codexProfileByKey(profileID); ok {
					profileName = prof.Name
				} else {
					profileName = profileID
				}
				resumeProfileKey = codexResumeProfileKey(profileID, containerName)
			}
		} else if *tmuxAttach {
			fatal(fmt.Errorf("codex container %s not found", containerName))
		}

		if strings.TrimSpace(containerID) != "" {
			if containerInfo != nil && (!shared.HasHostSiMount(containerInfo, "/home/si") || !shared.HasHostVaultEnvFileMount(containerInfo, requiredVaultFile)) {
				slug := codexContainerSlug(containerName)
				if strings.TrimSpace(slug) == "" {
					fatal(fmt.Errorf("codex container %s is missing required host `si vault` mounts; run `si respawn %s`", containerName, containerName))
				}
				if err := reconcileCodexRunMountDrift(tmuxMode, containerName, slug, func() error {
					spawnArgs := []string{slug}
					if workspace := codexContainerWorkspaceSource(containerInfo); workspace != "" {
						spawnArgs = append(spawnArgs, "--workspace", workspace)
					}
					if containerInfo.Config != nil {
						if image := strings.TrimSpace(containerInfo.Config.Image); image != "" {
							spawnArgs = append(spawnArgs, "--image", image)
						}
					}
					if profileID != "" && strings.EqualFold(strings.TrimSpace(slug), strings.TrimSpace(profileID)) {
						spawnArgs = append(spawnArgs, "--profile", strings.TrimSpace(profileID))
					}
					cmdCodexSpawn(spawnArgs)
					id, info, err = client.ContainerByName(ctx, containerName)
					if err != nil {
						return err
					}
					if id == "" || info == nil {
						return fmt.Errorf("codex container %s recreation failed", containerName)
					}
					if !shared.HasHostSiMount(info, "/home/si") || !shared.HasHostVaultEnvFileMount(info, requiredVaultFile) {
						return fmt.Errorf("codex container %s recreation missing required host `si vault` mounts; run `si respawn %s`", containerName, codexContainerSlug(containerName))
					}
					containerID = id
					containerInfo = info
					return nil
				}); err != nil {
					fatal(err)
				}
			}
			seedCodexConfig(ctx, client, containerID, false)
			if profileID != "" {
				if profile, ok := codexProfileByKey(profileID); ok {
					seedCodexAuth(ctx, client, containerID, false, profile)
				}
			}
			if record, err := syncCodexProfileSessionRecordFromContainer(ctx, client, containerID, resumeProfileKey); err == nil && strings.TrimSpace(record.SessionID) != "" {
				resumeRecord = record
			} else if err != nil {
				warnf("codex session metadata capture failed for %s: %v", containerName, err)
			}
		}
		if resumeProfileKey != "" && strings.TrimSpace(resumeRecord.SessionID) == "" {
			record, err := loadCodexProfileSessionRecord(resumeProfileKey)
			if err != nil {
				warnf("codex session metadata load failed for profile %s: %v", resumeProfileKey, err)
			} else {
				resumeRecord = record
			}
		}
	} else if tmuxMode {
		fatal(fmt.Errorf("docker client unavailable: %w", clientErr))
	}

	if tmuxMode {
		if err := attachCodexTmuxPane(containerName, resumeProfileKey, resumeRecord); err != nil {
			fatal(err)
		}
		if client != nil && strings.TrimSpace(containerID) != "" && strings.TrimSpace(resumeProfileKey) != "" {
			ctx := context.Background()
			if _, err := syncCodexProfileSessionRecordFromContainer(ctx, client, containerID, resumeProfileKey); err != nil {
				warnf("codex session metadata update failed for %s: %v", containerName, err)
			}
		}
		return
	}

	if len(cmd) == 0 {
		rc := buildShellRC(settings)
		cmd = []string{"bash", "-lc", fmt.Sprintf(`rc="/tmp/si-shellrc"
cat > "$rc" <<'EOF'
%s
EOF
printf '\033]0;%%s\007' "${SI_TERM_TITLE:-}"
exec bash --rcfile "$rc" -i`, rc)}
	}
	hostCwd := ""
	if cwd, err := os.Getwd(); err == nil {
		hostCwd = filepath.ToSlash(strings.TrimSpace(cwd))
	}
	baseArgs := []string{"exec"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		baseArgs = append(baseArgs, "-it")
	} else {
		baseArgs = append(baseArgs, "-i")
	}
	baseArgs = append(baseArgs, "-e", "SI_TERM_TITLE="+name)
	if profileID != "" {
		baseArgs = append(baseArgs, "-e", "SI_CODEX_PROFILE_ID="+profileID)
	}
	if profileName != "" {
		baseArgs = append(baseArgs, "-e", "SI_CODEX_PROFILE_NAME="+profileName)
	}
	execArgs := make([]string, 0, len(baseArgs)+2+len(cmd))
	execArgs = append(execArgs, baseArgs...)
	usedWorkdir := false
	if hostCwd != "" && strings.HasPrefix(hostCwd, "/") {
		wd := hostCwd
		if containerInfo != nil {
			if mapped, ok := containerCwdForHostCwd(containerInfo, hostCwd); ok {
				wd = mapped
			}
		}
		execArgs = append(execArgs, "-w", wd)
		usedWorkdir = true
	}
	execArgs = append(execArgs, containerName)
	execArgs = append(execArgs, cmd...)
	if err := execDockerCLI(execArgs...); err != nil {
		// If the host cwd isn't mapped inside the container (e.g. old container mounts),
		// retry without forcing the working directory.
		if usedWorkdir {
			retryArgs := make([]string, 0, len(baseArgs)+1+len(cmd))
			retryArgs = append(retryArgs, baseArgs...)
			retryArgs = append(retryArgs, containerName)
			retryArgs = append(retryArgs, cmd...)
			if retryErr := execDockerCLI(retryArgs...); retryErr == nil {
				return
			}
		}
		fatal(err)
	}
}

func selectCodexContainer(action string, nameHint bool) (string, bool) {
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
		fmt.Println(styleDim("no codex containers found (run: si spawn [name])"))
		return "", false
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Names[0] < containers[j].Names[0]
	})

	items := make([]codexContainerItem, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		items = append(items, codexContainerItem{
			Name:  name,
			State: c.State,
			Image: codexImageDisplay(c.Image),
		})
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Name, styleStatus(item.State), item.Image})
	}
	rendered := renderAlignedRows(rows, 2)

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleHeading("Available codex containers:"))
		for i, line := range rendered {
			fmt.Printf("  %2d) %s\n", i+1, line)
		}
		hint := "re-run with: si " + action
		if nameHint {
			hint += " <name>"
		}
		fmt.Println(styleDim(hint))
		return "", false
	}

	fmt.Println(styleHeading("Available codex containers:"))
	for i, line := range rendered {
		fmt.Printf("  %2d) %s\n", i+1, line)
	}

	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select container [1-%d] (Enter/Esc to cancel):", len(items))))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if isEscCancelInput(line) {
		return "", false
	}
	if line == "" {
		return "", false
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(items) {
		fmt.Println(styleDim("invalid selection"))
		return "", false
	}
	return items[idx-1].Name, true
}

func selectCodexContainerFromList(action string) (string, bool) {
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
		fmt.Println(styleDim("no codex containers found (run: si spawn [name])"))
		return "", false
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Names[0] < containers[j].Names[0]
	})

	items := make([]codexContainerItem, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		items = append(items, codexContainerItem{
			Name:  name,
			State: c.State,
			Image: codexImageDisplay(c.Image),
		})
	}

	headers := []string{
		styleHeading("CONTAINER"),
		styleHeading("STATE"),
		styleHeading("IMAGE"),
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Name, styleStatus(item.State), item.Image})
	}
	printAlignedTable(headers, rows, 2)

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleDim("re-run with: si " + action + " <name>"))
		return "", false
	}

	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select container [1-%d] or name (Enter/Esc to cancel):", len(items))))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if isEscCancelInput(line) {
		return "", false
	}
	if line == "" {
		return "", false
	}
	if idx, err := strconv.Atoi(line); err == nil {
		if idx < 1 || idx > len(items) {
			fmt.Println(styleDim("invalid selection"))
			return "", false
		}
		return items[idx-1].Name, true
	}
	line = strings.TrimPrefix(line, "/")
	for _, item := range items {
		if item.Name == line {
			return item.Name, true
		}
	}
	fmt.Println(styleDim("invalid selection"))
	return "", false
}

type codexContainerItem struct {
	Name  string
	State string
	Image string
}

type codexProfileContainerRef struct {
	Name  string
	State string
}

func codexContainersByProfile(ctx context.Context, client *shared.Client, profileID string) ([]codexProfileContainerRef, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, nil
	}
	containers, err := client.ListContainers(ctx, true, map[string]string{codexLabelKey: codexLabelValue})
	if err != nil {
		return nil, err
	}
	refs := make([]codexProfileContainerRef, 0, len(containers))
	for _, item := range containers {
		labelValue := strings.TrimSpace(item.Labels[codexProfileLabelKey])
		if !strings.EqualFold(labelValue, profileID) {
			continue
		}
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimSpace(strings.TrimPrefix(item.Names[0], "/"))
		}
		if name == "" {
			continue
		}
		refs = append(refs, codexProfileContainerRef{Name: name, State: strings.TrimSpace(item.State)})
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Name < refs[j].Name
	})
	return refs, nil
}

func choosePreferredCodexContainer(items []codexProfileContainerRef, preferred string) codexProfileContainerRef {
	if len(items) == 0 {
		return codexProfileContainerRef{}
	}
	preferred = strings.TrimSpace(preferred)
	for _, item := range items {
		if item.Name == preferred {
			return item
		}
	}
	for _, item := range items {
		if strings.EqualFold(item.State, "running") {
			return item
		}
	}
	return items[0]
}

func codexImageDisplay(image string) string {
	if image == "" {
		return ""
	}
	if strings.HasPrefix(image, "sha256:") {
		return ""
	}
	if at := strings.Index(image, "@sha256:"); at != -1 {
		return image[:at]
	}
	return image
}

func validateRunTmuxArgs(tmux bool, cmd []string) error {
	if tmux && len(cmd) > 0 {
		return fmt.Errorf("custom command mode requires --no-tmux")
	}
	return nil
}

func consumeRunContainerModeFlags(args []string, tmuxAttach *bool, noTmux *bool) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	out = append(out, args[0])
	parseModeFlags := true
	for _, arg := range args[1:] {
		if !parseModeFlags {
			out = append(out, arg)
			continue
		}
		switch strings.TrimSpace(arg) {
		case "--tmux":
			if tmuxAttach != nil {
				*tmuxAttach = true
			}
		case "--no-tmux":
			if noTmux != nil {
				*noTmux = true
			}
		default:
			parseModeFlags = false
			out = append(out, arg)
		}
	}
	return out
}

func codexTmuxSessionName(codexContainerName string) string {
	slug := codexContainerSlug(codexContainerName)
	if slug == "" {
		slug = strings.TrimSpace(codexContainerName)
	}
	return codexTmuxSessionPrefix + slug
}

func attachCodexTmuxPane(containerName string, resumeProfileKey string, resumeRecord codexSessionRecord) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("--tmux requires an interactive terminal")
	}
	if err := ensureTmuxAvailable(); err != nil {
		return err
	}
	session := codexTmuxSessionName(containerName)
	target := session + ":0.0"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hostCwd := ""
	if cwd, err := os.Getwd(); err == nil {
		hostCwd = filepath.ToSlash(strings.TrimSpace(cwd))
	}
	startDir := hostCwd
	// Map the host cwd onto the container's bind mounts so `si run --tmux` works even for older
	// containers that don't mirror the host absolute path (for example: mount at /home/si/Development/si).
	if hostCwd != "" && strings.HasPrefix(hostCwd, "/") {
		if client, err := shared.NewClient(); err == nil {
			defer client.Close()
			_, info, err := client.ContainerByName(ctx, containerName)
			if err == nil {
				if mapped, ok := containerCwdForHostCwd(info, hostCwd); ok {
					startDir = mapped
				}
			}
		}
	}
	cmd := buildCodexTmuxCommand(containerName, startDir)
	resumeSessionID := strings.TrimSpace(resumeRecord.SessionID)
	resumeCmd := buildCodexTmuxResumeCommand(containerName, startDir, resumeSessionID, resumeProfileKey)
	// Hash a stable "shape" of the command so we don't thrash sessions just because the user attached
	// from a different host subdirectory.
	cmdHashShape := buildCodexTmuxCommand(containerName, "__SI_START_DIR__")
	cmdHash := sha256.Sum256([]byte(cmdHashShape))
	cmdHashHex := hex.EncodeToString(cmdHash[:])
	if err := ensureCodexTmuxSession(ctx, session, target, cmd, resumeCmd, cmdHashHex, hostCwd, resumeSessionID, resumeProfileKey); err != nil {
		return err
	}
	tmuxTryLabelCodexPane(ctx, session, target, containerName)
	// #nosec G204 -- fixed tmux binary and session target generated by si.
	tmuxCmd := exec.Command("tmux", "attach-session", "-t", session)
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr
	tmuxCmd.Stdin = os.Stdin
	return tmuxCmd.Run()
}

func ensureCodexTmuxSession(
	ctx context.Context,
	session string,
	target string,
	cmd string,
	resumeCmd string,
	cmdHashHex string,
	hostCwd string,
	resumeSessionID string,
	resumeProfileKey string,
) error {
	_, hasSessionErr := tmuxOutput(ctx, "has-session", "-t", session)
	hasSession := hasSessionErr == nil
	cmdChanged := false
	hostCwdChanged := false
	if hasSession {
		if out, err := tmuxOutput(ctx, "show-options", "-v", "-t", session, codexTmuxCmdShaOpt); err == nil {
			stored := strings.TrimSpace(out)
			if stored != "" && strings.TrimSpace(cmdHashHex) != "" && stored != strings.TrimSpace(cmdHashHex) {
				cmdChanged = true
			}
		}
		if hostCwd != "" && strings.HasPrefix(hostCwd, "/") {
			if out, err := tmuxOutput(ctx, "show-options", "-v", "-t", session, codexTmuxHostCwdOpt); err == nil {
				stored := strings.TrimSpace(out)
				if stored != "" && stored != hostCwd {
					hostCwdChanged = true
				}
			}
		}
	}

	if !hasSession {
		launchCmd, resumed := codexSelectTmuxLaunchCommand(cmd, resumeCmd)
		if resumed {
			warnf("tmux session %s unavailable; resuming codex session %s for profile %s", session, strings.TrimSpace(resumeSessionID), firstNonEmpty(strings.TrimSpace(resumeProfileKey), "unknown"))
		}
		if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "bash", "-lc", launchCmd); err != nil {
			return err
		}
	}
	applyTmuxSessionDefaults(ctx, session)

	paneDead := false
	if out, err := tmuxOutput(ctx, "display-message", "-p", "-t", target, "#{pane_dead}"); err == nil {
		paneDead = isTmuxPaneDeadOutput(out)
	}
	if codexTmuxShouldResetSession(paneDead, cmdChanged, hostCwdChanged) {
		_, _ = tmuxOutput(ctx, "kill-session", "-t", session)
		launchCmd, resumed := codexSelectTmuxLaunchCommand(cmd, resumeCmd)
		if resumed {
			warnf("tmux session %s could not be recovered; resuming codex session %s for profile %s", session, strings.TrimSpace(resumeSessionID), firstNonEmpty(strings.TrimSpace(resumeProfileKey), "unknown"))
		}
		if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "bash", "-lc", launchCmd); err != nil {
			return err
		}
		applyTmuxSessionDefaults(ctx, session)
	}
	recordCodexTmuxSessionMetadata(ctx, session, cmdHashHex, hostCwd)
	return nil
}

func codexTmuxShouldResetSession(paneDead bool, cmdChanged bool, hostCwdChanged bool) bool {
	if paneDead {
		return true
	}
	// A live Codex pane should survive reconnects and path/metadata drift.
	_ = cmdChanged
	_ = hostCwdChanged
	return false
}

func codexRunShouldRecreateContainerForMissingVaultMounts(tmuxMode bool) bool {
	// In tmux mode, preserving the existing container keeps the live Codex pane
	// and conversation state intact across reconnects.
	return !tmuxMode
}

func reconcileCodexRunMountDrift(tmuxMode bool, containerName string, slug string, recreate func() error) error {
	if codexRunShouldRecreateContainerForMissingVaultMounts(tmuxMode) {
		warnf("codex container %s is missing required host `si vault` mounts; recreating for full `si`/`si vault` support", containerName)
		if recreate == nil {
			return errors.New("recreate callback required")
		}
		return recreate()
	}
	warnf("codex container %s is missing required host `si vault` mounts; preserving running container and tmux session (run `si respawn %s` to reconcile mounts)", containerName, slug)
	return nil
}

func recordCodexTmuxSessionMetadata(ctx context.Context, session string, cmdHashHex string, hostCwd string) {
	if strings.TrimSpace(cmdHashHex) != "" {
		_, _ = tmuxOutput(ctx, "set-option", "-t", session, codexTmuxCmdShaOpt, strings.TrimSpace(cmdHashHex))
	}
	if hostCwd != "" && strings.HasPrefix(hostCwd, "/") {
		_, _ = tmuxOutput(ctx, "set-option", "-t", session, codexTmuxHostCwdOpt, hostCwd)
	}
}

func tmuxTryLabelCodexPane(ctx context.Context, session string, target string, containerName string) {
	title := strings.TrimSpace(containerName)
	if title == "" {
		title = "codex"
	}
	// Prefer a readable window name and a visible pane title.
	title = "🧠 " + title
	_, _ = tmuxOutput(ctx, "rename-window", "-t", session+":0", title)
	_, _ = tmuxOutput(ctx, "select-pane", "-t", target, "-T", title)
	// Show titles in borders; session-scoped so it won't affect other tmux sessions.
	_, _ = tmuxOutput(ctx, "set-option", "-t", session, "pane-border-status", "top")
	_, _ = tmuxOutput(ctx, "set-option", "-t", session, "pane-border-format", "#{pane_title}")
}

func isTmuxPaneDeadOutput(out string) bool {
	return strings.TrimSpace(out) == "1"
}

func buildCodexTmuxCommand(containerName string, hostCwd string) string {
	startDir := strings.TrimSpace(hostCwd)
	cdStart := ""
	if startDir != "" && strings.HasPrefix(startDir, "/") {
		cdStart = fmt.Sprintf("cd %s 2>/dev/null || ", shellSingleQuote(startDir))
	}
	inner := "export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/home/si CODEX_HOME=/home/si/.codex; " +
		cdStart +
		"cd \"${SI_WORKSPACE_MIRROR:-/workspace}\" 2>/dev/null || cd /workspace 2>/dev/null || true; " +
		"codex --dangerously-bypass-approvals-and-sandbox; status=$?; " +
		"printf '\\n[si] codex exited (status %s). Run codex again, or exit to close this pane.\\n' \"$status\"; " +
		"exec bash -il"
	base := fmt.Sprintf("docker exec -it %s bash -lc %s", shellSingleQuote(strings.TrimSpace(containerName)), shellSingleQuote(inner))
	return fmt.Sprintf("%s || sudo -n %s", base, base)
}

func buildCodexTmuxResumeCommand(containerName string, hostCwd string, sessionID string, profileKey string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	startDir := strings.TrimSpace(hostCwd)
	cdStart := ""
	if startDir != "" && strings.HasPrefix(startDir, "/") {
		cdStart = fmt.Sprintf("cd %s 2>/dev/null || ", shellSingleQuote(startDir))
	}
	profileLabel := strings.TrimSpace(profileKey)
	if profileLabel == "" {
		profileLabel = codexContainerSlug(containerName)
	}
	inner := "export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/home/si CODEX_HOME=/home/si/.codex; " +
		cdStart +
		"cd \"${SI_WORKSPACE_MIRROR:-/workspace}\" 2>/dev/null || cd /workspace 2>/dev/null || true; " +
		"printf '\\n[si] tmux session unavailable; attempting codex resume %s for profile %s.\\n' " + shellSingleQuote(sessionID) + " " + shellSingleQuote(profileLabel) + "; " +
		"codex resume " + shellSingleQuote(sessionID) + " --dangerously-bypass-approvals-and-sandbox || codex --dangerously-bypass-approvals-and-sandbox; status=$?; " +
		"printf '\\n[si] codex exited (status %s). Run codex again, or exit to close this pane.\\n' \"$status\"; " +
		"exec bash -il"
	base := fmt.Sprintf("docker exec -it %s bash -lc %s", shellSingleQuote(strings.TrimSpace(containerName)), shellSingleQuote(inner))
	return fmt.Sprintf("%s || sudo -n %s", base, base)
}

func codexSelectTmuxLaunchCommand(cmd string, resumeCmd string) (string, bool) {
	resumeCmd = strings.TrimSpace(resumeCmd)
	if resumeCmd == "" {
		return cmd, false
	}
	return resumeCmd, true
}

func containerCwdForHostCwd(info *types.ContainerJSON, hostCwd string) (string, bool) {
	if info == nil {
		return "", false
	}
	hostCwd = filepath.Clean(strings.TrimSpace(hostCwd))
	if hostCwd == "" || !strings.HasPrefix(hostCwd, "/") {
		return "", false
	}

	// If the user is in a symlinked directory, try the physical path too so we can match Docker's mount source.
	hostCwdEval := hostCwd
	if eval, err := filepath.EvalSymlinks(hostCwd); err == nil {
		eval = filepath.Clean(strings.TrimSpace(eval))
		if eval != "" && strings.HasPrefix(eval, "/") {
			hostCwdEval = eval
		}
	}

	bestSrcLen := -1
	bestDest := ""
	bestRel := ""

	tryMatch := func(cwd, src, dest string) {
		if src == "" || dest == "" {
			return
		}
		if !strings.HasPrefix(src, "/") {
			return
		}
		if cwd != src && !strings.HasPrefix(cwd, src+string(os.PathSeparator)) {
			return
		}
		rel, err := filepath.Rel(src, cwd)
		if err != nil {
			return
		}
		rel = filepath.Clean(strings.TrimSpace(rel))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return
		}
		candidate := dest
		if rel != "" && rel != "." {
			candidate = path.Join(dest, filepath.ToSlash(rel))
		}
		bestCandidate := bestDest
		if bestRel != "" && bestRel != "." && bestDest != "" {
			bestCandidate = path.Join(bestDest, filepath.ToSlash(bestRel))
		}
		cwdSlash := filepath.ToSlash(cwd)
		candidateMatchesCwd := candidate == cwdSlash
		bestMatchesCwd := bestCandidate == cwdSlash
		if len(src) > bestSrcLen ||
			(len(src) == bestSrcLen && candidateMatchesCwd && !bestMatchesCwd) ||
			(len(src) == bestSrcLen && candidateMatchesCwd == bestMatchesCwd && bestDest == "/workspace" && dest != "/workspace") {
			bestSrcLen = len(src)
			bestDest = dest
			bestRel = rel
		}
	}

	for _, m := range info.Mounts {
		if strings.TrimSpace(string(m.Type)) != "bind" {
			continue
		}
		src := filepath.Clean(strings.TrimSpace(m.Source))
		dest := filepath.ToSlash(strings.TrimSpace(m.Destination))
		if dest == "" || !strings.HasPrefix(dest, "/") {
			continue
		}
		tryMatch(hostCwd, src, dest)
		if hostCwdEval != hostCwd {
			tryMatch(hostCwdEval, src, dest)
		}
	}

	if bestSrcLen < 0 || bestDest == "" {
		return "", false
	}
	if bestRel == "" || bestRel == "." {
		return bestDest, true
	}
	return path.Join(bestDest, filepath.ToSlash(bestRel)), true
}

func codexContainerWorkspaceMatches(info *types.ContainerJSON, desiredHost, mirrorTarget, requiredVaultFile string) bool {
	if info == nil {
		return false
	}
	if !shared.HasHostSiMount(info, "/home/si") ||
		!shared.HasDevelopmentMount(info, desiredHost, "/home/si") ||
		!shared.HasHostDevelopmentMount(info, desiredHost) ||
		!shared.HasHostVaultEnvFileMount(info, requiredVaultFile) {
		return false
	}
	desiredHost = filepath.Clean(strings.TrimSpace(desiredHost))
	mirrorTarget = filepath.ToSlash(strings.TrimSpace(mirrorTarget))
	if desiredHost == "" {
		return false
	}
	if mirrorTarget == "" || !strings.HasPrefix(mirrorTarget, "/") {
		mirrorTarget = "/workspace"
	}

	hasWorkspace := false
	hasMirror := mirrorTarget == "/workspace"
	for _, m := range info.Mounts {
		if strings.TrimSpace(string(m.Type)) != "bind" {
			continue
		}
		src := filepath.Clean(strings.TrimSpace(m.Source))
		dest := filepath.ToSlash(strings.TrimSpace(m.Destination))
		if dest == "/workspace" && src == desiredHost {
			hasWorkspace = true
		}
		if mirrorTarget != "/workspace" && dest == mirrorTarget && src == desiredHost {
			hasMirror = true
		}
	}
	if !hasWorkspace || !hasMirror {
		return false
	}
	if info.Config == nil {
		return false
	}
	wd := filepath.ToSlash(strings.TrimSpace(info.Config.WorkingDir))
	if wd != "" && wd != mirrorTarget {
		return false
	}
	if envValue(info.Config.Env, "SI_WORKSPACE_MIRROR") != mirrorTarget {
		return false
	}
	if envValue(info.Config.Env, "SI_WORKSPACE_HOST") != desiredHost {
		return false
	}
	return true
}

func codexContainerWorkspaceSource(info *types.ContainerJSON) string {
	if info == nil {
		return ""
	}
	for _, m := range info.Mounts {
		if strings.TrimSpace(string(m.Type)) != "bind" {
			continue
		}
		dest := filepath.ToSlash(strings.TrimSpace(m.Destination))
		if dest != "/workspace" {
			continue
		}
		src := filepath.Clean(strings.TrimSpace(m.Source))
		if src != "" && strings.HasPrefix(src, "/") {
			return src
		}
	}
	return ""
}

func envValue(env []string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for _, item := range env {
		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, key+"=") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(item, key+"="))
	}
	return ""
}

func cmdCodexLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	deviceAuth := fs.Bool("device-auth", true, "use device auth flow")
	openURL := fs.Bool("open-url", false, "open login URL in browser")
	openURLCmd := fs.String("open-url-cmd", "", "command to open login URL (use {url})")
	safariProfile := fs.String("safari-profile", "", "override Safari profile name (macOS only)")
	_ = fs.Parse(args)
	settings := loadSettingsOrDefault()
	if !flagProvided(args, "device-auth") && settings.Codex.Login.DeviceAuth != nil {
		*deviceAuth = *settings.Codex.Login.DeviceAuth
	}
	if !flagProvided(args, "open-url") && settings.Codex.Login.OpenURL != nil {
		*openURL = *settings.Codex.Login.OpenURL
	}
	if !flagProvided(args, "open-url-cmd") && strings.TrimSpace(settings.Codex.Login.OpenURLCommand) != "" {
		*openURLCmd = settings.Codex.Login.OpenURLCommand
	}
	if fs.NArg() > 1 {
		printUsage("usage: si login [profile] [--device-auth] [--open-url] [--open-url-cmd <command>] [--safari-profile <name>]")
		return
	}
	profileKey := ""
	if fs.NArg() == 1 {
		profileKey = fs.Arg(0)
	}

	var profile *codexProfile
	if strings.TrimSpace(profileKey) != "" {
		parsed, err := requireCodexProfile(profileKey)
		if err != nil {
			fatal(err)
		}
		profile = &parsed
	} else if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		selected, ok := selectCodexProfile("login", "")
		if !ok {
			return
		}
		profile = &selected
	} else {
		printUsage("usage: si login [profile] [--device-auth] [--open-url] [--open-url-cmd <command>] [--safari-profile <name>]")
		return
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	image := strings.TrimSpace(envOr("SI_CODEX_IMAGE", "aureuma/si:local"))
	if image == "" {
		fatal(fmt.Errorf("codex image required"))
	}
	containerName := fmt.Sprintf("si-codex-login-%s-%d", profile.ID, time.Now().UnixNano())
	labels := map[string]string{
		codexLabelKey:        codexLabelValue,
		codexProfileLabelKey: profile.ID,
		"si.mode":            "login",
	}
	cfg := &container.Config{
		Image:  image,
		Env:    filterEnv(append([]string{"HOME=/home/si", "CODEX_HOME=/home/si/.codex"}, hostUserEnv()...)),
		Labels: labels,
		Cmd:    []string{"bash", "-lc", "sleep infinity"},
	}
	hostCfg := &container.HostConfig{}
	netCfg := &network.NetworkingConfig{}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, containerName)
	if err != nil {
		fatal(err)
	}
	removeContainer := func() {
		_ = client.RemoveContainer(ctx, id, true)
	}
	if err := client.StartContainer(ctx, id); err != nil {
		removeContainer()
		fatal(err)
	}
	seedCodexConfig(ctx, client, id, false)

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
	watcher := newLoginURLWatcher(func(url string) {
		if *openURL {
			openLoginURL(url, *profile, *openURLCmd, *safariProfile)
		}
	}, copyDeviceCodeToClipboard)
	if err := execDockerCLIWithOutput(execArgs, watcher.Feed); err != nil {
		removeContainer()
		fatal(err)
	}
	if err := cacheCodexAuthFromContainer(ctx, client, id, *profile); err != nil {
		warnf("codex auth cache failed: %v", err)
	} else {
		successf("🔐 cached codex auth for profile %s", profile.ID)
	}
	if err := updateSettingsProfile(*profile); err != nil {
		warnf("settings update failed: %v", err)
	}
	triggerWarmupAfterLogin(*profile)
	removeContainer()
}

func selectCodexProfile(action string, defaultKey string) (codexProfile, bool) {
	items := codexProfileSummaries()
	if len(items) == 0 {
		fmt.Println(styleDim("no codex profiles configured"))
		return codexProfile{}, false
	}
	defaultKey = strings.TrimSpace(defaultKey)

	fmt.Println(styleHeading("Available codex profiles:"))
	printCodexProfilesTable(items, false, true)

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleDim("re-run with: si " + action + " <profile>"))
		return codexProfile{}, false
	}

	var prompt string
	if defaultKey != "" {
		prompt = fmt.Sprintf("Select profile [1-%d] (default %s, Esc to cancel)", len(items), defaultKey)
	} else {
		prompt = fmt.Sprintf("Select profile [1-%d] (Esc to cancel)", len(items))
	}
	fmt.Printf("%s ", styleDim(prompt+":"))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if isEscCancelInput(line) {
		return codexProfile{}, false
	}
	if line == "" && defaultKey != "" {
		if profile, ok := codexProfileByKey(defaultKey); ok {
			return profile, true
		}
	}
	if line == "" {
		return codexProfile{}, false
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(items) {
		fmt.Println(styleDim("invalid selection"))
		return codexProfile{}, false
	}
	profile, ok := codexProfileByKey(items[idx-1].ID)
	if !ok {
		fmt.Println(styleDim("invalid selection"))
		return codexProfile{}, false
	}
	return profile, true
}

func cmdCodexLogs(args []string) {
	if len(args) < 1 {
		printUsage("usage: si logs <name> [--tail N]")
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
		printUsage("usage: si tail <name> [--tail N]")
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
		printUsage("usage: si clone <name> <Org/Repo> [--gh-pat TOKEN]")
		return
	}
	name := args[0]
	repo := strings.TrimSpace(args[1])
	fs := flag.NewFlagSet("clone", flag.ExitOnError)
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
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	removeVolumes := fs.Bool("volumes", false, "remove codex/gh volumes too")
	all := fs.Bool("all", false, "remove all codex containers (prompts for confirmation)")
	_ = fs.Parse(args)
	name := ""
	if *all {
		if fs.NArg() > 0 {
			printUsage("usage: si remove [--all] [--volumes] [name]")
			return
		}
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
			fmt.Println(styleDim("no codex containers found"))
			return
		}
		names := make([]string, 0, len(containers))
		for _, c := range containers {
			n := strings.TrimPrefix(c.Names[0], "/")
			if strings.TrimSpace(n) != "" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		confirmed, ok := confirmYN(fmt.Sprintf("Remove ALL codex containers (%d): %s?", len(names), strings.Join(names, ", ")), false)
		if !ok || !confirmed {
			infof("canceled")
			return
		}
		removed := 0
		for _, c := range containers {
			if err := client.RemoveContainer(ctx, c.ID, true); err != nil {
				warnf("remove container %s failed: %v", strings.TrimPrefix(c.Names[0], "/"), err)
				continue
			}
			removed++
			if *removeVolumes {
				slug := codexContainerSlug(strings.TrimPrefix(c.Names[0], "/"))
				if strings.TrimSpace(slug) == "" {
					continue
				}
				codexVol := "si-codex-" + slug
				ghVol := "si-gh-" + slug
				if err := client.RemoveVolume(ctx, codexVol, true); err != nil {
					warnf("codex volume remove failed: %v", err)
				}
				if err := client.RemoveVolume(ctx, ghVol, true); err != nil {
					warnf("gh volume remove failed: %v", err)
				}
			}
		}
		successf("removed %d codex containers", removed)
		return
	}

	if fs.NArg() < 1 {
		selectedName, ok := selectCodexContainerFromList("remove")
		if !ok {
			return
		}
		name = selectedName
	} else {
		name = fs.Arg(0)
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
		fmt.Printf("%s %s\n", styleError("codex container not found:"), containerName)
		return
	}
	if err := client.RemoveContainer(ctx, id, true); err != nil {
		fatal(err)
	}
	if *removeVolumes {
		slug := codexContainerSlug(containerName)
		codexVol := "si-codex-" + slug
		ghVol := "si-gh-" + slug
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
		printUsage("usage: si stop <name>")
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
		printUsage("usage: si start <name>")
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

func codexDefaultProfileKey(settings Settings) string {
	key := strings.TrimSpace(settings.Codex.Profile)
	if key == "" {
		key = strings.TrimSpace(settings.Codex.Profiles.Active)
	}
	return key
}

func codexContainerSlug(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "/")
	return strings.TrimPrefix(name, "si-codex-")
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
	// #nosec G304 -- hostPath is fixed to user config location under home directory.
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if !os.IsNotExist(err) {
			warnf("codex config copy skipped: %v", err)
		}
	} else {
		copied := false
		for _, target := range codexContainerConfigTargets() {
			_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", filepath.Dir(target.Path)}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
			if err := client.CopyFileToContainer(ctx, containerID, target.Path, data, 0o600); err != nil {
				continue
			}
			copied = true
			_ = client.Exec(ctx, containerID, []string{"chown", target.Owner, target.Path}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
		}
		if !copied {
			warnf("codex config copy failed for container %s", strings.TrimSpace(containerID))
		}
	}
	ensureBrowserMCPForContainer(ctx, client, containerID)
}

func ensureBrowserMCPForContainer(ctx context.Context, client *shared.Client, containerID string) {
	url := strings.TrimSpace(codexBrowserMCPURL())
	if url == "" || client == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	rootCmd := []string{"bash", "-lc", fmt.Sprintf("if command -v codex >/dev/null 2>&1; then CODEX_HOME=/root/.codex codex mcp add %s --url %s >/dev/null 2>&1 || true; fi", quoteSingle(codexBrowserMCPName), quoteSingle(url))}
	_ = client.Exec(ctx, containerID, rootCmd, shared.ExecOptions{}, nil, io.Discard, io.Discard)

	siCmd := []string{"bash", "-lc", fmt.Sprintf("if command -v codex >/dev/null 2>&1; then CODEX_HOME=/home/si/.codex codex mcp add %s --url %s >/dev/null 2>&1 || true; fi", quoteSingle(codexBrowserMCPName), quoteSingle(url))}
	_ = client.Exec(ctx, containerID, siCmd, shared.ExecOptions{}, nil, io.Discard, io.Discard)
}

func codexBrowserMCPURL() string {
	if envIsTrue("SI_BROWSER_MCP_DISABLED") {
		return ""
	}
	if explicit := strings.TrimSpace(os.Getenv("SI_BROWSER_MCP_URL_INTERNAL")); explicit != "" {
		return explicit
	}
	if explicit := strings.TrimSpace(os.Getenv("SI_BROWSER_MCP_URL")); explicit != "" {
		return explicit
	}
	containerName := strings.TrimSpace(envOr("SI_BROWSER_CONTAINER", defaultBrowserContainer))
	port := envOrInt("SI_BROWSER_MCP_PORT", defaultBrowserMCPPort)
	if containerName == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/mcp", containerName, port)
}

func envIsTrue(key string) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch val {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type codexConfigTarget struct {
	Path  string
	Owner string
}

func codexContainerConfigTargets() []codexConfigTarget {
	return []codexConfigTarget{
		{Path: "/home/si/.codex/config.toml", Owner: "si:si"},
		{Path: "/root/.codex/config.toml", Owner: "root:root"},
	}
}

func seedCodexAuth(ctx context.Context, client *shared.Client, containerID string, cleanSlate bool, profile codexProfile) {
	if cleanSlate {
		return
	}
	if strings.TrimSpace(profile.ID) == "" {
		return
	}
	hostPath, err := codexProfileAuthPath(profile)
	if err != nil {
		warnf("codex auth copy skipped: %v", err)
		return
	}
	// #nosec G304 -- hostPath is derived from local profile auth path.
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		warnf("codex auth copy skipped: %v", err)
		return
	}
	copied := false
	for _, destPath := range codexContainerAuthPaths() {
		_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", filepath.Dir(destPath)}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
		if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
			warnf("codex auth copy failed (%s): %v", destPath, err)
			continue
		}
		copied = true
		owner := "si:si"
		if strings.HasPrefix(destPath, "/root/") {
			owner = "root:root"
		}
		_ = client.Exec(ctx, containerID, []string{"chown", owner, destPath}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	}
	if !copied {
		return
	}
}

func cacheCodexAuthFromContainer(ctx context.Context, client *shared.Client, containerID string, profile codexProfile) error {
	if strings.TrimSpace(profile.ID) == "" {
		return fmt.Errorf("profile id required")
	}
	data, err := readCodexAuthFromContainer(ctx, client, containerID)
	if err != nil {
		if isMissingContainerFile(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return cacheCodexAuthBytes(profile, data)
}

func cacheCodexAuthBytes(profile codexProfile, data []byte) error {
	if strings.TrimSpace(profile.ID) == "" {
		return fmt.Errorf("profile id required")
	}
	if len(data) == 0 {
		return nil
	}
	dir, err := ensureCodexProfileDir(profile)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "auth.json")
	tmp, err := os.CreateTemp(dir, "auth-*.json")
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

func codexContainerAuthPaths() []string {
	return []string{
		"/home/si/.codex/auth.json",
		"/root/.codex/auth.json",
	}
}

func readCodexAuthFromContainer(ctx context.Context, client *shared.Client, containerID string) ([]byte, error) {
	var lastErr error
	for _, candidate := range codexContainerAuthPaths() {
		data, err := client.ReadFileFromContainer(ctx, containerID, candidate)
		if err == nil {
			return data, nil
		}
		if isMissingContainerFile(err) {
			lastErr = err
			continue
		}
		return nil, err
	}
	if lastErr != nil {
		return nil, os.ErrNotExist
	}
	return nil, os.ErrNotExist
}

func readCodexAuthFromVolume(ctx context.Context, client *shared.Client, volumeName string) ([]byte, error) {
	if client == nil {
		return nil, errors.New("docker client required")
	}
	volumeName = strings.TrimSpace(volumeName)
	if volumeName == "" {
		return nil, errors.New("codex auth volume required")
	}
	image := strings.TrimSpace(envOr("SI_CODEX_IMAGE", "aureuma/si:local"))
	if image == "" {
		return nil, errors.New("codex image required")
	}
	tmpName := fmt.Sprintf("si-codex-authsync-%d", time.Now().UnixNano())
	cfg := &container.Config{
		Image: image,
		Cmd:   []string{"bash", "-lc", "sleep infinity"},
	}
	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   "/home/si/.codex",
				ReadOnly: true,
			},
		},
	}
	netCfg := &network.NetworkingConfig{}
	id, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, tmpName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = client.RemoveContainer(context.Background(), id, true)
	}()
	if err := client.StartContainer(ctx, id); err != nil {
		return nil, err
	}
	return readCodexAuthFromContainer(ctx, client, id)
}

func codexAuthVolumeFromContainerInfo(info *types.ContainerJSON) string {
	if info == nil {
		return ""
	}
	for _, point := range info.Mounts {
		dest := strings.TrimSpace(point.Destination)
		if dest != "/home/si/.codex" && dest != "/root/.codex" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(string(point.Type)), "volume") {
			if name := strings.TrimSpace(point.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func isMissingContainerFile(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "could not find the file") || strings.Contains(msg, "no such file")
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
		"detach":        true,
		"clean-slate":   true,
		"docker-socket": true,
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
			continue
		}
		if boolFlags[flagName] {
			if i+1 < len(args) {
				next := strings.TrimSpace(args[i+1])
				if isBoolLiteral(next) {
					out[len(out)-1] = arg + "=" + strings.ToLower(next)
					i++
				}
			}
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
