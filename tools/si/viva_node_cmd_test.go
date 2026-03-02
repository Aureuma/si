package main

import (
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
