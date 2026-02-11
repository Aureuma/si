package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func AutoDockerHost() (string, bool) {
	if os.Getenv("DOCKER_HOST") != "" {
		return "", false
	}
	if strings.TrimSpace(os.Getenv("DOCKER_CONTEXT")) != "" {
		return "", false
	}
	if defaultDockerSocketAvailable() {
		return "", false
	}
	host, ok := detectColimaHost()
	if ok {
		return host, true
	}
	return "", false
}

func defaultDockerSocketAvailable() bool {
	return socketExists("/var/run/docker.sock")
}

func detectColimaHost() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	colimaHome := strings.TrimSpace(os.Getenv("COLIMA_HOME"))
	if colimaHome == "" {
		colimaHome = filepath.Join(home, ".colima")
	}
	profiles := colimaProfileCandidates(home)
	if host, ok := detectColimaHostForProfiles(colimaHome, profiles); ok {
		return host, true
	}
	entries, readErr := os.ReadDir(colimaHome)
	if readErr != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, strings.TrimSpace(entry.Name()))
		}
	}
	sort.Strings(names)
	if host, ok := detectColimaHostForProfiles(colimaHome, names); ok {
		return host, true
	}
	return "", false
}

func detectColimaHostForProfiles(colimaHome string, profiles []string) (string, bool) {
	for _, profile := range profiles {
		p := strings.TrimSpace(profile)
		if p == "" {
			continue
		}
		candidate := filepath.Join(colimaHome, p, "docker.sock")
		if socketExists(candidate) {
			return "unix://" + candidate, true
		}
	}
	return "", false
}

func colimaProfileCandidates(home string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	push := func(value string) {
		name := strings.TrimSpace(value)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	// Explicit profile hints first.
	push(os.Getenv("COLIMA_PROFILE"))
	push(os.Getenv("COLIMA_INSTANCE"))
	// Honor docker current context hints (colima / colima-<profile>).
	if current := dockerCurrentContext(home); current != "" {
		if profile, ok := colimaProfileFromDockerContext(current); ok {
			push(profile)
		}
	}
	// Keep default as a fallback.
	push("default")
	return out
}

func dockerCurrentContext(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	path := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.CurrentContext)
}

func colimaProfileFromDockerContext(contextName string) (string, bool) {
	name := strings.TrimSpace(contextName)
	switch {
	case name == "colima":
		return "default", true
	case strings.HasPrefix(name, "colima-"):
		profile := strings.TrimSpace(strings.TrimPrefix(name, "colima-"))
		if profile != "" {
			return profile, true
		}
	}
	return "", false
}

func socketExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}
