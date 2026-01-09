package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	shared "silexa/agents/shared/docker"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

const stackLabelKey = "silexa.stack"
const stackLabelVal = "core"

func cmdStack(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: silexa stack <up|down|status>")
		return
	}
	switch args[0] {
	case "up":
		cmdStackUp(args[1:])
	case "down":
		cmdStackDown(args[1:])
	case "status":
		cmdStackStatus(args[1:])
	default:
		fmt.Println("unknown stack command:", args[0])
	}
}

func cmdStackUp(args []string) {
	root := mustRepoRoot()
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	_, _ = client.EnsureNetwork(ctx, shared.DefaultNetwork, map[string]string{stackLabelKey: stackLabelVal})

	_, _ = client.EnsureVolume(ctx, "silexa-temporal-db", map[string]string{stackLabelKey: stackLabelVal})
	_, _ = client.EnsureVolume(ctx, "silexa-resource-broker-data", map[string]string{stackLabelKey: stackLabelVal})
	_, _ = client.EnsureVolume(ctx, "silexa-infra-broker-data", map[string]string{stackLabelKey: stackLabelVal})
	_, _ = client.EnsureVolume(ctx, "silexa-mcp-catalog", map[string]string{stackLabelKey: stackLabelVal})
	_, _ = client.EnsureVolume(ctx, "silexa-mcp-dind", map[string]string{stackLabelKey: stackLabelVal})

	secretsDir := filepath.Join(root, "secrets")
	configsDir := filepath.Join(root, "configs")
	approverToken := envOr("CREDENTIALS_APPROVER_TOKEN", "")
	if approverToken == "" {
		if token, ok, err := readFileTrim(filepath.Join(secretsDir, "credentials_mcp_token")); err == nil && ok {
			approverToken = token
		}
	}

	services := stackServices(stackContext{
		Root:          root,
		ConfigsDir:    configsDir,
		SecretsDir:    secretsDir,
		ApproverToken: approverToken,
	})
	for _, svc := range services {
		if _, err := ensureContainer(ctx, client, svc); err != nil {
			fatal(err)
		}
	}
	fmt.Println("stack up: ok")
	_ = args
}

func cmdStackDown(args []string) {
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	for _, name := range stackContainerNames() {
		if id, _, _ := client.ContainerByName(ctx, name); id != "" {
			_ = client.RemoveContainer(ctx, id, true)
		}
	}
	fmt.Println("stack down: containers removed")
	_ = args
}

func cmdStackStatus(args []string) {
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	containers, err := client.ListContainers(ctx, true, map[string]string{stackLabelKey: stackLabelVal})
	if err != nil {
		fatal(err)
	}
	if len(containers) == 0 {
		fmt.Println("no stack containers found")
		return
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Names[0] < containers[j].Names[0]
	})
	fmt.Printf("%-28s %-10s %-20s\n", "CONTAINER", "STATE", "IMAGE")
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		fmt.Printf("%-28s %-10s %-20s\n", name, c.State, c.Image)
	}
	_ = args
}

type stackContext struct {
	Root          string
	ConfigsDir    string
	SecretsDir    string
	ApproverToken string
}

type stackService struct {
	Name       string
	Image      string
	Env        []string
	Ports      map[int]int
	Mounts     []mount.Mount
	Cmd        []string
	Entrypoint []string
	Aliases    []string
	Labels     map[string]string
	Privileged bool
	User       string
}

func stackServices(ctx stackContext) []stackService {
	stackLabels := func(component string) map[string]string {
		return map[string]string{
			stackLabelKey:        stackLabelVal,
			"silexa.component":   component,
		}
	}
	configMount := func(target string) mount.Mount {
		return mount.Mount{Type: mount.TypeBind, Source: ctx.ConfigsDir, Target: target, ReadOnly: true}
	}
	secretsMount := func(target string) mount.Mount {
		return mount.Mount{Type: mount.TypeBind, Source: ctx.SecretsDir, Target: target, ReadOnly: true}
	}

	temporalEnv := []string{
		"DB=" + envOr("TEMPORAL_DB", "postgresql"),
		"POSTGRES_SEEDS=" + envOr("TEMPORAL_POSTGRES_HOST", "silexa-postgres"),
		"POSTGRES_USER=" + envOr("TEMPORAL_POSTGRES_USER", "temporal"),
		"POSTGRES_PWD=" + envOr("TEMPORAL_POSTGRES_PASSWORD", "temporal"),
		"POSTGRES_DB=" + envOr("TEMPORAL_POSTGRES_DB", "temporal"),
		"VISIBILITY_DBNAME=" + envOr("TEMPORAL_VISIBILITY_DB", "temporal_visibility"),
	}

	managerEnv := []string{
		"TEMPORAL_ADDRESS=" + envOr("TEMPORAL_ADDRESS", "temporal-frontend:7233"),
		"TEMPORAL_NAMESPACE=" + envOr("TEMPORAL_NAMESPACE", "default"),
		"TEMPORAL_TASK_QUEUE=" + envOr("TEMPORAL_TASK_QUEUE", "silexa-state"),
		"TELEGRAM_NOTIFY_URL=" + envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"),
		"TELEGRAM_CHAT_ID=" + envOr("TELEGRAM_CHAT_ID", ""),
		"DYAD_REQUIRE_ASSIGNMENT=" + envOr("DYAD_REQUIRE_ASSIGNMENT", "true"),
		"DYAD_ALLOW_UNASSIGNED=" + envOr("DYAD_ALLOW_UNASSIGNED", "true"),
		"DYAD_REQUIRE_REGISTERED=" + envOr("DYAD_REQUIRE_REGISTERED", "true"),
		"DYAD_ENFORCE_AVAILABLE=" + envOr("DYAD_ENFORCE_AVAILABLE", "true"),
		"DYAD_MAX_OPEN_PER_DYAD=" + envOr("DYAD_MAX_OPEN_PER_DYAD", "10"),
		"DYAD_ALLOW_POOL=" + envOr("DYAD_ALLOW_POOL", "true"),
		"DYAD_TASK_DIGEST_INTERVAL=" + envOr("DYAD_TASK_DIGEST_INTERVAL", "10m"),
		"BEAM_RECONCILE_INTERVAL=" + envOr("BEAM_RECONCILE_INTERVAL", "1m"),
	}

	workerEnv := append([]string{}, managerEnv...)

	services := []stackService{
		{
			Name:   "silexa-postgres",
			Image:  envOr("TEMPORAL_POSTGRES_IMAGE", "postgres:15"),
			Env: []string{
				"POSTGRES_USER=" + envOr("TEMPORAL_POSTGRES_USER", "temporal"),
				"POSTGRES_PASSWORD=" + envOr("TEMPORAL_POSTGRES_PASSWORD", "temporal"),
				"POSTGRES_DB=" + envOr("TEMPORAL_POSTGRES_DB", "temporal"),
			},
			Mounts: []mount.Mount{
				{Name: "silexa-temporal-db", Type: mount.TypeVolume, Target: "/var/lib/postgresql/data"},
			},
			Labels:  stackLabels("postgres"),
			Aliases: []string{"silexa-postgres"},
		},
		{
			Name:   "silexa-temporal",
			Image:  envOr("TEMPORAL_IMAGE", "temporalio/auto-setup:1.24.4"),
			Env:    temporalEnv,
			Ports:  map[int]int{7233: 7233},
			Labels: stackLabels("temporal"),
			Aliases: []string{"temporal-frontend"},
		},
		{
			Name:   "silexa-manager",
			Image:  envOr("SILEXA_MANAGER_IMAGE", "silexa/manager:local"),
			Env:    managerEnv,
			Ports:  map[int]int{9090: 9090},
			Labels: stackLabels("manager"),
			Aliases: []string{"silexa-manager"},
		},
		{
			Name:   "silexa-manager-worker",
			Image:  envOr("SILEXA_MANAGER_IMAGE", "silexa/manager:local"),
			Env:    workerEnv,
			Cmd:    []string{"manager-worker"},
			Labels: stackLabels("manager-worker"),
			Aliases: []string{"silexa-manager-worker"},
		},
		{
			Name:   "silexa-router",
			Image:  envOr("SILEXA_ROUTER_IMAGE", "silexa/router:local"),
			Env: []string{
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"ROUTER_RULES_FILE=/configs/router_rules.json",
				"ROUTER_POLL_INTERVAL=" + envOr("ROUTER_POLL_INTERVAL", "10s"),
				"DYAD_REQUIRE_REGISTERED=" + envOr("DYAD_REQUIRE_REGISTERED", "true"),
				"DYAD_ENFORCE_AVAILABLE=" + envOr("DYAD_ENFORCE_AVAILABLE", "true"),
				"DYAD_REQUIRE_ONLINE=" + envOr("DYAD_REQUIRE_ONLINE", "true"),
				"DYAD_MAX_OPEN_PER_DYAD=" + envOr("DYAD_MAX_OPEN_PER_DYAD", "10"),
			},
			Mounts: []mount.Mount{configMount("/configs")},
			Labels: stackLabels("router"),
			Aliases: []string{"silexa-router"},
		},
		{
			Name:   "silexa-codex-monitor",
			Image:  envOr("SILEXA_CODEX_MONITOR_IMAGE", "silexa/codex-monitor:local"),
			Env: []string{
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"CODEX_ACCOUNTS_FILE=/configs/codex_accounts.json",
				"CODEX_STATUS_POLL_INTERVAL=" + envOr("CODEX_STATUS_POLL_INTERVAL", "2m"),
				"CODEX_COOLDOWN_THRESHOLD_PCT=" + envOr("CODEX_COOLDOWN_THRESHOLD_PCT", "10"),
				"CODEX_PLAN_LIMIT_MINUTES=" + envOr("CODEX_PLAN_LIMIT_MINUTES", "300"),
				"CODEX_SPAWN_DYADS=" + envOr("CODEX_SPAWN_DYADS", "1"),
				"CODEX_MONITOR_ADDR=" + envOr("CODEX_MONITOR_ADDR", ":8086"),
			},
			Ports:  map[int]int{8086: 8086},
			Mounts: []mount.Mount{configMount("/configs")},
			Labels: stackLabels("codex-monitor"),
			Aliases: []string{"silexa-codex-monitor"},
		},
		{
			Name:   "silexa-resource-broker",
			Image:  envOr("SILEXA_RESOURCE_BROKER_IMAGE", "silexa/resource-broker:local"),
			Env: []string{
				"TELEGRAM_NOTIFY_URL=" + envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"),
				"TELEGRAM_CHAT_ID=" + envOr("TELEGRAM_CHAT_ID", ""),
				"DATA_DIR=/data",
			},
			Ports:  map[int]int{9091: 9091},
			Mounts: []mount.Mount{{Name: "silexa-resource-broker-data", Type: mount.TypeVolume, Target: "/data"}},
			Labels: stackLabels("resource-broker"),
			Aliases: []string{"silexa-resource-broker"},
		},
		{
			Name:   "silexa-infra-broker",
			Image:  envOr("SILEXA_INFRA_BROKER_IMAGE", "silexa/infra-broker:local"),
			Env: []string{
				"TELEGRAM_NOTIFY_URL=" + envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"),
				"TELEGRAM_CHAT_ID=" + envOr("TELEGRAM_CHAT_ID", ""),
				"DATA_DIR=/data",
			},
			Ports:  map[int]int{9092: 9092},
			Mounts: []mount.Mount{{Name: "silexa-infra-broker-data", Type: mount.TypeVolume, Target: "/data"}},
			Labels: stackLabels("infra-broker"),
			Aliases: []string{"silexa-infra-broker"},
		},
		{
			Name:   "silexa-program-manager",
			Image:  envOr("SILEXA_PROGRAM_MANAGER_IMAGE", "silexa/program-manager:local"),
			Env: []string{
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"PROGRAM_CONFIG_FILE=/configs/programs/web_hosting.json",
				"PROGRAM_RECONCILE_INTERVAL=" + envOr("PROGRAM_RECONCILE_INTERVAL", "30s"),
			},
			Mounts: []mount.Mount{configMount("/configs")},
			Labels: stackLabels("program-manager"),
			Aliases: []string{"silexa-program-manager"},
		},
		{
			Name:   "silexa-credentials-mcp",
			Image:  envOr("SILEXA_CREDENTIALS_MCP_IMAGE", "silexa/credentials-mcp:local"),
			Env: []string{
				"ADDR=:8091",
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"CREDENTIALS_REGISTRY=/configs/credentials-registry.json",
				"SECRETS_DIR=/credentials/secrets",
				"SOPS_AGE_KEY_FILE=/run/secrets/age.key",
				"REQUEST_TIMEOUT=" + envOr("CREDENTIALS_REQUEST_TIMEOUT", "15s"),
				"CREDENTIALS_APPROVER_TOKEN=" + ctx.ApproverToken,
			},
			Ports:  map[int]int{8091: 8091},
			Mounts: []mount.Mount{configMount("/configs"), secretsMount("/credentials/secrets"), secretsMount("/run/secrets")},
			Labels: stackLabels("credentials-mcp"),
			Aliases: []string{"silexa-credentials-mcp"},
		},
		{
			Name:   "silexa-mcp-dind",
			Image:  envOr("SILEXA_MCP_DIND_IMAGE", "docker:26-dind"),
			Env:    []string{"DOCKER_TLS_CERTDIR="},
			Cmd:    []string{"--host=tcp://0.0.0.0:2375", "--host=unix:///var/run/docker.sock"},
			Mounts: []mount.Mount{{Name: "silexa-mcp-dind", Type: mount.TypeVolume, Target: "/var/lib/docker"}},
			Privileged: true,
			Labels: stackLabels("mcp-dind"),
			Aliases: []string{"silexa-mcp-dind"},
		},
		{
			Name:   "silexa-mcp-gateway",
			Image:  envOr("SILEXA_MCP_GATEWAY_IMAGE", "silexa/mcp-gateway:local"),
			Env: []string{
				"DOCKER_HOST=tcp://silexa-mcp-dind:2375",
				"DOCKER_MCP_IN_CONTAINER=1",
				"DOCKER_MCP_IN_DIND=1",
				"GH_TOKEN_FILE=/run/secrets/gh_token",
				"STRIPE_API_KEY_FILE=/run/secrets/stripe_api_key",
			},
			Ports:  map[int]int{8088: 8088},
			Mounts: []mount.Mount{{Name: "silexa-mcp-catalog", Type: mount.TypeVolume, Target: "/catalog"}, secretsMount("/run/secrets")},
			Labels: stackLabels("mcp-gateway"),
			Aliases: []string{"silexa-mcp-gateway"},
		},
		{
			Name:   "silexa-telegram-bot",
			Image:  envOr("SILEXA_TELEGRAM_BOT_IMAGE", "silexa/telegram-bot:local"),
			Env: []string{
				"TELEGRAM_CHAT_ID=" + envOr("TELEGRAM_CHAT_ID", ""),
				"TELEGRAM_BOT_TOKEN_FILE=/run/secrets/telegram_bot_token",
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"CODEX_MONITOR_URL=" + envOr("CODEX_MONITOR_URL", "http://silexa-codex-monitor:8086/status"),
			},
			Ports:  map[int]int{8081: 8081},
			Mounts: []mount.Mount{secretsMount("/run/secrets")},
			Labels: stackLabels("telegram-bot"),
			Aliases: []string{"silexa-telegram-bot"},
		},
		{
			Name:   "silexa-dashboard",
			Image:  envOr("SILEXA_DASHBOARD_IMAGE", "silexa/dashboard:local"),
			Env: []string{
				"MANAGER_URL=" + envOr("MANAGER_URL", "http://silexa-manager:9090"),
				"ADDR=:8087",
			},
			Ports:  map[int]int{8087: 8087},
			Labels: stackLabels("dashboard"),
			Aliases: []string{"silexa-dashboard"},
		},
	}
	return services
}

func stackContainerNames() []string {
	return []string{
		"silexa-dashboard",
		"silexa-telegram-bot",
		"silexa-mcp-gateway",
		"silexa-mcp-dind",
		"silexa-credentials-mcp",
		"silexa-program-manager",
		"silexa-infra-broker",
		"silexa-resource-broker",
		"silexa-codex-monitor",
		"silexa-router",
		"silexa-manager-worker",
		"silexa-manager",
		"silexa-temporal",
		"silexa-postgres",
	}
}

func ensureContainer(ctx context.Context, client *shared.Client, svc stackService) (string, error) {
	if svc.Name == "" {
		return "", fmt.Errorf("container name required")
	}
	id, info, err := client.ContainerByName(ctx, svc.Name)
	if err != nil {
		return "", err
	}
	if id != "" {
		if info != nil && info.State != nil && !info.State.Running {
			if err := client.StartContainer(ctx, id); err != nil {
				return "", err
			}
		}
		return id, nil
	}

	exposed := nat.PortSet{}
	bindings := map[nat.Port][]nat.PortBinding{}
	for containerPort, hostPort := range svc.Ports {
		port := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
		exposed[port] = struct{}{}
		hostPortStr := ""
		if hostPort > 0 {
			hostPortStr = fmt.Sprintf("%d", hostPort)
		}
		bindings[port] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: hostPortStr}}
	}

	cfg := &container.Config{
		Image:        svc.Image,
		Env:          filterEnv(svc.Env),
		Labels:       svc.Labels,
		ExposedPorts: exposed,
		Cmd:          svc.Cmd,
		Entrypoint:   svc.Entrypoint,
		User:         svc.User,
	}
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        svc.Mounts,
		PortBindings:  bindings,
		Privileged:    svc.Privileged,
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			shared.DefaultNetwork: {
				Aliases: append([]string{svc.Name}, svc.Aliases...),
			},
		},
	}

	createdID, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, svc.Name)
	if err != nil {
		return "", err
	}
	if err := client.StartContainer(ctx, createdID); err != nil {
		return "", err
	}
	return createdID, nil
}

func filterEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasSuffix(entry, "=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}
