package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/stripebridge"
)

func TestResolveStripeAccountSelection(t *testing.T) {
	settings := Settings{
		Stripe: StripeSettings{
			DefaultAccount: "alpha",
			Accounts: map[string]StripeAccountSetting{
				"alpha": {ID: "acct_alpha"},
			},
		},
	}
	alias, account, accountID := resolveStripeAccountSelection(settings, "")
	if alias != "alpha" || account.ID != "acct_alpha" || accountID != "acct_alpha" {
		t.Fatalf("unexpected selection alias=%q id=%q accountID=%q", alias, account.ID, accountID)
	}
}

func TestResolveStripeAPIKeyPrecedence(t *testing.T) {
	t.Setenv("SI_STRIPE_SANDBOX_API_KEY", "sk_sandbox_from_env")
	t.Setenv("SI_STRIPE_API_KEY", "sk_fallback")
	account := StripeAccountSetting{
		SandboxKey: "sk_sandbox_from_settings",
	}
	key, source := resolveStripeAPIKey(account, stripebridge.EnvSandbox)
	if key != "sk_sandbox_from_settings" || source != "settings.sandbox_key" {
		t.Fatalf("expected settings key precedence, got key=%q source=%q", key, source)
	}
}

func TestResolveStripeAPIKeyEnvReference(t *testing.T) {
	t.Setenv("MY_SANDBOX_KEY", "sk_sandbox_env_ref")
	account := StripeAccountSetting{
		SandboxKeyEnv: "MY_SANDBOX_KEY",
	}
	key, source := resolveStripeAPIKey(account, stripebridge.EnvSandbox)
	if key != "sk_sandbox_env_ref" || source != "env:MY_SANDBOX_KEY" {
		t.Fatalf("unexpected key source: %q (%q)", key, source)
	}
}

func TestResolveStripeRuntimeContextRejectsTestEnv(t *testing.T) {
	prev := os.Getenv("SI_STRIPE_API_KEY")
	t.Setenv("SI_STRIPE_API_KEY", "sk_sandbox_test")
	defer os.Setenv("SI_STRIPE_API_KEY", prev)
	_, err := resolveStripeRuntimeContext("", "test", "")
	if err == nil {
		t.Fatalf("expected test env rejection")
	}
}

func TestParseStripeEnvironment(t *testing.T) {
	if _, err := parseStripeEnvironment("sandbox"); err != nil {
		t.Fatalf("expected sandbox env to parse: %v", err)
	}
	if _, err := parseStripeEnvironment("test"); err == nil {
		t.Fatalf("expected test env rejection")
	}
}

func TestCmdStripeContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"contexts\":[{\"alias\":\"core\",\"id\":\"acct_core\"}]}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdStripeContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdStripeContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"account_id\":\"acct_core\",\"environment\":\"sandbox\",\"key_source\":\"settings.sandbox_key\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdStripeContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdStripeAuthStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"account_id\":\"acct_core\",\"environment\":\"sandbox\",\"key_source\":\"env:SI_STRIPE_API_KEY\",\"key_preview\":\"sk_test_...\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		cmdStripeAuthStatus([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "stripe\nauth\nstatus\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
