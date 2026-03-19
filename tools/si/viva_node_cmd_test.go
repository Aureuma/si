package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeVivaNodeProfileDefaults(t *testing.T) {
	profile := normalizeVivaNodeProfile(VivaNodeProfile{})
	if profile.Port != "22" {
		t.Fatalf("unexpected default port: %q", profile.Port)
	}
	if profile.StrictHostKeyChecking != "yes" {
		t.Fatalf("unexpected strict host key checking default: %q", profile.StrictHostKeyChecking)
	}
	if profile.ConnectTimeoutSeconds != 10 {
		t.Fatalf("unexpected connect timeout default: %d", profile.ConnectTimeoutSeconds)
	}
	if profile.ServerAliveIntervalSeconds != 30 {
		t.Fatalf("unexpected server alive interval default: %d", profile.ServerAliveIntervalSeconds)
	}
	if profile.ServerAliveCountMax != 5 {
		t.Fatalf("unexpected server alive count default: %d", profile.ServerAliveCountMax)
	}
	if profile.Compression == nil || !*profile.Compression {
		t.Fatalf("expected compression default true")
	}
	if profile.Multiplex == nil || !*profile.Multiplex {
		t.Fatalf("expected multiplex default true")
	}
	if profile.ControlPersist != "5m" {
		t.Fatalf("unexpected control persist default: %q", profile.ControlPersist)
	}
	if profile.ControlPath != "~/.ssh/cm-si-viva-%C" {
		t.Fatalf("unexpected control path default: %q", profile.ControlPath)
	}
	if profile.Protocols.SSH == nil || !*profile.Protocols.SSH {
		t.Fatalf("expected ssh protocol default true")
	}
	if profile.Protocols.Mosh == nil || !*profile.Protocols.Mosh {
		t.Fatalf("expected mosh protocol default true")
	}
	if profile.Protocols.Rsync == nil || !*profile.Protocols.Rsync {
		t.Fatalf("expected rsync protocol default true")
	}
}

func TestResolveVivaNodeConfigReference(t *testing.T) {
	orig := resolveVivaNodeVaultKeyValue
	defer func() { resolveVivaNodeVaultKeyValue = orig }()

	resolveVivaNodeVaultKeyValue = func(_ Settings, key string) (string, bool) {
		if key == "HOST_FROM_VAULT" {
			return "vault.example.com", true
		}
		return "", false
	}

	t.Setenv("HOST_FROM_ENV", "env.example.com")
	settings := defaultSettings()

	if got := resolveVivaNodeConfigReference(settings, "HOST_FROM_ENV", ""); got != "env.example.com" {
		t.Fatalf("expected env-key resolution, got %q", got)
	}
	if got := resolveVivaNodeConfigReference(settings, "", "env:HOST_FROM_VAULT"); got != "vault.example.com" {
		t.Fatalf("expected env: ref vault resolution, got %q", got)
	}
	if got := resolveVivaNodeConfigReference(settings, "", "${HOST_FROM_ENV}"); got != "env.example.com" {
		t.Fatalf("expected ${} env resolution, got %q", got)
	}
	if got := resolveVivaNodeConfigReference(settings, "", "literal-host"); got != "literal-host" {
		t.Fatalf("expected literal passthrough, got %q", got)
	}
}

func TestResolveVivaNodeConnection(t *testing.T) {
	orig := resolveVivaNodeVaultKeyValue
	defer func() { resolveVivaNodeVaultKeyValue = orig }()
	resolveVivaNodeVaultKeyValue = func(_ Settings, key string) (string, bool) {
		switch key {
		case "SSH_HOST_KEY":
			return "host.example.com", true
		case "SSH_USER_KEY":
			return "deploy", true
		case "SSH_PORT_KEY":
			return "7129", true
		default:
			return "", false
		}
	}
	settings := defaultSettings()
	entry := VivaNodeProfile{
		HostEnvKey: "SSH_HOST_KEY",
		UserEnvKey: "SSH_USER_KEY",
		PortEnvKey: "SSH_PORT_KEY",
	}
	conn, err := resolveVivaNodeConnection(settings, "prod", entry, vivaNodeConnectionOverrides{})
	if err != nil {
		t.Fatalf("resolveVivaNodeConnection: %v", err)
	}
	if conn.Host != "host.example.com" || conn.User != "deploy" || conn.Port != "7129" {
		t.Fatalf("unexpected resolved connection: %#v", conn)
	}
}

func TestBuildVivaNodeSSHArgs(t *testing.T) {
	conn := vivaNodeConnection{
		Host:                       "host.example.com",
		User:                       "deploy",
		Port:                       "7129",
		StrictHostKeyChecking:      "yes",
		ConnectTimeoutSeconds:      10,
		ServerAliveIntervalSeconds: 30,
		ServerAliveCountMax:        5,
		Compression:                true,
		Multiplex:                  true,
		ControlPersist:             "5m",
		ControlPath:                "~/.ssh/cm-si-viva-%C",
	}
	args := buildVivaNodeSSHArgs(conn, []string{"uname", "-a"})
	joined := strings.Join(args, " ")
	checks := []string{
		"-p 7129",
		"StrictHostKeyChecking=yes",
		"ConnectTimeout=10",
		"ServerAliveInterval=30",
		"ServerAliveCountMax=5",
		"Compression=yes",
		"ControlMaster=auto",
		"ControlPersist=5m",
		"ControlPath=~/.ssh/cm-si-viva-%C",
		"deploy@host.example.com",
		"uname -a",
	}
	for _, needle := range checks {
		if !strings.Contains(joined, needle) {
			t.Fatalf("expected ssh args to include %q: %q", needle, joined)
		}
	}
}

func TestBuildVivaNodeRsyncArgs(t *testing.T) {
	conn := vivaNodeConnection{
		Host:                       "host.example.com",
		User:                       "deploy",
		Port:                       "7129",
		StrictHostKeyChecking:      "yes",
		ConnectTimeoutSeconds:      10,
		ServerAliveIntervalSeconds: 30,
		ServerAliveCountMax:        5,
		Compression:                true,
		Multiplex:                  true,
		ControlPersist:             "5m",
		ControlPath:                "~/.ssh/cm-si-viva-%C",
	}
	push := buildVivaNodeRsyncArgs(conn, "./local", "~/remote", false, true, true)
	joinedPush := strings.Join(push, " ")
	if !strings.Contains(joinedPush, "--delete") || !strings.Contains(joinedPush, "--dry-run") {
		t.Fatalf("expected delete and dry-run flags in push args: %q", joinedPush)
	}
	if !strings.Contains(joinedPush, "./local") || !strings.Contains(joinedPush, "deploy@host.example.com:~/remote") {
		t.Fatalf("unexpected push rsync args: %q", joinedPush)
	}

	pull := buildVivaNodeRsyncArgs(conn, "~/remote", "./local", true, false, false)
	joinedPull := strings.Join(pull, " ")
	if !strings.Contains(joinedPull, "deploy@host.example.com:~/remote") || !strings.Contains(joinedPull, " ./local") {
		t.Fatalf("unexpected pull rsync args: %q", joinedPull)
	}
}

func TestResolveVivaNodeSelectionUsesDefault(t *testing.T) {
	settings := defaultSettings()
	settings.Viva.Node.DefaultNode = "prod"
	settings.Viva.Node.Entries = map[string]VivaNodeProfile{
		"prod": {Host: "host.example.com", User: "deploy"},
		"dev":  {Host: "dev.example.com", User: "dev"},
	}
	key, _, err := resolveVivaNodeSelection(settings, "", "ssh")
	if err != nil {
		t.Fatalf("resolveVivaNodeSelection: %v", err)
	}
	if key != "prod" {
		t.Fatalf("expected default node prod, got %q", key)
	}
}

func TestResolveVivaNodeBootstrapRepos(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "si"), 0o755); err != nil {
		t.Fatalf("mkdir si repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "safe"), 0o755); err != nil {
		t.Fatalf("mkdir safe repo: %v", err)
	}
	origRemote := resolveVivaNodeGitRemoteOriginURL
	defer func() { resolveVivaNodeGitRemoteOriginURL = origRemote }()
	resolveVivaNodeGitRemoteOriginURL = func(path string) (string, error) {
		return "git@github.com:aureuma/" + filepath.Base(path) + ".git", nil
	}
	repos, err := resolveVivaNodeBootstrapRepos(root, []string{"si", "safe"})
	if err != nil {
		t.Fatalf("resolveVivaNodeBootstrapRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].Name != "si" || repos[1].Name != "safe" {
		t.Fatalf("unexpected repo list: %#v", repos)
	}
	if repos[0].RemoteURL == "" || repos[1].RemoteURL == "" {
		t.Fatalf("expected remote urls in repos: %#v", repos)
	}
}

func TestResolveVivaNodeBootstrapRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "si"), 0o755); err != nil {
		t.Fatalf("mkdir si repo: %v", err)
	}
	origRemote := resolveVivaNodeGitRemoteOriginURL
	defer func() { resolveVivaNodeGitRemoteOriginURL = origRemote }()
	resolveVivaNodeGitRemoteOriginURL = func(path string) (string, error) {
		return "git@github.com:aureuma/" + filepath.Base(path) + ".git", nil
	}
	t.Setenv("GH_PAT_AUREUMA", "ghp_test_123")
	runtime, err := resolveVivaNodeBootstrapRuntime(defaultSettings(), normalizeVivaNodeBootstrapSettings(VivaNodeBootstrapSettings{
		SourceRoot: root,
		Repos:      []string{"si"},
	}), true)
	if err != nil {
		t.Fatalf("resolveVivaNodeBootstrapRuntime: %v", err)
	}
	if runtime.SourceRoot != root {
		t.Fatalf("unexpected source root: %q", runtime.SourceRoot)
	}
	if len(runtime.Repos) != 1 || runtime.Repos[0].Name != "si" {
		t.Fatalf("unexpected repos: %#v", runtime.Repos)
	}
	if runtime.Secrets.GitHubToken == "" {
		t.Fatalf("expected secrets to resolve, got: %#v", runtime.Secrets)
	}
}

func TestResolveVivaNodeBootstrapRuntimeAllowsMissingSecretsInDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "si"), 0o755); err != nil {
		t.Fatalf("mkdir si repo: %v", err)
	}
	origRemote := resolveVivaNodeGitRemoteOriginURL
	defer func() { resolveVivaNodeGitRemoteOriginURL = origRemote }()
	resolveVivaNodeGitRemoteOriginURL = func(path string) (string, error) {
		return "git@github.com:aureuma/" + filepath.Base(path) + ".git", nil
	}
	runtime, err := resolveVivaNodeBootstrapRuntime(defaultSettings(), normalizeVivaNodeBootstrapSettings(VivaNodeBootstrapSettings{
		SourceRoot: root,
		Repos:      []string{"si"},
	}), false)
	if err != nil {
		t.Fatalf("resolveVivaNodeBootstrapRuntime dry-run: %v", err)
	}
	if runtime.Secrets.GitHubToken != "" {
		t.Fatalf("expected empty secrets in dry-run mode, got: %#v", runtime.Secrets)
	}
}

func TestApplyVivaNodeBootstrapPathDefaultsUsesWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	settings := defaultSettings()
	settings.Paths.WorkspaceRoot = root

	cfg, err := applyVivaNodeBootstrapPathDefaults(&settings, VivaNodeBootstrapSettings{}, t.TempDir(), false)
	if err != nil {
		t.Fatalf("applyVivaNodeBootstrapPathDefaults() unexpected err: %v", err)
	}
	if cfg.SourceRoot != root {
		t.Fatalf("unexpected source root: %q", cfg.SourceRoot)
	}
	if cfg.WorkspaceDir != filepath.Join("~", filepath.Base(root)) {
		t.Fatalf("unexpected workspace dir: %q", cfg.WorkspaceDir)
	}
}

func TestBuildVivaNodeBootstrapScript(t *testing.T) {
	script := buildVivaNodeBootstrapScript(vivaNodeBootstrapRuntime{
		WorkspaceDir:    "~/Development",
		ShellProfile:    "~/.bashrc",
		EnvFile:         "~/.si/node-bootstrap.env",
		BuildSI:         true,
		PullLatest:      true,
		InstallOrbitals: []string{"remote-control"},
		Repos: []vivaNodeBootstrapRepo{
			{Name: "si", RemoteURL: "git@github.com:aureuma/si.git"},
		},
		Secrets: vivaNodeBootstrapSecrets{
			GitHubToken: "ghp_test",
		},
	})
	checks := []string{
		"set -euo pipefail",
		"git clone git@github.com:aureuma/si.git \"$WORKSPACE_DIR/si\"",
		"cargo build --locked --manifest-path rust/crates/si-cli/Cargo.toml --bin si-rs --target-dir .artifacts/cargo-target",
		"cp .artifacts/cargo-target/debug/si-rs bin/si",
		"$SI_BIN orbits install remote-control",
		"export GH_PAT_AUREUMA=ghp_test",
	}
	for _, needle := range checks {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected script to include %q, got:\n%s", needle, script)
		}
	}
}

func TestCmdVivaNodeBootstrapDryRunJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", tmp)
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "si"), 0o755); err != nil {
		t.Fatalf("mkdir si repo: %v", err)
	}
	settings := defaultSettings()
	settings.Viva.Node.DefaultNode = "dev"
	settings.Viva.Node.Entries = map[string]VivaNodeProfile{
		"dev": {Host: "host.example.com", User: "deploy", Port: "7129"},
	}
	settings.Viva.Node.Bootstrap = normalizeVivaNodeBootstrapSettings(VivaNodeBootstrapSettings{
		SourceRoot: sourceRoot,
		Repos:      []string{"si"},
	})
	if err := saveSettings(settings); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	t.Setenv("GH_PAT_AUREUMA", "ghp_test_123")
	origRemote := resolveVivaNodeGitRemoteOriginURL
	defer func() { resolveVivaNodeGitRemoteOriginURL = origRemote }()
	resolveVivaNodeGitRemoteOriginURL = func(path string) (string, error) {
		return "git@github.com:aureuma/" + filepath.Base(path) + ".git", nil
	}
	origSSH := runVivaNodeSSHExternalWithInput
	defer func() { runVivaNodeSSHExternalWithInput = origSSH }()
	runVivaNodeSSHExternalWithInput = func(_ string, _ []string, _ string) error {
		t.Fatalf("ssh should not run in dry-run mode")
		return nil
	}
	out := captureOutputForTest(t, func() {
		cmdVivaNode([]string{"bootstrap", "--node", "dev", "--dry-run", "--json"})
	})
	if !strings.Contains(out, "\"ok\": true") || !strings.Contains(out, "\"dry_run\": true") {
		t.Fatalf("unexpected json output: %q", out)
	}
}

func TestCmdVivaNodeBootstrapExecutesSSH(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", tmp)
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "si"), 0o755); err != nil {
		t.Fatalf("mkdir si repo: %v", err)
	}
	settings := defaultSettings()
	settings.Viva.Node.DefaultNode = "dev"
	settings.Viva.Node.Entries = map[string]VivaNodeProfile{
		"dev": {Host: "host.example.com", User: "deploy", Port: "7129"},
	}
	settings.Viva.Node.Bootstrap = normalizeVivaNodeBootstrapSettings(VivaNodeBootstrapSettings{
		SourceRoot: sourceRoot,
		Repos:      []string{"si"},
	})
	if err := saveSettings(settings); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	t.Setenv("GH_PAT_AUREUMA", "ghp_test_123")
	origRemote := resolveVivaNodeGitRemoteOriginURL
	defer func() { resolveVivaNodeGitRemoteOriginURL = origRemote }()
	resolveVivaNodeGitRemoteOriginURL = func(path string) (string, error) {
		return "git@github.com:aureuma/" + filepath.Base(path) + ".git", nil
	}
	origSSH := runVivaNodeSSHExternalWithInput
	defer func() { runVivaNodeSSHExternalWithInput = origSSH }()
	called := false
	receivedArgs := []string(nil)
	receivedScript := ""
	runVivaNodeSSHExternalWithInput = func(_ string, args []string, input string) error {
		called = true
		receivedArgs = append([]string{}, args...)
		receivedScript = input
		return nil
	}
	_ = captureOutputForTest(t, func() {
		cmdVivaNode([]string{"bootstrap", "--node", "dev", "--json"})
	})
	if !called {
		t.Fatalf("expected ssh bootstrap runner to be called")
	}
	joined := strings.Join(receivedArgs, " ")
	if !strings.Contains(joined, "deploy@host.example.com") || !strings.Contains(joined, "bash -se") {
		t.Fatalf("unexpected ssh args: %q", joined)
	}
	if !strings.Contains(receivedScript, "cargo build --locked --manifest-path rust/crates/si-cli/Cargo.toml --bin si-rs --target-dir .artifacts/cargo-target") {
		t.Fatalf("expected script to build si, got:\n%s", receivedScript)
	}
}
