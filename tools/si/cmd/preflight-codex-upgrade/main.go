package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	goTimeout := strings.TrimSpace(os.Getenv("SI_CODEX_UPGRADE_TEST_TIMEOUT"))
	if goTimeout == "" {
		goTimeout = "15m"
	}
	goBin := strings.TrimSpace(os.Getenv("SI_GO_BIN"))
	if goBin == "" {
		goBin = "go"
	}

	tests := []struct {
		pkg     string
		pattern string
	}{
		{"./agents/shared/docker", "Test(BuildContainerCoreMounts|BuildDyadSpecs|BuildDyadEnvForwardsBrowserMCPOverrides|HostSiCodexProfileMounts|HasHostSiMount|HostDockerConfigMount|HasHostDockerConfigMount|HostSiGoToolchainMount|HasHostSiGoToolchainMount|HostVaultEnvFileMount|HasHostVaultEnvFileMount|HasDevelopmentMount|HasHostDevelopmentMount)"},
		{"./tools/codex-init", "Test(SyncCodexSkills|ParseArgsExecForwarding|CollectGitSafeDirectories|BrowserMCPURLFromEnv)"},
		{"./tools/si", "Test(CodexBrowserMCPURL|CodexContainerWorkspaceMatches|CodexContainerWorkspaceSource|SplitNameAndFlags|CodexRespawnBoolFlags|SplitDyadSpawnArgs|DyadProfileArg|DyadSkipAuthArg|DyadLoopBoolEnv|DyadLoopIntSetting|DockerBuildArgsIncludesSecret|RunDockerBuild|ShouldRetryLegacyBuild|CmdBuildImage)"},
	}

	fmt.Println("[preflight] codex image compatibility checks (spawn + dyad + container runtime)")
	for _, tc := range tests {
		fmt.Printf("[preflight] %s test %s -run '%s'\n", goBin, tc.pkg, tc.pattern)
		cmd := exec.Command(goBin, "test", tc.pkg, "-run", tc.pattern, "-count=1", "-timeout", goTimeout)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}
	fmt.Println("[preflight] codex image compatibility checks passed")
}

func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.work")); err == nil {
		return cwd, nil
	}
	return "", fmt.Errorf("go.work not found; run from repo root")
}
