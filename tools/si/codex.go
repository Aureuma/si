package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	shared "si/agents/shared/docker"

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

func dispatchCodexCommand(cmd string, args []string) bool {
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
	case "warm-weekly":
		cmdWarmWeekly(args)
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
	fs := flag.NewFlagSet("spawn", flag.ExitOnError)
	flags := addCodexSpawnFlags(fs)
	nameArg, filtered := splitNameAndFlags(args, codexSpawnBoolFlags())
	fs.Parse(filtered)
	settings := loadSettingsOrDefault()

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
	if !flagProvided(args, "gh-volume") && strings.TrimSpace(settings.Codex.GHVolume) != "" {
		*flags.ghVolume = strings.TrimSpace(settings.Codex.GHVolume)
	}
	if !flagProvided(args, "detach") && settings.Codex.Detach != nil {
		*flags.detach = *settings.Codex.Detach
	}
	if !flagProvided(args, "clean-slate") && settings.Codex.CleanSlate != nil {
		*flags.cleanSlate = *settings.Codex.CleanSlate
	}

	name := nameArg
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			printUsage("usage: si spawn <name> [--repo Org/Repo] [--gh-pat TOKEN]")
			return
		}
		if !explicitProfile && len(codexProfileSummaries()) > 0 {
			defaultProfileKey := strings.TrimSpace(settings.Codex.Profile)
			if defaultProfileKey == "" {
				defaultProfileKey = strings.TrimSpace(settings.Codex.Profiles.Active)
			}
			selected, ok := selectCodexProfile("spawn", defaultProfileKey)
			if !ok {
				return
			}
			*flags.profile = selected.ID
			explicitProfile = true
		}
		fmt.Printf("%s ", styleDim("Container name:"))
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			fatal(err)
		}
		name = strings.TrimSpace(line)
		if name == "" {
			return
		}
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

	existingID, info, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if existingID != "" {
		var profile *codexProfile
		if explicitProfile && strings.TrimSpace(*flags.profile) != "" {
			parsed, err := requireCodexProfile(*flags.profile)
			if err != nil {
				fatal(err)
			}
			profile = &parsed
		}
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
		if !*flags.cleanSlate {
			if identity, ok := hostGitIdentity(); ok {
				seedGitIdentity(ctx, client, existingID, "si", "/home/si", identity)
			}
		}
		infof("codex container %s already exists", containerName)
		return
	}

	if !explicitProfile {
		defaultProfileKey := strings.TrimSpace(settings.Codex.Profile)
		if defaultProfileKey == "" {
			defaultProfileKey = strings.TrimSpace(settings.Codex.Profiles.Active)
		}
		if defaultProfileKey != "" && !term.IsTerminal(int(os.Stdin.Fd())) {
			*flags.profile = defaultProfileKey
		} else if len(codexProfileSummaries()) > 0 {
			selected, ok := selectCodexProfile("spawn", defaultProfileKey)
			if !ok {
				return
			}
			*flags.profile = selected.ID
		} else if !term.IsTerminal(int(os.Stdin.Fd())) && defaultProfileKey == "" {
			printUsage("usage: si spawn <name> [--profile <profile>] [--repo Org/Repo] [--gh-pat TOKEN]")
			return
		}
	}

	var profile *codexProfile
	if strings.TrimSpace(*flags.profile) != "" {
		parsed, err := requireCodexProfile(*flags.profile)
		if err != nil {
			fatal(err)
		}
		profile = &parsed
	}

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
	if profile != nil {
		labels[codexProfileLabelKey] = profile.ID
	}

	env := []string{
		"HOME=/home/si",
		"CODEX_HOME=/home/si/.codex",
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
		{Type: mount.TypeVolume, Source: ghVol, Target: "/home/si/.config/gh"},
		{Type: mount.TypeBind, Source: *flags.workspaceHost, Target: workspaceTarget},
	}
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
	addCodexSpawnFlags(fs)
	removeVolumes := fs.Bool("volumes", false, "remove codex/gh volumes too")
	nameArg, filtered := splitNameAndFlags(args, codexRespawnBoolFlags())
	_ = fs.Parse(filtered)
	if nameArg == "" {
		selectedName, ok := selectCodexContainer("respawn", true)
		if !ok {
			return
		}
		nameArg = selectedName
	}
	if !flagProvided(args, "profile") {
		selected, ok := selectCodexProfile("respawn", "")
		if !ok {
			return
		}
		filtered = append(filtered, "--profile", selected.ID)
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
	fmt.Printf("%s %s %s\n",
		padRightANSI(styleHeading("CONTAINER"), 28),
		padRightANSI(styleHeading("STATE"), 10),
		padRightANSI(styleHeading("IMAGE"), 20),
	)
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		image := codexImageDisplay(c.Image)
		fmt.Printf("%s %s %s\n",
			padRightANSI(name, 28),
			padRightANSI(styleStatus(c.State), 10),
			padRightANSI(image, 20),
		)
	}
}

func cmdCodexExec(args []string) {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	oneOff := fs.Bool("one-off", false, "run a one-off codex exec in an isolated container")
	promptFlag := fs.String("prompt", "", "prompt to execute (one-off mode)")
	outputOnly := fs.Bool("output-only", false, "print only the Codex response (one-off mode)")
	noMcp := fs.Bool("no-mcp", false, "disable MCP servers (one-off mode)")
	profileKey := fs.String("profile", "", "codex profile name/email (one-off mode)")
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "aureuma/si:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	codexVolume := fs.String("codex-volume", envOr("SI_CODEX_EXEC_VOLUME", ""), "codex volume name")
	ghVolume := fs.String("gh-volume", "", "gh config volume name")
	model := fs.String("model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	effort := fs.String("effort", envOr("CODEX_REASONING_EFFORT", "medium"), "codex reasoning effort")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in one-off containers")
	keep := fs.Bool("keep", false, "keep the one-off container after execution")
	envs := multiFlag{}
	fs.Var(&envs, "env", "env var (repeatable KEY=VALUE)")
	_ = fs.Parse(args)

	settings := loadSettingsOrDefault()
	if !flagProvided(args, "docker-socket") && settings.Codex.DockerSocket != nil {
		*dockerSocket = *settings.Codex.DockerSocket
	}

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
			GHVolume:      strings.TrimSpace(*ghVolume),
			Env:           envs,
			Model:         strings.TrimSpace(*model),
			Effort:        strings.TrimSpace(*effort),
			DisableMCP:    *noMcp,
			OutputOnly:    *outputOnly,
			KeepContainer: *keep,
			DockerSocket:  *dockerSocket,
			Profile:       profile,
		}
		if err := runCodexExecOneOff(opts); err != nil {
			fatal(err)
		}
		return
	}

	if len(rest) < 1 {
		name, ok := selectCodexContainer("run", true)
		if !ok {
			return
		}
		rest = []string{name}
	}
	name := rest[0]
	containerName := codexContainerName(name)
	profileID := ""
	profileName := ""
	{
		client, err := shared.NewClient()
		if err == nil {
			ctx := context.Background()
			if id, info, err := client.ContainerByName(ctx, containerName); err == nil && id != "" {
				if info != nil && info.Config != nil {
					profileID = strings.TrimSpace(info.Config.Labels[codexProfileLabelKey])
				}
				if profileID != "" {
					if prof, ok := codexProfileByKey(profileID); ok {
						profileName = prof.Name
					} else {
						profileName = profileID
					}
				}
			}
			client.Close()
		}
	}
	cmd := rest[1:]
	if len(cmd) == 0 {
		rc := buildShellRC(settings)
		cmd = []string{"bash", "-lc", fmt.Sprintf(`rc="/tmp/si-shellrc"
cat > "$rc" <<'EOF'
%s
EOF
printf '\033]0;%%s\007' "${SI_TERM_TITLE:-}"
exec bash --rcfile "$rc" -i`, rc)}
	}
	execArgs := []string{"exec"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		execArgs = append(execArgs, "-it")
	} else {
		execArgs = append(execArgs, "-i")
	}
	execArgs = append(execArgs, "-e", "SI_TERM_TITLE="+name)
	if profileID != "" {
		execArgs = append(execArgs, "-e", "SI_CODEX_PROFILE_ID="+profileID)
	}
	if profileName != "" {
		execArgs = append(execArgs, "-e", "SI_CODEX_PROFILE_NAME="+profileName)
	}
	execArgs = append(execArgs, containerName)
	execArgs = append(execArgs, cmd...)
	if err := execDockerCLI(execArgs...); err != nil {
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
		fmt.Println(styleDim("no codex containers found (run: si spawn <name>)"))
		return "", false
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Names[0] < containers[j].Names[0]
	})

	nameWidth := 0
	items := make([]codexContainerItem, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		if len(name) > nameWidth {
			nameWidth = len(name)
		}
		items = append(items, codexContainerItem{
			Name:  name,
			State: c.State,
			Image: codexImageDisplay(c.Image),
		})
	}
	if nameWidth < 10 {
		nameWidth = 10
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleHeading("Available codex containers:"))
		for i, item := range items {
			fmt.Printf("  %2d) %s %s %s\n",
				i+1,
				padRightANSI(item.Name, nameWidth),
				padRightANSI(styleStatus(item.State), 10),
				item.Image,
			)
		}
		hint := "re-run with: si " + action
		if nameHint {
			hint += " <name>"
		}
		fmt.Println(styleDim(hint))
		return "", false
	}

	fmt.Println(styleHeading("Available codex containers:"))
	for i, item := range items {
		fmt.Printf("  %2d) %s %s %s\n",
			i+1,
			padRightANSI(item.Name, nameWidth),
			padRightANSI(styleStatus(item.State), 10),
			item.Image,
		)
	}

	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select container [1-%d] (or press Enter to cancel):", len(items))))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fatal(err)
	}
	line = strings.TrimSpace(line)
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
		fmt.Println(styleDim("no codex containers found (run: si spawn <name>)"))
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

	fmt.Printf("%s %s %s\n",
		padRightANSI(styleHeading("CONTAINER"), 28),
		padRightANSI(styleHeading("STATE"), 10),
		padRightANSI(styleHeading("IMAGE"), 20),
	)
	for _, item := range items {
		fmt.Printf("%s %s %s\n",
			padRightANSI(item.Name, 28),
			padRightANSI(styleStatus(item.State), 10),
			padRightANSI(item.Image, 20),
		)
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleDim("re-run with: si " + action + " <name>"))
		return "", false
	}

	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select container [1-%d] or name (or press Enter to cancel):", len(items))))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fatal(err)
	}
	line = strings.TrimSpace(line)
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

	if profile == nil {
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
	printCodexProfilesTable(items, false)

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(styleDim("re-run with: si " + action + " <profile>"))
		return codexProfile{}, false
	}

	prompt := fmt.Sprintf("Select profile [1-%d]", len(items))
	if defaultKey != "" {
		prompt = fmt.Sprintf("Select profile [1-%d] (default %s)", len(items), defaultKey)
	}
	fmt.Printf("%s ", styleDim(prompt+":"))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fatal(err)
	}
	line = strings.TrimSpace(line)
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
	_ = fs.Parse(args)
	name := ""
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
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		warnf("codex config copy skipped: %v", err)
		return
	}
	const destPath = "/home/si/.codex/config.toml"
	_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", filepath.Dir(destPath)}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
		warnf("codex config copy failed: %v", err)
		return
	}
	_ = client.Exec(ctx, containerID, []string{"chown", "si:si", destPath}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
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
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		warnf("codex auth copy skipped: %v", err)
		return
	}
	const destPath = "/home/si/.codex/auth.json"
	_ = client.Exec(ctx, containerID, []string{"mkdir", "-p", filepath.Dir(destPath)}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
	if err := client.CopyFileToContainer(ctx, containerID, destPath, data, 0o600); err != nil {
		warnf("codex auth copy failed: %v", err)
		return
	}
	_ = client.Exec(ctx, containerID, []string{"chown", "si:si", destPath}, shared.ExecOptions{}, nil, io.Discard, io.Discard)
}

func cacheCodexAuthFromContainer(ctx context.Context, client *shared.Client, containerID string, profile codexProfile) error {
	if strings.TrimSpace(profile.ID) == "" {
		return fmt.Errorf("profile id required")
	}
	data, err := client.ReadFileFromContainer(ctx, containerID, "/home/si/.codex/auth.json")
	if err != nil {
		if isMissingContainerFile(err) {
			return nil
		}
		return err
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
