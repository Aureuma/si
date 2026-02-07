package main

import (
	"os"
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
