#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

GO_TEST_TIMEOUT="${SI_CODEX_UPGRADE_TEST_TIMEOUT:-15m}"

run_go_test() {
  local pkg="$1"
  local pattern="$2"
  echo "[preflight] go test ${pkg} -run '${pattern}'"
  go test "$pkg" -run "$pattern" -count=1 -timeout "$GO_TEST_TIMEOUT"
}

echo "[preflight] codex image compatibility checks (spawn + dyad + container runtime)"

# Shared container mount/layout guarantees used by both `si spawn` and `si dyad`.
run_go_test ./agents/shared/docker 'Test(BuildContainerCoreMounts|BuildDyadSpecs|BuildDyadEnvForwardsBrowserMCPOverrides|HostSiCodexProfileMounts|HasHostSiMount|HostVaultEnvFileMount|HasHostVaultEnvFileMount|HasDevelopmentMount|HasHostDevelopmentMount)'

# codex-init startup guarantees (skills + browser MCP URL resolution).
run_go_test ./tools/codex-init 'Test(SyncCodexSkills|ParseArgsExecForwarding|CollectGitSafeDirectories|BrowserMCPURLFromEnv)'

# SI CLI compatibility lanes for spawn/dyad/image build behavior.
run_go_test ./tools/si 'Test(CodexBrowserMCPURL|CodexContainerWorkspaceMatches|CodexContainerWorkspaceSource|SplitNameAndFlags|CodexRespawnBoolFlags|SplitDyadSpawnArgs|DyadProfileArg|DyadSkipAuthArg|DyadLoopBoolEnv|DyadLoopIntSetting|DockerBuildArgsIncludesSecret|RunDockerBuild|ShouldRetryLegacyBuild|CmdBuildImage)'

echo "[preflight] codex image compatibility checks passed"

