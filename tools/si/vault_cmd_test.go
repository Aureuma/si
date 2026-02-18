package main

import (
	"slices"
	"testing"
)

func TestResolveSubcommandDispatchArgs_ExplicitArgs(t *testing.T) {
	called := false
	args, showUsage, ok := resolveSubcommandDispatchArgs([]string{"status"}, false, func() (string, bool) {
		called = true
		return "", false
	})
	if !ok {
		t.Fatalf("expected ok=true when args are provided")
	}
	if showUsage {
		t.Fatalf("did not expect showUsage when args are provided")
	}
	if called {
		t.Fatalf("did not expect selectFn to be called when args are provided")
	}
	if len(args) != 1 || args[0] != "status" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestResolveSubcommandDispatchArgs_NoArgsNonInteractiveShowsUsage(t *testing.T) {
	called := false
	args, showUsage, ok := resolveSubcommandDispatchArgs(nil, false, func() (string, bool) {
		called = true
		return "", false
	})
	if ok {
		t.Fatalf("expected ok=false in non-interactive mode with no args")
	}
	if !showUsage {
		t.Fatalf("expected showUsage=true in non-interactive mode with no args")
	}
	if called {
		t.Fatalf("did not expect selectFn call in non-interactive mode")
	}
	if args != nil {
		t.Fatalf("expected nil args, got %v", args)
	}
}

func TestResolveSubcommandDispatchArgs_NoArgsInteractiveSelection(t *testing.T) {
	called := false
	args, showUsage, ok := resolveSubcommandDispatchArgs(nil, true, func() (string, bool) {
		called = true
		return "set", true
	})
	if !ok {
		t.Fatalf("expected ok=true for interactive selection")
	}
	if showUsage {
		t.Fatalf("did not expect showUsage when selection succeeds")
	}
	if !called {
		t.Fatalf("expected selectFn to be called")
	}
	if len(args) != 1 || args[0] != "set" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestResolveSubcommandDispatchArgs_NoArgsInteractiveCancel(t *testing.T) {
	called := false
	args, showUsage, ok := resolveSubcommandDispatchArgs(nil, true, func() (string, bool) {
		called = true
		return "", false
	})
	if ok {
		t.Fatalf("expected ok=false when selection is canceled")
	}
	if showUsage {
		t.Fatalf("did not expect usage when selection is canceled")
	}
	if !called {
		t.Fatalf("expected selectFn to be called")
	}
	if args != nil {
		t.Fatalf("expected nil args when canceled, got %v", args)
	}
}

func TestVaultCommandActionSetsArePopulated(t *testing.T) {
	tests := []struct {
		name    string
		actions []subcommandAction
	}{
		{name: "vault", actions: vaultActions},
		{name: "vault docker", actions: vaultDockerActions},
		{name: "vault trust", actions: vaultTrustActions},
		{name: "vault recipients", actions: vaultRecipientsActions},
		{name: "vault hooks", actions: vaultHooksActions},
	}
	for _, tc := range tests {
		if len(tc.actions) == 0 {
			t.Fatalf("%s actions should not be empty", tc.name)
		}
		for _, action := range tc.actions {
			if action.Name == "" {
				t.Fatalf("%s action name should not be empty", tc.name)
			}
			if action.Description == "" {
				t.Fatalf("%s action description should not be empty", tc.name)
			}
		}
	}
}

func TestVaultActionNamesMatchDispatchSwitches(t *testing.T) {
	expectActionNames(t, "vault", vaultActions, []string{
		"init", "status", "check", "fmt", "encrypt", "decrypt", "set", "get", "list", "run", "docker", "trust", "recipients", "keygen", "use",
	})
	expectActionNames(t, "vault docker", vaultDockerActions, []string{"exec"})
	expectActionNames(t, "vault trust", vaultTrustActions, []string{"status", "accept", "forget"})
	expectActionNames(t, "vault recipients", vaultRecipientsActions, []string{"list", "add", "remove"})
	expectActionNames(t, "vault hooks", vaultHooksActions, []string{"install", "status", "uninstall"})
}

func expectActionNames(t *testing.T, name string, actions []subcommandAction, expected []string) {
	t.Helper()
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, action.Name)
	}
	if len(got) != len(expected) {
		t.Fatalf("%s action count mismatch: got=%v expected=%v", name, got, expected)
	}
	for _, want := range expected {
		if !slices.Contains(got, want) {
			t.Fatalf("%s missing action %q: got=%v", name, want, got)
		}
	}
}
