package docker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

const (
	DefaultNetwork = "silexa"
	LabelApp       = "app"
	LabelDyad      = "silexa.dyad"
	LabelMember    = "silexa.member"
	LabelRole      = "silexa.role"
	LabelDept      = "silexa.department"
)

const DyadAppLabel = "silexa-dyad"

type DyadOptions struct {
	Dyad              string
	Role              string
	Department        string
	ActorImage        string
	CriticImage       string
	ManagerURL        string
	TelegramURL       string
	TelegramChatID    string
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
	ApproverToken     string
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
	return "silexa-" + member + "-" + dyad
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
	if strings.TrimSpace(opts.Department) == "" {
		opts.Department = opts.Role
	}
	if strings.TrimSpace(opts.ActorImage) == "" || strings.TrimSpace(opts.CriticImage) == "" {
		return ContainerSpec{}, ContainerSpec{}, errors.New("actor and critic images required")
	}
	if strings.TrimSpace(opts.Network) == "" {
		opts.Network = DefaultNetwork
	}
	if strings.TrimSpace(opts.CodexVolume) == "" {
		opts.CodexVolume = "silexa-codex-" + opts.Dyad
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
		LabelDept: opts.Department,
	}

	actorLabels := cloneLabels(labels)
	actorLabels[LabelMember] = "actor"
	criticLabels := cloneLabels(labels)
	criticLabels[LabelMember] = "critic"

	actorEnv := buildDyadEnv(opts, "actor", opts.CodexEffortActor)
	criticEnv := buildDyadEnv(opts, "critic", opts.CodexEffortCritic)

	if opts.Dyad == "silexa-credentials" && strings.TrimSpace(opts.ApproverToken) != "" {
		actorEnv = append(actorEnv, "CREDENTIALS_APPROVER_TOKEN="+strings.TrimSpace(opts.ApproverToken))
		criticEnv = append(criticEnv, "CREDENTIALS_APPROVER_TOKEN="+strings.TrimSpace(opts.ApproverToken))
	}

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
		WorkingDir:   "/workspace/silexa/apps",
		Env:          actorEnv,
		Labels:       actorLabels,
		Entrypoint:   []string{"tini", "-s", "--", "/usr/local/bin/silexa-codex-init"},
		Cmd:          []string{"--exec", "tail", "-f", "/dev/null"},
		ExposedPorts: actorExposed,
	}
	actorMounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: opts.CodexVolume, Target: "/root/.codex"},
		{Type: mount.TypeBind, Source: opts.WorkspaceHost, Target: "/workspace"},
	}
	if target, ok := InferWorkspaceTarget(opts.WorkspaceHost); ok && target != "/workspace" {
		actorMounts = append(actorMounts, mount.Mount{Type: mount.TypeBind, Source: opts.WorkspaceHost, Target: target})
	}
	actorHostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        actorMounts,
		PortBindings:  actorBindings,
	}

	criticEnv = append(criticEnv,
		"MANAGER_URL="+strings.TrimSpace(opts.ManagerURL),
		"TELEGRAM_NOTIFY_URL="+strings.TrimSpace(opts.TelegramURL),
		"TELEGRAM_CHAT_ID="+strings.TrimSpace(opts.TelegramChatID),
		"ACTOR_CONTAINER="+DyadContainerName(opts.Dyad, "actor"),
		"HOME=/root",
		"CODEX_HOME=/root/.codex",
	)

	criticConfig := &container.Config{
		Image:  opts.CriticImage,
		Env:    criticEnv,
		Labels: criticLabels,
		User:   "root",
	}
	criticMounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: opts.CodexVolume, Target: "/root/.codex"},
		{Type: mount.TypeBind, Source: opts.WorkspaceHost, Target: "/workspace"},
		{Type: mount.TypeBind, Source: opts.ConfigsHost, Target: "/configs", ReadOnly: true},
		{Type: mount.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"},
	}
	if target, ok := InferWorkspaceTarget(opts.WorkspaceHost); ok && target != "/workspace" {
		criticMounts = append(criticMounts, mount.Mount{Type: mount.TypeBind, Source: opts.WorkspaceHost, Target: target})
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
	env := []string{
		"ROLE=" + opts.Role,
		"DEPARTMENT=" + opts.Department,
		"DYAD_NAME=" + opts.Dyad,
		"DYAD_MEMBER=" + member,
		"CODEX_INIT_FORCE=1",
	}
	model := strings.TrimSpace(opts.CodexModel)
	if model != "" {
		env = append(env, "CODEX_MODEL="+model)
	}
	if effort != "" {
		env = append(env, "CODEX_REASONING_EFFORT="+effort)
	}
	return env
}

func appendOptionalEnv(env []string, key, val string) []string {
	if strings.TrimSpace(val) == "" {
		return env
	}
	return append(env, key+"="+strings.TrimSpace(val))
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
