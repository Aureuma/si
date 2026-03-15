package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	siExperimentalRustCLIEnv = "SI_EXPERIMENTAL_RUST_CLI"
	siRustCLIBinEnv          = "SI_RUST_CLI_BIN"
)

var (
	rustCLIExecCommand = exec.Command
	rustCLILookPath    = exec.LookPath
	rustCLIRepoRoot    = repoRoot
)

type rustCodexSpawnPlanRequest struct {
	Name          string
	ProfileID     string
	Workspace     string
	Workdir       string
	CodexVolume   string
	SkillsVolume  string
	GHVolume      string
	Repo          string
	GHPAT         string
	DockerSocket  bool
	Detach        bool
	CleanSlate    bool
	Image         string
	Network       string
	VaultEnvFile  string
	IncludeHostSI bool
}

type rustCodexSpawnPlan struct {
	Name                   string                    `json:"name"`
	ContainerName          string                    `json:"container_name"`
	Image                  string                    `json:"image"`
	NetworkName            string                    `json:"network_name"`
	WorkspaceHost          string                    `json:"workspace_host"`
	WorkspacePrimaryTarget string                    `json:"workspace_primary_target"`
	WorkspaceMirrorTarget  string                    `json:"workspace_mirror_target"`
	Workdir                string                    `json:"workdir"`
	CodexVolume            string                    `json:"codex_volume"`
	SkillsVolume           string                    `json:"skills_volume"`
	GHVolume               string                    `json:"gh_volume"`
	DockerSocket           bool                      `json:"docker_socket"`
	CleanSlate             bool                      `json:"clean_slate"`
	Detach                 bool                      `json:"detach"`
	Env                    []string                  `json:"env"`
	Mounts                 []rustCodexSpawnPlanMount `json:"mounts"`
}

type rustCodexSpawnPlanMount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

func runVersionCommand() error {
	delegated, err := maybeDispatchRustCLIReadOnly("version")
	if err != nil {
		return err
	}
	if delegated {
		return nil
	}
	printVersion()
	return nil
}

func runHelpCommand(args []string) error {
	if len(args) <= 1 {
		delegated, err := maybeDispatchRustCLIReadOnly("help", args...)
		if err != nil {
			return err
		}
		if delegated {
			return nil
		}
	}
	usage()
	return nil
}

func runProvidersCharacteristicsCommand(args []string) (bool, error) {
	return maybeDispatchRustCLIReadOnly("providers", append([]string{"characteristics"}, args...)...)
}

func maybeBuildRustCodexSpawnPlan(request rustCodexSpawnPlanRequest) (*rustCodexSpawnPlan, bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return nil, false, nil
	}
	output, err := runRustCLIJSON(buildRustCodexSpawnPlanArgs(request)...)
	if err != nil {
		return nil, false, err
	}
	var plan rustCodexSpawnPlan
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, false, fmt.Errorf("decode rust codex spawn plan: %w", err)
	}
	return &plan, true, nil
}

func maybeDispatchRustCLIReadOnly(command string, args ...string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return false, err
	}
	cmd := rustCLIExecCommand(bin, append([]string{command}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("run rust si cli %q: %w", command, err)
	}
	return true, nil
}

func runRustCLIJSON(args ...string) ([]byte, error) {
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return nil, err
	}
	cmd := rustCLIExecCommand(bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return nil, fmt.Errorf("run rust si cli %q: %w: %s", strings.Join(args, " "), err, stderrText)
		}
		return nil, fmt.Errorf("run rust si cli %q: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func buildRustCodexSpawnPlanArgs(request rustCodexSpawnPlanRequest) []string {
	args := []string{
		"codex",
		"spawn-plan",
		"--format",
		"json",
		"--workspace",
		strings.TrimSpace(request.Workspace),
		"--docker-socket=" + strconv.FormatBool(request.DockerSocket),
		"--detach=" + strconv.FormatBool(request.Detach),
		"--clean-slate=" + strconv.FormatBool(request.CleanSlate),
		"--include-host-si=" + strconv.FormatBool(request.IncludeHostSI),
	}
	if value := strings.TrimSpace(request.Name); value != "" {
		args = append(args, "--name", value)
	}
	if value := strings.TrimSpace(request.ProfileID); value != "" {
		args = append(args, "--profile-id", value)
	}
	if value := strings.TrimSpace(request.Workdir); value != "" {
		args = append(args, "--workdir", value)
	}
	if value := strings.TrimSpace(request.CodexVolume); value != "" {
		args = append(args, "--codex-volume", value)
	}
	if value := strings.TrimSpace(request.SkillsVolume); value != "" {
		args = append(args, "--skills-volume", value)
	}
	if value := strings.TrimSpace(request.GHVolume); value != "" {
		args = append(args, "--gh-volume", value)
	}
	if value := strings.TrimSpace(request.Repo); value != "" {
		args = append(args, "--repo", value)
	}
	if value := strings.TrimSpace(request.GHPAT); value != "" {
		args = append(args, "--gh-pat", value)
	}
	if value := strings.TrimSpace(request.Image); value != "" {
		args = append(args, "--image", value)
	}
	if value := strings.TrimSpace(request.Network); value != "" {
		args = append(args, "--network", value)
	}
	if value := strings.TrimSpace(request.VaultEnvFile); value != "" {
		args = append(args, "--vault-env-file", value)
	}
	return args
}

func shouldUseExperimentalRustCLI() bool {
	if strings.TrimSpace(os.Getenv(siRustCLIBinEnv)) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(siExperimentalRustCLIEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func resolveRustCLIBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(siRustCLIBinEnv)); explicit != "" {
		path, err := resolveExecutablePath(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", siRustCLIBinEnv, err)
		}
		return path, nil
	}

	if root, err := rustCLIRepoRoot(); err == nil {
		candidate := filepath.Join(root, ".artifacts", "cargo-target", "debug", "si-rs")
		if path, err := resolveExecutablePath(candidate); err == nil {
			return path, nil
		}
	}

	path, err := rustCLILookPath("si-rs")
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf(
		"experimental Rust CLI enabled but no si-rs binary found; set %s or build rust/crates/si-cli",
		siRustCLIBinEnv,
	)
}

func resolveExecutablePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", abs)
	}
	return abs, nil
}
