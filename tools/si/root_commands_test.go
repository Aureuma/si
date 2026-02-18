package main

import "testing"

func TestDispatchRootCommandAliases(t *testing.T) {
	resetRootCommandHandlersForTest()
	handlers := getRootCommandHandlers()
	expected := []string{
		"version", "--version", "-v",
		"spawn", "respawn", "list", "ps", "status", "report", "login", "logout", "exec", "run", "logs", "tail", "clone", "remove", "rm", "delete", "stop", "start", "warmup",
		"analyze", "lint",
		"stripe",
		"vault", "creds",
		"github",
		"cloudflare", "cf",
		"google",
		"apple",
		"social",
		"workos",
		"aws",
		"gcp",
		"openai",
		"oci",
		"image", "images",
		"publish", "pub",
		"providers", "provider", "integrations", "apis",
		"docker",
		"browser",
		"dyad",
		"build",
		"paas",
		"persona",
		"skill",
		"help", "-h", "--help",
	}
	for _, cmd := range expected {
		if _, ok := handlers[cmd]; !ok {
			t.Fatalf("missing root command alias: %s", cmd)
		}
	}
	if dispatchRootCommand("definitely-unknown-command", nil) {
		t.Fatalf("unknown command should not dispatch")
	}
}
