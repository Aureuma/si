package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	shared "silexa/agents/shared/docker"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	logger := log.New(os.Stdout, "dashboard ", log.LstdFlags|log.LUTC)
	managerURL := env("MANAGER_URL", "http://manager:9090")
	addr := env("ADDR", ":8087")

	dockerClient, err := shared.NewClient()
	if err != nil {
		logger.Fatalf("docker client init: %v", err)
	}
	defer dockerClient.Close()

	r := chi.NewRouter()
	r.Use(corsMiddleware)

	r.Get("/api/dyad-tasks", func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(managerURL + "/dyad-tasks")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	r.Post("/api/spawn", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Name        string `json:"name"`
			Role        string `json:"role"`
			Department  string `json:"department"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if payload.Role == "" {
			payload.Role = payload.Name
		}
		if payload.Department == "" {
			payload.Department = payload.Role
		}
		opts, err := buildDyadOptions(payload.Name, payload.Role, payload.Department)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, _, err := dockerClient.EnsureDyad(r.Context(), opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": "spawned"})
	})

	// Static UI
	r.Handle("/*", http.FileServer(http.FS(staticFS)))

	logger.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func buildDyadOptions(dyad, role, dept string) (shared.DyadOptions, error) {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return shared.DyadOptions{}, errors.New("dyad name required")
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
		return shared.DyadOptions{}, errors.New("configs path not found: " + configsHost)
	}

	return shared.DyadOptions{
		Dyad:              dyad,
		Role:              role,
		Department:        dept,
		ActorImage:        env("ACTOR_IMAGE", "silexa/actor:local"),
		CriticImage:       env("CRITIC_IMAGE", "silexa/critic:local"),
		ManagerURL:        env("MANAGER_SERVICE_URL", env("MANAGER_URL", "http://silexa-manager:9090")),
		TelegramURL:       env("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify"),
		TelegramChatID:    strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		CodexModel:        env("CODEX_MODEL", "gpt-5.2-codex"),
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
		Network:           env("SILEXA_DOCKER_NETWORK", shared.DefaultNetwork),
		ForwardPorts:      strings.TrimSpace(os.Getenv("CODEX_FORWARD_PORTS")),
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
