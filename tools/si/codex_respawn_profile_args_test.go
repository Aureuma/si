package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRespawnSpawnProfileArgs_InfersProfileFromContainerName(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--detach=false"},
		"profile-alpha",
		"",
		func(name string) (string, bool) {
			if name == "profile-alpha" {
				return "profile-alpha", true
			}
			return "", false
		},
	)
	if profile != "profile-alpha" {
		t.Fatalf("unexpected profile %q", profile)
	}
	want := []string{"--detach=false", "--profile", "profile-alpha"}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}

func TestNormalizeRespawnSpawnProfileArgs_DisablesDefaultProfileWhenNoProfileResolved(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--detach=false"},
		"custom",
		"",
		func(string) (string, bool) { return "", false },
	)
	if profile != "" {
		t.Fatalf("expected empty profile, got %q", profile)
	}
	want := []string{"--detach=false", "--profile="}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}

func TestNormalizeRespawnSpawnProfileArgs_PreservesExplicitProfileFlag(t *testing.T) {
	filtered, profile := normalizeRespawnSpawnProfileArgs(
		[]string{"--profile", "profile-gamma"},
		"profile-gamma",
		"profile-gamma",
		func(string) (string, bool) { return "", false },
	)
	if profile != "profile-gamma" {
		t.Fatalf("unexpected profile %q", profile)
	}
	want := []string{"--profile", "profile-gamma"}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], want[i])
		}
	}
}

func TestApplyRustCodexRespawnPlanUsesRustEffectiveNameProfileAndTargets(t *testing.T) {
	filtered, name, profile, targets := applyRustCodexRespawnPlan(
		[]string{"--detach=false", "--profile="},
		"custom",
		"",
		[]string{"custom"},
		&rustCodexRespawnPlan{
			EffectiveName: "profile-alpha",
			ProfileID:     "profile-alpha",
			RemoveTargets: []string{"alpha", "profile-alpha", "", "alpha"},
		},
		func(string) (string, bool) { return "", false },
	)
	if name != "profile-alpha" {
		t.Fatalf("unexpected name %q", name)
	}
	if profile != "profile-alpha" {
		t.Fatalf("unexpected profile %q", profile)
	}
	wantFiltered := []string{"--detach=false", "--profile", "profile-alpha"}
	if len(filtered) != len(wantFiltered) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(wantFiltered), filtered)
	}
	for i := range wantFiltered {
		if filtered[i] != wantFiltered[i] {
			t.Fatalf("unexpected filtered[%d]=%q want=%q", i, filtered[i], wantFiltered[i])
		}
	}
	wantTargets := []string{"alpha", "profile-alpha"}
	if len(targets) != len(wantTargets) {
		t.Fatalf("unexpected targets len=%d want=%d (%v)", len(targets), len(wantTargets), targets)
	}
	for i := range wantTargets {
		if targets[i] != wantTargets[i] {
			t.Fatalf("unexpected targets[%d]=%q want=%q", i, targets[i], wantTargets[i])
		}
	}
}

func TestCmdCodexRespawnUsesRustPlanForRemoveAndSpawnActions(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"effective_name\":\"ferma\",\"profile_id\":\"ferma\",\"remove_targets\":[\"alpha\",\"ferma\"]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	prevRemove := runCodexRemoveFn
	prevSpawn := runCodexSpawnFn
	t.Cleanup(func() {
		runCodexRemoveFn = prevRemove
		runCodexSpawnFn = prevSpawn
	})

	var removes [][]string
	var spawns [][]string
	runCodexRemoveFn = func(args []string) {
		removes = append(removes, append([]string(nil), args...))
	}
	runCodexSpawnFn = func(args []string) {
		spawns = append(spawns, append([]string(nil), args...))
	}

	cmdCodexRespawn([]string{"ferma", "--profile=", "--volumes", "--repo", "acme/repo"})

	if len(removes) != 2 {
		t.Fatalf("unexpected remove calls: %#v", removes)
	}
	wantRemoves := [][]string{
		{"--volumes", "alpha"},
		{"--volumes", "ferma"},
	}
	for i := range wantRemoves {
		if len(removes[i]) != len(wantRemoves[i]) {
			t.Fatalf("unexpected remove args[%d]=%v want %v", i, removes[i], wantRemoves[i])
		}
		for j := range wantRemoves[i] {
			if removes[i][j] != wantRemoves[i][j] {
				t.Fatalf("unexpected remove args[%d][%d]=%q want %q", i, j, removes[i][j], wantRemoves[i][j])
			}
		}
	}
	if len(spawns) != 1 {
		t.Fatalf("unexpected spawn calls: %#v", spawns)
	}
	wantSpawn := []string{"--repo", "acme/repo", "--profile", "ferma", "ferma"}
	if len(spawns[0]) != len(wantSpawn) {
		t.Fatalf("unexpected spawn args=%v want %v", spawns[0], wantSpawn)
	}
	for i := range wantSpawn {
		if spawns[0][i] != wantSpawn[i] {
			t.Fatalf("unexpected spawn args[%d]=%q want %q", i, spawns[0][i], wantSpawn[i])
		}
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	if !strings.Contains(argsText, "codex\nrespawn-plan\nferma\n--format\njson") {
		t.Fatalf("unexpected rust respawn args: %q", argsText)
	}
	if !strings.Contains(argsText, "--profile-container\nferma") {
		t.Fatalf("unexpected rust respawn args: %q", argsText)
	}
	if !strings.Contains(argsText, "--profile-id\nferma") {
		t.Fatalf("unexpected rust respawn args: %q", string(argsData))
	}
}

func TestCmdCodexRespawnProfileMatrix(t *testing.T) {
	cases := []struct {
		name         string
		profile      string
		removeTarget string
	}{
		{name: "ferma", profile: "ferma", removeTarget: "ferma-old"},
		{name: "berylla", profile: "berylla", removeTarget: "berylla-old"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			argsPath := filepath.Join(dir, "args.txt")
			scriptPath := filepath.Join(dir, "si-rs")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"effective_name\":\"" + tc.name + "\",\"profile_id\":\"" + tc.profile + "\",\"remove_targets\":[\"" + tc.removeTarget + "\",\"" + tc.name + "\"]}'\n"
			if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
				t.Fatalf("write script: %v", err)
			}

			t.Setenv(siRustCLIBinEnv, scriptPath)
			t.Setenv(siRustCLILegacyToggleEnv, "")

			prevRemove := runCodexRemoveFn
			prevSpawn := runCodexSpawnFn
			t.Cleanup(func() {
				runCodexRemoveFn = prevRemove
				runCodexSpawnFn = prevSpawn
			})

			var removes [][]string
			var spawns [][]string
			runCodexRemoveFn = func(args []string) {
				removes = append(removes, append([]string(nil), args...))
			}
			runCodexSpawnFn = func(args []string) {
				spawns = append(spawns, append([]string(nil), args...))
			}

			cmdCodexRespawn([]string{tc.name, "--profile=", "--volumes"})

			if len(removes) != 2 {
				t.Fatalf("unexpected remove calls: %#v", removes)
			}
			wantRemoves := [][]string{
				{"--volumes", tc.removeTarget},
				{"--volumes", tc.name},
			}
			for i := range wantRemoves {
				if strings.Join(removes[i], "\n") != strings.Join(wantRemoves[i], "\n") {
					t.Fatalf("unexpected remove args[%d]=%v want %v", i, removes[i], wantRemoves[i])
				}
			}
			if len(spawns) != 1 {
				t.Fatalf("unexpected spawn calls: %#v", spawns)
			}
			wantSpawn := []string{"--profile", tc.profile, tc.name}
			if strings.Join(spawns[0], "\n") != strings.Join(wantSpawn, "\n") {
				t.Fatalf("unexpected spawn args=%v want %v", spawns[0], wantSpawn)
			}

			argsData, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatalf("read args file: %v", err)
			}
			argsText := string(argsData)
			if !strings.Contains(argsText, "codex\nrespawn-plan\n"+tc.name+"\n--format\njson") {
				t.Fatalf("unexpected rust respawn args: %q", argsText)
			}
			if !strings.Contains(argsText, "--profile-id\n"+tc.profile) {
				t.Fatalf("unexpected rust respawn args: %q", argsText)
			}
		})
	}
}
