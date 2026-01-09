package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"
)

func main() {
	loadSecret("GH_TOKEN", "GH_TOKEN_FILE", "/run/secrets/gh_token")
	loadSecret("STRIPE_API_KEY", "STRIPE_API_KEY_FILE", "/run/secrets/stripe_api_key")

	if strings.TrimSpace(os.Getenv("DOCKER_HOST")) != "" {
		waitForDocker()
	}

	catalogDir := "/catalog"
	_ = os.MkdirAll(catalogDir, 0o755)
	catalogPath := filepath.Join(catalogDir, "catalog.yaml")

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"gateway", "run", "--transport", "streaming", "--port", "8088", "--catalog", catalogPath}
	}
	if len(args) > 0 && args[0] == "gateway" {
		if _, err := os.Stat(catalogPath); err != nil {
			_ = exec.Command("/usr/local/bin/docker-mcp", "catalog", "bootstrap", catalogPath).Run()
		}
	}

	if err := syscall.Exec("/usr/local/bin/docker-mcp", append([]string{"docker-mcp"}, args...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func loadSecret(envKey, fileEnvKey, defaultPath string) {
	if val := strings.TrimSpace(os.Getenv(envKey)); val != "" && !isUnset(val) {
		return
	}
	path := strings.TrimSpace(os.Getenv(fileEnvKey))
	if path == "" {
		path = defaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	val := strings.TrimSpace(string(data))
	if val == "" || isUnset(val) {
		return
	}
	_ = os.Setenv(envKey, val)
}

func isUnset(val string) bool {
	return strings.EqualFold(strings.TrimSpace(val), "unset")
}

func waitForDocker() {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return
	}
	defer cli.Close()

	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := cli.Ping(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(1 * time.Second)
	}
}
