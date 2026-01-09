package beam

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"
)

func (a *Activities) ApplyDyadResources(ctx context.Context, req DyadBootstrapRequest) (DyadBootstrapResult, error) {
	if a.docker == nil {
		return DyadBootstrapResult{}, fmt.Errorf("docker client unavailable")
	}
	opts, err := buildDyadOptions(req.Dyad, req.Role, req.Department)
	if err != nil {
		return DyadBootstrapResult{}, err
	}
	if _, _, err := a.docker.client.EnsureDyad(ctx, opts); err != nil {
		return DyadBootstrapResult{}, err
	}
	return DyadBootstrapResult{
		ActorContainer:  shared.DyadContainerName(req.Dyad, "actor"),
		CriticContainer: shared.DyadContainerName(req.Dyad, "critic"),
	}, nil
}

func (a *Activities) WaitDyadReady(ctx context.Context, req DyadBootstrapRequest) error {
	if a.docker == nil {
		return fmt.Errorf("docker client unavailable")
	}
	if strings.TrimSpace(req.Dyad) == "" {
		return fmt.Errorf("dyad required")
	}
	timeout := 6 * time.Minute
	deadline := time.Now().Add(timeout)
	for {
		exists, ready, err := a.docker.client.DyadStatus(ctx, req.Dyad)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("dyad %s not found", req.Dyad)
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dyad %s not ready after %s", req.Dyad, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func buildDyadOptions(dyad, role, dept string) (shared.DyadOptions, error) {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return shared.DyadOptions{}, fmt.Errorf("dyad name required")
	}
	if role == "" {
		role = "generic"
	}
	if dept == "" {
		dept = role
	}

	actorEffort, criticEffort := codexEffortForRole(role)
	if v := strings.TrimSpace(os.Getenv("CODEX_ACTOR_EFFORT")); v != "" {
		actorEffort = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_CRITIC_EFFORT")); v != "" {
		criticEffort = v
	}

	workspaceHost, err := workspaceHostPath()
	if err != nil {
		return shared.DyadOptions{}, err
	}
	configsHost := strings.TrimSpace(os.Getenv("SILEXA_CONFIGS_HOST"))
	if configsHost == "" {
		configsHost = filepath.Join(workspaceHost, "configs")
	}
	if _, err := os.Stat(configsHost); err != nil {
		return shared.DyadOptions{}, fmt.Errorf("configs path not found: %s", configsHost)
	}

	approverToken := strings.TrimSpace(os.Getenv("CREDENTIALS_APPROVER_TOKEN"))
	if approverToken == "" {
		if tokenFile := strings.TrimSpace(os.Getenv("CREDENTIALS_APPROVER_TOKEN_FILE")); tokenFile != "" {
			if data, err := os.ReadFile(tokenFile); err == nil {
				approverToken = strings.TrimSpace(string(data))
			}
		} else {
			tokenFile = filepath.Join(workspaceHost, "secrets", "credentials_mcp_token")
			if data, err := os.ReadFile(tokenFile); err == nil {
				approverToken = strings.TrimSpace(string(data))
			}
		}
	}

	return shared.DyadOptions{
		Dyad:              dyad,
		Role:              role,
		Department:        dept,
		ActorImage:        envOr("ACTOR_IMAGE", "silexa/actor:local"),
		CriticImage:       envOr("CRITIC_IMAGE", "silexa/critic:local"),
		ManagerURL:        envOr("MANAGER_SERVICE_URL", envOr("MANAGER_URL", "http://silexa-manager:9090")),
		TelegramURL:       envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"),
		TelegramChatID:    strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		CodexModel:        envOr("CODEX_MODEL", "gpt-5.2-codex"),
		CodexEffortActor:  actorEffort,
		CodexEffortCritic: criticEffort,
		CodexModelLow:     strings.TrimSpace(os.Getenv("CODEX_MODEL_LOW")),
		CodexModelMedium:  strings.TrimSpace(os.Getenv("CODEX_MODEL_MEDIUM")),
		CodexModelHigh:    strings.TrimSpace(os.Getenv("CODEX_MODEL_HIGH")),
		CodexEffortLow:    strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_LOW")),
		CodexEffortMedium: strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_MEDIUM")),
		CodexEffortHigh:   strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_HIGH")),
		WorkspaceHost:     workspaceHost,
		ConfigsHost:       configsHost,
		Network:           envOr("SILEXA_DOCKER_NETWORK", shared.DefaultNetwork),
		ForwardPorts:      strings.TrimSpace(os.Getenv("CODEX_FORWARD_PORTS")),
		ApproverToken:     approverToken,
	}, nil
}

func codexEffortForRole(role string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(role)) {
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

func workspaceHostPath() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("SILEXA_WORKSPACE_HOST")); raw != "" {
		return filepath.Abs(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("SILEXA_WORKSPACE")); raw != "" {
		return filepath.Abs(raw)
	}
	if runningInDocker() {
		return "", errors.New("SILEXA_WORKSPACE_HOST required when running inside a container")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(cwd)
}

func runningInDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	text := string(data)
	return strings.Contains(text, "docker") || strings.Contains(text, "containerd")
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}
