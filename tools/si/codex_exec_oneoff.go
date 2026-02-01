package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	shared "si/agents/shared/docker"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
)

type codexExecOneOffOptions struct {
	Prompt        string
	Image         string
	WorkspaceHost string
	Workdir       string
	Network       string
	CodexVolume   string
	GHVolume      string
	Env           []string
	Model         string
	Effort        string
	DisableMCP    bool
	OutputOnly    bool
	KeepContainer bool
	DockerSocket  bool
	Profile       *codexProfile
}

func runCodexExecOneOff(opts codexExecOneOffOptions) error {
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return fmt.Errorf("prompt required")
	}
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		return fmt.Errorf("image required")
	}
	if strings.TrimSpace(opts.Workdir) == "" {
		opts.Workdir = "/workspace"
	}
	if strings.TrimSpace(opts.WorkspaceHost) == "" {
		if root, err := repoRoot(); err == nil {
			opts.WorkspaceHost = root
		}
	}

	env := []string{
		"HOME=/home/si",
		"CODEX_HOME=/home/si/.codex",
	}
	env = append(env, hostUserEnv()...)
	if opts.Profile != nil {
		if strings.TrimSpace(opts.Profile.ID) != "" {
			env = append(env, "SI_CODEX_PROFILE_ID="+opts.Profile.ID)
		}
		if strings.TrimSpace(opts.Profile.Name) != "" {
			env = append(env, "SI_CODEX_PROFILE_NAME="+opts.Profile.Name)
		}
	}
	if strings.TrimSpace(opts.Model) != "" {
		env = append(env, "CODEX_MODEL="+strings.TrimSpace(opts.Model))
	}
	if strings.TrimSpace(opts.Effort) != "" {
		env = append(env, "CODEX_REASONING_EFFORT="+normalizeReasoningEffort(opts.Effort))
	}
	env = append(env, opts.Env...)

	configHostDir := ""
	configTargetDir := ""
	if opts.DisableMCP {
		var err error
		configHostDir, err = writeNoMcpConfig(opts.Model, opts.Effort)
		if err != nil {
			return err
		}
		configTargetDir = "/tmp/si-codex-config"
		env = append(env, "CODEX_CONFIG_DIR="+configTargetDir)
		env = append(env, "CODEX_MCP_DISABLED=1")
	}
	if configHostDir != "" {
		defer os.RemoveAll(configHostDir)
	}

	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()
	ctx := context.Background()

	if strings.TrimSpace(opts.Network) != "" {
		_, _ = client.EnsureNetwork(ctx, opts.Network, map[string]string{codexLabelKey: codexLabelValue})
	}

	containerName := codexExecContainerName()
	labels := map[string]string{
		codexLabelKey: codexLabelValue,
		"si.mode":     "exec",
		"si.exec":     "one-off",
	}

	mounts := []mount.Mount{}
	if strings.TrimSpace(opts.CodexVolume) != "" {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: opts.CodexVolume, Target: "/home/si/.codex"})
	}
	if strings.TrimSpace(opts.GHVolume) != "" {
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: opts.GHVolume, Target: "/home/si/.config/gh"})
	}
	if strings.TrimSpace(opts.WorkspaceHost) != "" {
		mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: opts.WorkspaceHost, Target: "/workspace"})
	}
	if configHostDir != "" && configTargetDir != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   configHostDir,
			Target:   configTargetDir,
			ReadOnly: true,
		})
	}
	if opts.DockerSocket {
		if socketMount, ok := shared.DockerSocketMount(); ok {
			mounts = append(mounts, socketMount)
		}
	}

	cfg := &container.Config{
		Image:      image,
		Env:        filterEnv(env),
		Labels:     labels,
		WorkingDir: opts.Workdir,
		Cmd:        []string{"bash", "-lc", "sleep infinity"},
	}
	hostCfg := &container.HostConfig{
		AutoRemove: !opts.KeepContainer,
		Mounts:     mounts,
	}
	netCfg := &network.NetworkingConfig{}
	if strings.TrimSpace(opts.Network) != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				opts.Network: {Aliases: []string{containerName}},
			},
		}
	}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, netCfg, containerName)
	if err != nil {
		return err
	}
	if !opts.KeepContainer {
		defer func() {
			_ = client.RemoveContainer(ctx, id, true)
		}()
	}
	if err := client.StartContainer(ctx, id); err != nil {
		return err
	}
	if opts.Profile != nil {
		seedCodexAuth(ctx, client, id, false, *opts.Profile)
	}

	cmd := buildCodexExecCommand(opts, prompt)
	raw, err := execInContainerRaw(ctx, client, id, cmd, shared.ExecOptions{TTY: true, WorkDir: opts.Workdir})
	if opts.OutputOnly {
		out := extractCodexExecOutput(raw)
		if out == "" {
			out = strings.TrimSpace(raw)
		}
		_, _ = os.Stdout.Write([]byte(out))
	} else {
		_, _ = os.Stdout.Write([]byte(raw))
	}
	return err
}

func buildCodexExecCommand(opts codexExecOneOffOptions, prompt string) []string {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "gpt-5.2-codex"
	}
	effort := normalizeReasoningEffort(opts.Effort)
	if effort == "" {
		effort = "medium"
	}
	cmd := []string{"codex"}
	cmd = append(cmd, "-m", model)
	cmd = append(cmd, "-c", fmt.Sprintf("model_reasoning_effort=%s", effort))
	if strings.TrimSpace(opts.Workdir) != "" {
		cmd = append(cmd, "-C", opts.Workdir)
	}
	cmd = append(cmd, "--dangerously-bypass-approvals-and-sandbox", "exec", prompt)
	return cmd
}

func execInContainerRaw(ctx context.Context, client *shared.Client, containerID string, cmd []string, opts shared.ExecOptions) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := client.Exec(ctx, containerID, cmd, opts, nil, &stdout, &stderr); err != nil {
		return stdout.String() + stderr.String(), err
	}
	if opts.TTY {
		return stdout.String(), nil
	}
	if stderr.Len() > 0 {
		return stdout.String() + stderr.String(), nil
	}
	return stdout.String(), nil
}

func codexExecContainerName() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("si-codex-exec-%d-%d", time.Now().Unix(), rng.Intn(10_000))
}

func writeNoMcpConfig(model, effort string) (string, error) {
	dir, err := os.MkdirTemp("", "si-codex-exec-config-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	config := buildNoMcpConfig(model, effort)
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func buildNoMcpConfig(model, effort string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gpt-5.2-codex"
	}
	effort = normalizeReasoningEffort(effort)
	if effort == "" {
		effort = "medium"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf(`# managed by si-codex-exec (mcp disabled)
# Generated at %s.

model = "%s"
model_reasoning_effort = "%s"

[features]
web_search_request = false

[sandbox_workspace_write]
network_access = true
`, now, escapeConfigValue(model), escapeConfigValue(effort))
}

func escapeConfigValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

var codexAnsiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripCodexANSI(s string) string {
	return codexAnsiRe.ReplaceAllString(s, "")
}

func extractCodexExecOutput(raw string) string {
	clean := stripCodexANSI(raw)
	lines := strings.Split(clean, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			filtered = append(filtered, "")
			continue
		}
		if isNoiseLine(trimmed) {
			continue
		}
		if isBoxOnlyLine(trimmed) {
			continue
		}
		trimmed = trimBoxEdges(trimmed)
		if trimmed == "" || isBoxOnlyLine(trimmed) {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	blocks := splitBlocks(filtered)
	if len(blocks) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(blocks[len(blocks)-1], "\n"))
}

func splitBlocks(lines []string) [][]string {
	blocks := [][]string{}
	current := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = []string{}
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

func normalizeReasoningEffort(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "low":
		return "medium"
	case "medium", "high", "xhigh":
		return v
	default:
		return strings.TrimSpace(value)
	}
}

func isNoiseLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.HasPrefix(lower, "directory:"):
		return true
	case strings.HasPrefix(lower, "model:"):
		return true
	case strings.HasPrefix(lower, "openai codex"):
		return true
	case strings.HasPrefix(lower, "tip:"):
		return true
	case strings.HasPrefix(lower, "\u203a"):
		return true
	case strings.HasPrefix(lower, "\u21b3"):
		return true
	case strings.HasPrefix(lower, "\u2022 working"):
		return true
	case strings.HasPrefix(lower, "\u2022 preparing"):
		return true
	case strings.Contains(lower, "context left"):
		return true
	}
	return false
}

func trimBoxEdges(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		if r == '|' {
			return true
		}
		return isBoxRune(r)
	})
}

func isBoxOnlyLine(s string) bool {
	if strings.TrimSpace(s) == "" {
		return true
	}
	trimmed := strings.TrimSpace(s)
	for _, r := range trimmed {
		if r == '|' || isBoxRune(r) {
			continue
		}
		return false
	}
	return true
}

func isBoxRune(r rune) bool {
	switch r {
	case '\u2500', '\u2502', '\u255e', '\u2561', '\u2564', '\u2567', '\u256a', '\u256d', '\u256e', '\u256f', '\u2570':
		return true
	default:
		return false
	}
}
