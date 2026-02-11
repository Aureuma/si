package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

const (
	DefaultNetwork = "si"
	LabelApp       = "app"
	LabelDyad      = "si.dyad"
	LabelMember    = "si.member"
	LabelRole      = "si.role"
)

const DyadAppLabel = "si-dyad"

type DyadOptions struct {
	Dyad              string
	Role              string
	ActorImage        string
	CriticImage       string
	CodexModel        string
	CodexEffortActor  string
	CodexEffortCritic string
	CodexModelLow     string
	CodexModelMedium  string
	CodexModelHigh    string
	CodexEffortLow    string
	CodexEffortMedium string
	CodexEffortHigh   string
	WorkspaceHost     string
	ConfigsHost       string
	CodexVolume       string
	Network           string
	ForwardPorts      string
	DockerSocket      bool
	LoopEnabled       *bool
	LoopGoal          string
	LoopSeedPrompt    string
	LoopMaxTurns      int
	LoopSleepSeconds  int
	LoopStartupDelay  int
	LoopTurnTimeout   int
	LoopRetryMax      int
	LoopRetryBase     int
	LoopPromptLines   int
	LoopAllowMCP      *bool
	LoopTmuxCapture   string
	LoopPausePoll     int
}

type ContainerSpec struct {
	Name          string
	Config        *container.Config
	HostConfig    *container.HostConfig
	NetworkConfig *network.NetworkingConfig
}

func DyadContainerName(dyad, member string) string {
	dyad = strings.TrimSpace(dyad)
	member = strings.TrimSpace(member)
	if dyad == "" || member == "" {
		return ""
	}
	return "si-" + member + "-" + dyad
}

func (c *Client) DyadStatus(ctx context.Context, dyad string) (bool, bool, error) {
	if strings.TrimSpace(dyad) == "" {
		return false, false, errors.New("dyad required")
	}
	actorName := DyadContainerName(dyad, "actor")
	criticName := DyadContainerName(dyad, "critic")
	actorID, actorInfo, err := c.ContainerByName(ctx, actorName)
	if err != nil {
		return false, false, err
	}
	criticID, criticInfo, err := c.ContainerByName(ctx, criticName)
	if err != nil {
		return false, false, err
	}
	if actorID == "" || criticID == "" {
		return false, false, nil
	}
	actorRunning := actorInfo != nil && actorInfo.State != nil && actorInfo.State.Running
	criticRunning := criticInfo != nil && criticInfo.State != nil && criticInfo.State.Running
	return true, actorRunning && criticRunning, nil
}

func (c *Client) RestartDyad(ctx context.Context, dyad string) error {
	actorName := DyadContainerName(dyad, "actor")
	criticName := DyadContainerName(dyad, "critic")
	actorID, _, _ := c.ContainerByName(ctx, actorName)
	criticID, _, _ := c.ContainerByName(ctx, criticName)
	if actorID == "" && criticID == "" {
		return fmt.Errorf("dyad %s not found", dyad)
	}
	timeout := 10 * time.Second
	if actorID != "" {
		if err := c.RestartContainer(ctx, actorID, timeout); err != nil {
			return err
		}
	}
	if criticID != "" {
		if err := c.RestartContainer(ctx, criticID, timeout); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RemoveDyad(ctx context.Context, dyad string, force bool) error {
	actorName := DyadContainerName(dyad, "actor")
	criticName := DyadContainerName(dyad, "critic")
	actorID, _, _ := c.ContainerByName(ctx, actorName)
	criticID, _, _ := c.ContainerByName(ctx, criticName)
	if actorID != "" {
		if err := c.RemoveContainer(ctx, actorID, force); err != nil {
			return err
		}
	}
	if criticID != "" {
		if err := c.RemoveContainer(ctx, criticID, force); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) EnsureDyad(ctx context.Context, opts DyadOptions) (string, string, error) {
	actorName := DyadContainerName(opts.Dyad, "actor")
	criticName := DyadContainerName(opts.Dyad, "critic")
	if actorName == "" || criticName == "" {
		return "", "", errors.New("dyad name required")
	}
	actorID, actorInfo, err := c.ContainerByName(ctx, actorName)
	if err != nil {
		return "", "", err
	}
	criticID, criticInfo, err := c.ContainerByName(ctx, criticName)
	if err != nil {
		return "", "", err
	}
	if actorID == "" || criticID == "" {
		return c.CreateDyad(ctx, opts)
	}
	// Backward compatibility: recreate older dyads that were created before host
	// ~/.si mounts were added, so full `si`/`si vault` works inside both members.
	if !HasHostSiMount(actorInfo, "/root") || !HasHostSiMount(criticInfo, "/root") {
		if actorID != "" {
			if err := c.RemoveContainer(ctx, actorID, true); err != nil {
				return "", "", err
			}
		}
		if criticID != "" {
			if err := c.RemoveContainer(ctx, criticID, true); err != nil {
				return "", "", err
			}
		}
		return c.CreateDyad(ctx, opts)
	}
	if actorInfo != nil && actorInfo.State != nil && !actorInfo.State.Running {
		if err := c.StartContainer(ctx, actorID); err != nil {
			return "", "", err
		}
	}
	if criticInfo != nil && criticInfo.State != nil && !criticInfo.State.Running {
		if err := c.StartContainer(ctx, criticID); err != nil {
			return "", "", err
		}
	}
	return actorID, criticID, nil
}

func (c *Client) CreateDyad(ctx context.Context, opts DyadOptions) (string, string, error) {
	actorSpec, criticSpec, err := BuildDyadSpecs(opts)
	if err != nil {
		return "", "", err
	}
	networkName := ""
	for name := range actorSpec.NetworkConfig.EndpointsConfig {
		networkName = name
		break
	}
	if networkName != "" {
		_, _ = c.EnsureNetwork(ctx, networkName, nil)
	}

	actorID, err := c.CreateContainer(ctx, actorSpec.Config, actorSpec.HostConfig, actorSpec.NetworkConfig, actorSpec.Name)
	if err != nil {
		return "", "", err
	}
	if err := c.StartContainer(ctx, actorID); err != nil {
		return "", "", err
	}

	criticID, err := c.CreateContainer(ctx, criticSpec.Config, criticSpec.HostConfig, criticSpec.NetworkConfig, criticSpec.Name)
	if err != nil {
		return "", "", err
	}
	if err := c.StartContainer(ctx, criticID); err != nil {
		return "", "", err
	}
	return actorID, criticID, nil
}

func BuildDyadSpecs(opts DyadOptions) (ContainerSpec, ContainerSpec, error) {
	if strings.TrimSpace(opts.Dyad) == "" {
		return ContainerSpec{}, ContainerSpec{}, errors.New("dyad name required")
	}
	if strings.TrimSpace(opts.Role) == "" {
		opts.Role = "generic"
	}
	if strings.TrimSpace(opts.ActorImage) == "" || strings.TrimSpace(opts.CriticImage) == "" {
		return ContainerSpec{}, ContainerSpec{}, errors.New("actor and critic images required")
	}
	if strings.TrimSpace(opts.Network) == "" {
		opts.Network = DefaultNetwork
	}
	if strings.TrimSpace(opts.CodexVolume) == "" {
		opts.CodexVolume = "si-codex-" + opts.Dyad
	}
	if strings.TrimSpace(opts.WorkspaceHost) == "" {
		return ContainerSpec{}, ContainerSpec{}, errors.New("workspace host path required")
	}
	if strings.TrimSpace(opts.ConfigsHost) == "" {
		opts.ConfigsHost = filepath.Join(opts.WorkspaceHost, "configs")
	}

	labels := map[string]string{
		LabelApp:  DyadAppLabel,
		LabelDyad: opts.Dyad,
		LabelRole: opts.Role,
	}

	actorLabels := cloneLabels(labels)
	actorLabels[LabelMember] = "actor"
	criticLabels := cloneLabels(labels)
	criticLabels[LabelMember] = "critic"

	socketMount := mount.Mount{}
	hasSocket := false
	if opts.DockerSocket {
		socketMount, hasSocket = DockerSocketMount()
	}

	actorEnv := buildDyadEnv(opts, "actor", opts.CodexEffortActor)
	criticEnv := buildDyadEnv(opts, "critic", opts.CodexEffortCritic)

	actorEnv = append(actorEnv,
		"HOME=/root",
		"CODEX_HOME=/root/.codex",
	)

	actorEnv = appendOptionalEnv(actorEnv, "CODEX_MODEL_LOW", opts.CodexModelLow)
	actorEnv = appendOptionalEnv(actorEnv, "CODEX_MODEL_MEDIUM", opts.CodexModelMedium)
	actorEnv = appendOptionalEnv(actorEnv, "CODEX_MODEL_HIGH", opts.CodexModelHigh)
	actorEnv = appendOptionalEnv(actorEnv, "CODEX_REASONING_EFFORT_LOW", opts.CodexEffortLow)
	actorEnv = appendOptionalEnv(actorEnv, "CODEX_REASONING_EFFORT_MEDIUM", opts.CodexEffortMedium)
	actorEnv = appendOptionalEnv(actorEnv, "CODEX_REASONING_EFFORT_HIGH", opts.CodexEffortHigh)

	criticEnv = appendOptionalEnv(criticEnv, "CODEX_MODEL_LOW", opts.CodexModelLow)
	criticEnv = appendOptionalEnv(criticEnv, "CODEX_MODEL_MEDIUM", opts.CodexModelMedium)
	criticEnv = appendOptionalEnv(criticEnv, "CODEX_MODEL_HIGH", opts.CodexModelHigh)
	criticEnv = appendOptionalEnv(criticEnv, "CODEX_REASONING_EFFORT_LOW", opts.CodexEffortLow)
	criticEnv = appendOptionalEnv(criticEnv, "CODEX_REASONING_EFFORT_MEDIUM", opts.CodexEffortMedium)
	criticEnv = appendOptionalEnv(criticEnv, "CODEX_REASONING_EFFORT_HIGH", opts.CodexEffortHigh)

	actorExposed, actorBindings, err := parseForwardPorts(opts.ForwardPorts)
	if err != nil {
		return ContainerSpec{}, ContainerSpec{}, err
	}

	actorConfig := &container.Config{
		Image:        opts.ActorImage,
		WorkingDir:   "/workspace",
		Env:          actorEnv,
		Labels:       actorLabels,
		Entrypoint:   []string{"tini", "-s", "--", "/usr/local/bin/si-codex-init"},
		Cmd:          []string{"--exec", "tail", "-f", "/dev/null"},
		ExposedPorts: actorExposed,
		User:         "root",
	}
	actorMirrorTarget := ""
	if target, ok := InferWorkspaceTarget(opts.WorkspaceHost); ok && target != "/workspace" {
		actorMirrorTarget = target
	}
	actorMounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: opts.CodexVolume, Target: "/root/.codex"},
	}
	actorMounts = append(actorMounts, BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          opts.WorkspaceHost,
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  actorMirrorTarget,
		ContainerHome:          "/root",
		IncludeHostSi:          true,
	})...)
	if hasSocket {
		actorMounts = append(actorMounts, socketMount)
	}
	actorHostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        actorMounts,
		PortBindings:  actorBindings,
	}

	criticEnv = append(criticEnv,
		"ACTOR_CONTAINER="+DyadContainerName(opts.Dyad, "actor"),
		"HOME=/root",
		"CODEX_HOME=/root/.codex",
	)

	criticConfig := &container.Config{
		Image:      opts.CriticImage,
		Env:        criticEnv,
		Labels:     criticLabels,
		Entrypoint: []string{"tini", "-s", "--"},
		Cmd:        []string{"critic"},
		User:       "root",
	}
	criticMirrorTarget := ""
	if target, ok := InferWorkspaceTarget(opts.WorkspaceHost); ok && target != "/workspace" {
		criticMirrorTarget = target
	}
	criticMounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: opts.CodexVolume, Target: "/root/.codex"},
		{Type: mount.TypeBind, Source: opts.ConfigsHost, Target: "/configs", ReadOnly: true},
	}
	criticMounts = append(criticMounts, BuildContainerCoreMounts(ContainerCoreMountPlan{
		WorkspaceHost:          opts.WorkspaceHost,
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  criticMirrorTarget,
		ContainerHome:          "/root",
		IncludeHostSi:          true,
	})...)
	if hasSocket {
		criticMounts = append(criticMounts, socketMount)
	}
	criticHostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        criticMounts,
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.Network: {
				Aliases: []string{actorContainerAlias(opts.Dyad, "actor")},
			},
		},
	}
	criticNetCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			opts.Network: {
				Aliases: []string{actorContainerAlias(opts.Dyad, "critic")},
			},
		},
	}

	return ContainerSpec{
			Name:          DyadContainerName(opts.Dyad, "actor"),
			Config:        actorConfig,
			HostConfig:    actorHostConfig,
			NetworkConfig: netCfg,
		}, ContainerSpec{
			Name:          DyadContainerName(opts.Dyad, "critic"),
			Config:        criticConfig,
			HostConfig:    criticHostConfig,
			NetworkConfig: criticNetCfg,
		}, nil
}

func buildDyadEnv(opts DyadOptions, member, effort string) []string {
	termTitle := dyadTermTitle(opts.Dyad, member)
	env := []string{
		"ROLE=" + opts.Role,
		"DYAD_NAME=" + opts.Dyad,
		"DYAD_MEMBER=" + member,
		"CODEX_INIT_FORCE=1",
		"SI_TERM_TITLE=" + termTitle,
		// Ensure tmux + shell render Unicode (emoji) reliably.
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	model := strings.TrimSpace(opts.CodexModel)
	if model != "" {
		env = append(env, "CODEX_MODEL="+model)
	}
	if effort != "" {
		env = append(env, "CODEX_REASONING_EFFORT="+effort)
	}
	uid := os.Getuid()
	gid := os.Getgid()
	if raw := strings.TrimSpace(os.Getenv("SI_HOST_UID")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			uid = parsed
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SI_HOST_GID")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			gid = parsed
		}
	}
	if uid > 0 && gid > 0 {
		env = append(env, fmt.Sprintf("SI_HOST_UID=%d", uid))
		env = append(env, fmt.Sprintf("SI_HOST_GID=%d", gid))
	}
	if member == "critic" {
		env = appendOptionalBoolEnv(env, "DYAD_LOOP_ENABLED", opts.LoopEnabled)
		env = appendOptionalEnv(env, "DYAD_LOOP_GOAL", opts.LoopGoal)
		env = appendOptionalEnv(env, "DYAD_LOOP_SEED_CRITIC_PROMPT", opts.LoopSeedPrompt)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_MAX_TURNS", opts.LoopMaxTurns)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_SLEEP_SECONDS", opts.LoopSleepSeconds)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_STARTUP_DELAY_SECONDS", opts.LoopStartupDelay)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_TURN_TIMEOUT_SECONDS", opts.LoopTurnTimeout)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_RETRY_MAX", opts.LoopRetryMax)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_RETRY_BASE_SECONDS", opts.LoopRetryBase)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_PROMPT_LINES", opts.LoopPromptLines)
		env = appendOptionalBoolEnv(env, "DYAD_LOOP_ALLOW_MCP_STARTUP", opts.LoopAllowMCP)
		env = appendOptionalEnv(env, "DYAD_LOOP_TMUX_CAPTURE", opts.LoopTmuxCapture)
		env = appendOptionalIntEnv(env, "DYAD_LOOP_PAUSE_POLL_SECONDS", opts.LoopPausePoll)
		// Host overrides (useful for offline testing and hard enforcement tweaks).
		env = appendHostEnvIfSet(env, "DYAD_LOOP_STRICT_REPORT")
		env = appendHostEnvIfSet(env, "DYAD_LOOP_TMUX_CAPTURE_LINES")
		env = appendHostEnvIfSet(env, "DYAD_CODEX_START_CMD")
		env = appendHostEnvIfSet(env, "DYAD_STATE_DIR")
	}
	// Forward fake-codex tuning knobs so `DYAD_CODEX_START_CMD=/workspace/tools/dyad/fake-codex.sh`
	// can be used for offline dyad smoke tests with long/slow outputs.
	for _, key := range []string{
		"FAKE_CODEX_PROMPT_CHAR",
		"FAKE_CODEX_DELAY_SECONDS",
		"FAKE_CODEX_LONG_LINES",
		"FAKE_CODEX_LONG_IF_CONTAINS",
		"FAKE_CODEX_NO_MARKERS",
	} {
		env = appendHostEnvIfSet(env, key)
	}
	return env
}

func dyadTermTitle(dyad string, member string) string {
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

func appendOptionalEnv(env []string, key, val string) []string {
	if strings.TrimSpace(val) == "" {
		return env
	}
	return append(env, key+"="+strings.TrimSpace(val))
}

func appendOptionalIntEnv(env []string, key string, val int) []string {
	// Dyad loop settings treat 0 as a meaningful value (e.g. no sleep, no startup delay).
	// Negative values are treated as "unset".
	if strings.TrimSpace(key) == "" || val < 0 {
		return env
	}
	return append(env, fmt.Sprintf("%s=%d", key, val))
}

func appendOptionalBoolEnv(env []string, key string, val *bool) []string {
	if strings.TrimSpace(key) == "" || val == nil {
		return env
	}
	if *val {
		return append(env, key+"=1")
	}
	return append(env, key+"=0")
}

func appendHostEnvIfSet(env []string, key string) []string {
	val := strings.TrimSpace(os.Getenv(strings.TrimSpace(key)))
	if val == "" {
		return env
	}
	return append(env, key+"="+val)
}

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func actorContainerAlias(dyad, member string) string {
	name := DyadContainerName(dyad, member)
	if name == "" {
		return ""
	}
	return name
}

func parseForwardPorts(raw string) (nat.PortSet, map[nat.Port][]nat.PortBinding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "1455-1465"
	}
	ports := []int{}
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				return nil, nil, fmt.Errorf("invalid port range %q", part)
			}
			start, end, err := parsePortRange(rangeParts[0], rangeParts[1])
			if err != nil {
				return nil, nil, err
			}
			for p := start; p <= end; p++ {
				ports = append(ports, p)
			}
			continue
		}
		p, err := parsePort(part)
		if err != nil {
			return nil, nil, err
		}
		ports = append(ports, p)
	}
	if len(ports) == 0 {
		return nil, nil, errors.New("no forward ports")
	}
	exposed := nat.PortSet{}
	bindings := map[nat.Port][]nat.PortBinding{}
	for _, port := range ports {
		key := nat.Port(fmt.Sprintf("%d/tcp", port))
		exposed[key] = struct{}{}
		bindings[key] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}
	}
	return exposed, bindings, nil
}

func parsePortRange(startRaw, endRaw string) (int, int, error) {
	start, err := parsePort(startRaw)
	if err != nil {
		return 0, 0, err
	}
	end, err := parsePort(endRaw)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("invalid port range %d-%d", start, end)
	}
	return start, end, nil
}

func parsePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("port required")
	}
	var port int
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid port %q", raw)
		}
		port = port*10 + int(ch-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %d", port)
	}
	return port, nil
}
