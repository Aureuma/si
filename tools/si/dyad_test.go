package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"

	shared "si/agents/shared/docker"
)

func TestNormalizeDyadCommandAliases(t *testing.T) {
	cases := map[string]string{
		"spawn":  "spawn",
		"run":    "exec",
		" RUN ":  "exec",
		"exec":   "exec",
		"ps":     "list",
		"rm":     "remove",
		"delete": "remove",
		"start":  "start",
		"up":     "start",
		"down":   "stop",
		"stop":   "stop",
	}
	for in, want := range cases {
		got := normalizeDyadCommand(in)
		if got != want {
			t.Fatalf("normalizeDyadCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDyadMember(t *testing.T) {
	member, err := normalizeDyadMember("", "critic")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if member != "critic" {
		t.Fatalf("unexpected member %q", member)
	}

	member, err = normalizeDyadMember("ACTOR", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if member != "actor" {
		t.Fatalf("unexpected member %q", member)
	}

	if _, err := normalizeDyadMember("observer", "actor"); err == nil {
		t.Fatalf("expected invalid member error")
	}
}

func TestParseMenuSelection(t *testing.T) {
	options := []string{"alpha", "beta", "gamma"}

	idx, err := parseMenuSelection("2", options)
	if err != nil || idx != 1 {
		t.Fatalf("numeric selection mismatch: idx=%d err=%v", idx, err)
	}

	idx, err = parseMenuSelection("BETA", options)
	if err != nil || idx != 1 {
		t.Fatalf("name selection mismatch: idx=%d err=%v", idx, err)
	}

	idx, err = parseMenuSelection("/gamma", options)
	if err != nil || idx != 2 {
		t.Fatalf("slash name selection mismatch: idx=%d err=%v", idx, err)
	}

	idx, err = parseMenuSelection("", options)
	if err != nil || idx != -1 {
		t.Fatalf("empty selection mismatch: idx=%d err=%v", idx, err)
	}

	idx, err = parseMenuSelection("\x1b", options)
	if err != nil || idx != -1 {
		t.Fatalf("esc selection mismatch: idx=%d err=%v", idx, err)
	}

	idx, err = parseMenuSelection("\x1b[A", options)
	if err != nil || idx != -1 {
		t.Fatalf("esc sequence selection mismatch: idx=%d err=%v", idx, err)
	}

	if _, err := parseMenuSelection("0", options); err == nil {
		t.Fatalf("expected out-of-range error")
	}
	if _, err := parseMenuSelection("99", options); err == nil {
		t.Fatalf("expected out-of-range error")
	}
	if _, err := parseMenuSelection("nope", options); err == nil {
		t.Fatalf("expected invalid name error")
	}
}

func TestPrintDyadRowsIncludesIndexWhenRequested(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = false
	defer func() { ansiEnabled = prev }()

	rows := []dyadRow{
		{Dyad: "alpha", Role: "generic", Actor: "running", Critic: "exited"},
	}
	out := captureOutputForTest(t, func() {
		printDyadRows(rows, true)
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and row, got %q", out)
	}
	if !strings.Contains(lines[0], "#") {
		t.Fatalf("expected index column in header, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "1") || !strings.Contains(lines[1], "alpha") {
		t.Fatalf("expected numbered dyad row, got %q", lines[1])
	}
}

func TestPrintDyadRowsOmitsIndexWhenNotRequested(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = false
	defer func() { ansiEnabled = prev }()

	rows := []dyadRow{
		{Dyad: "beta", Role: "generic", Actor: "running", Critic: "running"},
	}
	out := captureOutputForTest(t, func() {
		printDyadRows(rows, false)
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and row, got %q", out)
	}
	if strings.Contains(lines[0], "#") {
		t.Fatalf("did not expect index column in header, got %q", lines[0])
	}
}

func TestSplitDyadSpawnArgsKeepsFlagsParsable(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"generic",
		"qa",
		"--forward-ports", "1455-1455",
		"--docker-socket=false",
		"--configs", "/tmp/configs",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--forward-ports", "1455-1455",
		"--docker-socket=false",
		"--configs", "/tmp/configs",
		"generic",
		"qa",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsFlagsBeforeName(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"--role", "infra",
		"demo",
		"--docker-socket",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--role", "infra",
		"--docker-socket",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsNoName(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"--role", "research",
	})
	if name != "" {
		t.Fatalf("expected empty name, got %q", name)
	}
	want := []string{
		"--role", "research",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsBoolFlagDoesNotConsumePositional(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--docker-socket",
		"infra",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--docker-socket",
		"infra",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsBoolFlagWithSeparateValue(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--docker-socket", "false",
		"infra",
		"platform",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--docker-socket=false",
		"infra",
		"platform",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestMaybeApplyRustDyadSpawnPlanMutatesOptionsWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"role\":\"ios\",\"network_name\":\"si\",\"workspace_host\":\"/workspace\",\"configs_host\":\"/configs-src\",\"codex_volume\":\"si-codex-alpha\",\"skills_volume\":\"si-codex-skills\",\"forward_ports\":\"1455-1465\",\"docker_socket\":true,\"actor\":{\"member\":\"actor\",\"container_name\":\"si-actor-alpha\",\"image\":\"actor:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]},\"critic\":{\"member\":\"critic\",\"container_name\":\"si-critic-alpha\",\"image\":\"critic:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	opts := &shared.DyadOptions{
		Dyad:          "alpha",
		Role:          "generic",
		ActorImage:    "actor:old",
		CriticImage:   "critic:old",
		WorkspaceHost: "/workspace",
		ConfigsHost:   "/configs-old",
		SkillsVolume:  "skills-old",
		ForwardPorts:  "9999-9999",
		Network:       "old-net",
	}
	if err := maybeApplyRustDyadSpawnPlan(opts); err != nil {
		t.Fatalf("maybeApplyRustDyadSpawnPlan: %v", err)
	}
	if opts.Role != "ios" || opts.ConfigsHost != "/configs-src" || opts.ForwardPorts != "1455-1465" {
		t.Fatalf("unexpected mutated options: %+v", opts)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "dyad\nspawn-plan\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestRemoveDyadWithCompatibilityDelegatesToRustWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := removeDyadWithCompatibility(context.Background(), nil, "alpha")
	if err != nil {
		t.Fatalf("removeDyadWithCompatibility: %v", err)
	}
	if !delegated {
		t.Fatalf("expected removeDyadWithCompatibility to delegate")
	}
	if strings.TrimSpace(output) != "removed" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nremove\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestRemoveDyadWithCompatibilityRequiresClientWithoutRust(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, "")
	t.Setenv(siExperimentalRustCLIEnv, "")

	_, delegated, err := removeDyadWithCompatibility(context.Background(), nil, "alpha")
	if err == nil {
		t.Fatalf("expected missing client error")
	}
	if delegated {
		t.Fatalf("did not expect delegation without Rust")
	}
	if !strings.Contains(err.Error(), "client required") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestSplitDyadSpawnArgsSkipAuthBoolDoesNotConsumeNextFlag(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--skip-auth",
		"--actor-image", "aureuma/si:local",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--skip-auth",
		"--actor-image", "aureuma/si:local",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsSkipAuthBoolWithSeparateValue(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--skip-auth", "false",
		"--actor-image", "aureuma/si:local",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--skip-auth=false",
		"--actor-image", "aureuma/si:local",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsAutopilotBoolDoesNotConsumeNextFlag(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--autopilot",
		"--actor-image", "aureuma/si:local",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--autopilot",
		"--actor-image", "aureuma/si:local",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestSplitDyadSpawnArgsAutopilotBoolWithSeparateValue(t *testing.T) {
	name, filtered := splitDyadSpawnArgs([]string{
		"demo",
		"--autopilot", "false",
		"--actor-image", "aureuma/si:local",
	})
	if name != "demo" {
		t.Fatalf("unexpected name %q", name)
	}
	want := []string{
		"--autopilot=false",
		"--actor-image", "aureuma/si:local",
	}
	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered len=%d want=%d (%v)", len(filtered), len(want), filtered)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered[%d]=%q want %q (%v)", i, filtered[i], want[i], filtered)
		}
	}
}

func TestIsBoolLiteral(t *testing.T) {
	for _, input := range []string{"true", "false", "1", "0", "t", "f", " TRUE "} {
		if !isBoolLiteral(input) {
			t.Fatalf("expected %q to be a bool literal", input)
		}
	}
	for _, input := range []string{"yes", "no", "on", "off", "", "2"} {
		if isBoolLiteral(input) {
			t.Fatalf("expected %q to not be a bool literal", input)
		}
	}
}

func TestValidateDyadSpawnOptionValue(t *testing.T) {
	if err := validateDyadSpawnOptionValue("role", "infra"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := validateDyadSpawnOptionValue("role", ""); err == nil {
		t.Fatalf("expected missing value error")
	}
	if err := validateDyadSpawnOptionValue("role", "--profile"); err == nil {
		t.Fatalf("expected flag-like value error")
	}
}

func TestDyadProfileArg(t *testing.T) {
	if value, ok := dyadProfileArg([]string{"--profile", "profile-beta"}); !ok || value != "profile-beta" {
		t.Fatalf("expected --profile value, got ok=%v value=%q", ok, value)
	}
	if value, ok := dyadProfileArg([]string{"--profile=profile-delta"}); !ok || value != "profile-delta" {
		t.Fatalf("expected --profile= value, got ok=%v value=%q", ok, value)
	}
	if _, ok := dyadProfileArg([]string{"--role", "generic"}); ok {
		t.Fatalf("expected missing profile to return ok=false")
	}
}

func TestDyadSkipAuthArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare flag", args: []string{"--skip-auth"}, want: true},
		{name: "equals true", args: []string{"--skip-auth=true"}, want: true},
		{name: "equals false", args: []string{"--skip-auth=false"}, want: false},
		{name: "separate true", args: []string{"--skip-auth", "true"}, want: true},
		{name: "separate false", args: []string{"--skip-auth", "false"}, want: false},
		{name: "invalid literal", args: []string{"--skip-auth=maybe"}, want: false},
		{name: "missing", args: []string{"--profile", "profile-beta"}, want: false},
	}
	for _, tc := range cases {
		got := dyadSkipAuthArg(tc.args)
		if got != tc.want {
			t.Fatalf("%s: dyadSkipAuthArg(%v)=%v want %v", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestDyadLoopBoolEnv(t *testing.T) {
	t.Setenv("DYAD_LOOP_ENABLED", "true")
	if got, ok := dyadLoopBoolEnv("DYAD_LOOP_ENABLED"); !ok || !got {
		t.Fatalf("expected parsed true, got ok=%v val=%v", ok, got)
	}

	t.Setenv("DYAD_LOOP_ENABLED", "0")
	if got, ok := dyadLoopBoolEnv("DYAD_LOOP_ENABLED"); !ok || got {
		t.Fatalf("expected parsed false, got ok=%v val=%v", ok, got)
	}

	t.Setenv("DYAD_LOOP_ENABLED", "not-a-bool")
	if _, ok := dyadLoopBoolEnv("DYAD_LOOP_ENABLED"); ok {
		t.Fatalf("expected invalid value to return ok=false")
	}
}

func TestDyadLoopIntSetting(t *testing.T) {
	t.Setenv("DYAD_LOOP_MAX_TURNS", "7")
	if got := dyadLoopIntSetting("DYAD_LOOP_MAX_TURNS", 3); got != 7 {
		t.Fatalf("expected env value 7, got %d", got)
	}

	t.Setenv("DYAD_LOOP_MAX_TURNS", "invalid")
	if got := dyadLoopIntSetting("DYAD_LOOP_MAX_TURNS", 3); got != 3 {
		t.Fatalf("expected fallback value 3, got %d", got)
	}

	t.Setenv("DYAD_LOOP_MAX_TURNS", "")
	if got := dyadLoopIntSetting("DYAD_LOOP_MAX_TURNS", 9); got != 9 {
		t.Fatalf("expected empty env fallback value 9, got %d", got)
	}
}

func TestBuildDyadRowsAggregatesAndSorts(t *testing.T) {
	containers := []types.Container{
		{
			Labels: map[string]string{
				shared.LabelDyad:   "beta",
				shared.LabelRole:   "research",
				shared.LabelMember: "critic",
			},
			State: "running",
		},
		{
			Labels: map[string]string{
				shared.LabelDyad:   "alpha",
				shared.LabelRole:   "infra",
				shared.LabelMember: "actor",
			},
			State: "running",
		},
		{
			Labels: map[string]string{
				shared.LabelDyad:   "alpha",
				shared.LabelMember: "critic",
			},
			State: "exited",
		},
		{
			Labels: map[string]string{
				shared.LabelDyad:   "beta",
				shared.LabelMember: "actor",
			},
			State: "paused",
		},
		{
			Labels: map[string]string{
				shared.LabelMember: "actor",
			},
			State: "running",
		},
	}

	rows := buildDyadRows(containers)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Dyad != "alpha" || rows[1].Dyad != "beta" {
		t.Fatalf("expected sorted rows by dyad name, got %+v", rows)
	}
	if rows[0].Role != "infra" {
		t.Fatalf("unexpected alpha metadata: %+v", rows[0])
	}
	if rows[0].Actor != "running" || rows[0].Critic != "exited" {
		t.Fatalf("unexpected alpha member states: %+v", rows[0])
	}
	if rows[1].Role != "research" {
		t.Fatalf("unexpected beta metadata: %+v", rows[1])
	}
	if rows[1].Actor != "paused" || rows[1].Critic != "running" {
		t.Fatalf("unexpected beta member states: %+v", rows[1])
	}
}

func TestBuildDyadRowsUnknownMemberIgnored(t *testing.T) {
	containers := []types.Container{
		{
			Labels: map[string]string{
				shared.LabelDyad:   "alpha",
				shared.LabelRole:   "infra",
				shared.LabelMember: "observer",
			},
			State: "running",
		},
	}
	rows := buildDyadRows(containers)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Actor != "" || rows[0].Critic != "" {
		t.Fatalf("expected empty actor/critic states, got %+v", rows[0])
	}
}

func TestBuildDyadStatusResult(t *testing.T) {
	actorInfo := &types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Status: "running"},
		},
	}
	criticInfo := &types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Status: "exited"},
		},
	}

	got := buildDyadStatusResult("atlas", "1234567890abcdef", actorInfo, "fedcba0987654321", criticInfo)
	if !got.Found {
		t.Fatalf("expected status to be found")
	}
	if got.Actor == nil || got.Actor.Name != "si-actor-atlas" || got.Actor.Status != "running" {
		t.Fatalf("unexpected actor payload: %+v", got.Actor)
	}
	if got.Critic == nil || got.Critic.Name != "si-critic-atlas" || got.Critic.Status != "exited" {
		t.Fatalf("unexpected critic payload: %+v", got.Critic)
	}

	notFound := buildDyadStatusResult("atlas", "", nil, "", nil)
	if notFound.Found {
		t.Fatalf("expected not-found result when both members are missing")
	}
}

func TestBuildDyadStatusResultWithNamesUsesResolvedNames(t *testing.T) {
	actorInfo := &types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Status: "running"},
		},
	}
	criticInfo := &types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Status: "exited"},
		},
	}

	got := buildDyadStatusResultWithNames(
		"atlas",
		"si-dyad-atlas-actor-rust",
		"1234567890abcdef",
		actorInfo,
		"si-dyad-atlas-critic-rust",
		"fedcba0987654321",
		criticInfo,
	)
	if got.Actor == nil || got.Actor.Name != "si-dyad-atlas-actor-rust" {
		t.Fatalf("unexpected actor payload: %+v", got.Actor)
	}
	if got.Critic == nil || got.Critic.Name != "si-dyad-atlas-critic-rust" {
		t.Fatalf("unexpected critic payload: %+v", got.Critic)
	}
}

func TestResolveDyadContainerNameUsesRustStatusWhenConfigured(t *testing.T) {
	prev := readRustDyadStatusForLookup
	t.Cleanup(func() {
		readRustDyadStatusForLookup = prev
	})
	readRustDyadStatusForLookup = func(dyad string) (*rustDyadStatus, bool, error) {
		if dyad != "atlas" {
			t.Fatalf("unexpected dyad %q", dyad)
		}
		return &rustDyadStatus{
			Dyad: "atlas",
			Actor: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-actor-rust",
			},
			Critic: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-critic-rust",
			},
		}, true, nil
	}

	actorName, err := resolveDyadContainerName("atlas", "actor")
	if err != nil {
		t.Fatalf("resolveDyadContainerName actor: %v", err)
	}
	if actorName != "si-dyad-atlas-actor-rust" {
		t.Fatalf("unexpected actor name %q", actorName)
	}
	criticName, err := resolveDyadContainerName("atlas", "critic")
	if err != nil {
		t.Fatalf("resolveDyadContainerName critic: %v", err)
	}
	if criticName != "si-dyad-atlas-critic-rust" {
		t.Fatalf("unexpected critic name %q", criticName)
	}
}

func TestResolveDyadContainerNameFallsBackToGoNaming(t *testing.T) {
	prev := readRustDyadStatusForLookup
	t.Cleanup(func() {
		readRustDyadStatusForLookup = prev
	})
	readRustDyadStatusForLookup = func(dyad string) (*rustDyadStatus, bool, error) {
		return &rustDyadStatus{Dyad: dyad}, true, nil
	}

	name, err := resolveDyadContainerName("atlas", "actor")
	if err != nil {
		t.Fatalf("resolveDyadContainerName: %v", err)
	}
	if name != shared.DyadContainerName("atlas", "actor") {
		t.Fatalf("expected Go fallback name, got %q", name)
	}
}

func TestBuildDyadPeekFallbackPlanUsesResolvedNames(t *testing.T) {
	prev := readRustDyadStatusForLookup
	t.Cleanup(func() {
		readRustDyadStatusForLookup = prev
	})
	readRustDyadStatusForLookup = func(dyad string) (*rustDyadStatus, bool, error) {
		return &rustDyadStatus{
			Dyad: dyad,
			Actor: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-actor-rust",
			},
			Critic: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-critic-rust",
			},
		}, true, nil
	}

	plan, err := buildDyadPeekFallbackPlan("atlas", "atlas", "")
	if err != nil {
		t.Fatalf("buildDyadPeekFallbackPlan: %v", err)
	}
	if plan.ActorContainerName != "si-dyad-atlas-actor-rust" || plan.CriticContainerName != "si-dyad-atlas-critic-rust" {
		t.Fatalf("unexpected container names: %+v", plan)
	}
	if plan.PeekSessionName != "si-dyad-peek-atlas" {
		t.Fatalf("unexpected peek session: %+v", plan)
	}
	if !strings.Contains(plan.ActorAttachCommand, "si-dyad-atlas-actor-rust") {
		t.Fatalf("unexpected actor attach command %q", plan.ActorAttachCommand)
	}
}

func TestResolveDyadSpawnExistingContainerNamesUsesResolvedNames(t *testing.T) {
	prev := readRustDyadStatusForLookup
	t.Cleanup(func() {
		readRustDyadStatusForLookup = prev
	})
	readRustDyadStatusForLookup = func(dyad string) (*rustDyadStatus, bool, error) {
		return &rustDyadStatus{
			Dyad: dyad,
			Actor: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-actor-rust",
			},
			Critic: &rustDyadContainerStatusRef{
				Name: "si-dyad-atlas-critic-rust",
			},
		}, true, nil
	}

	actorName, criticName, err := resolveDyadSpawnExistingContainerNames("atlas")
	if err != nil {
		t.Fatalf("resolveDyadSpawnExistingContainerNames: %v", err)
	}
	if actorName != "si-dyad-atlas-actor-rust" || criticName != "si-dyad-atlas-critic-rust" {
		t.Fatalf("unexpected resolved names actor=%q critic=%q", actorName, criticName)
	}
}

func TestCmdDyadStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"found\":true}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadStatus([]string{"--json", "alpha"})
	})
	if !strings.Contains(output, "{\"dyad\":\"alpha\",\"found\":true}") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstatus\nalpha\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[{\"dyad\":\"alpha\",\"actor\":\"running\",\"critic\":\"running\"}]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadList([]string{"--json"})
	})
	if !strings.Contains(output, "\"dyad\":\"alpha\"") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlist\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadCleanupDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed=3'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadCleanup(nil)
	})
	if !strings.Contains(output, "removed 3 stopped dyad containers") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\ncleanup" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadLogsDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"member\":\"critic\",\"tail\":50,\"logs\":\"critic logs\\n\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadLogs([]string{"--json", "--member", "critic", "--tail", "50", "alpha"})
	})
	if !strings.Contains(output, "{\"dyad\":\"alpha\",\"member\":\"critic\",\"tail\":50,\"logs\":\"critic logs\\n\"}") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlogs\nalpha\n--member\ncritic\n--tail\n50\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadStartDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'started'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadStart([]string{"alpha"})
	})
	if !strings.Contains(output, "dyad alpha started") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstart\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadStopDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'stopped'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadStop([]string{"alpha"})
	})
	if !strings.Contains(output, "dyad alpha stopped") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstop\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadRestartDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'restarted'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadRestart([]string{"alpha"})
	})
	if !strings.Contains(output, "dyad alpha restarted") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nrestart\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadRemoveDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdDyadRemove([]string{"alpha"})
	})
	if !strings.Contains(output, "dyad alpha removed") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nremove\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadExecUsesParsedInputs(t *testing.T) {
	prev := runDyadExecFn
	t.Cleanup(func() {
		runDyadExecFn = prev
	})

	var gotDyad string
	var gotMember string
	var gotCmd []string
	var gotTTY bool
	runDyadExecFn = func(dyad, member string, cmd []string, tty bool) error {
		gotDyad = dyad
		gotMember = member
		gotCmd = append([]string(nil), cmd...)
		gotTTY = tty
		return nil
	}

	_ = captureOutputForTest(t, func() {
		cmdDyadExec([]string{"--member", "critic", "--tty", "alpha", "--", "bash", "-lc", "echo hi"})
	})

	if gotDyad != "alpha" {
		t.Fatalf("unexpected dyad %q", gotDyad)
	}
	if gotMember != "critic" {
		t.Fatalf("unexpected member %q", gotMember)
	}
	if !gotTTY {
		t.Fatalf("expected tty exec")
	}
	wantCmd := []string{"bash", "-lc", "echo hi"}
	if len(gotCmd) != len(wantCmd) {
		t.Fatalf("unexpected cmd len=%d want=%d (%v)", len(gotCmd), len(wantCmd), gotCmd)
	}
	for i := range wantCmd {
		if gotCmd[i] != wantCmd[i] {
			t.Fatalf("unexpected cmd[%d]=%q want %q", i, gotCmd[i], wantCmd[i])
		}
	}
}

func TestDyadDelegatedLifecycleSmoke(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\ncase \"$2\" in\n  status)\n    printf '%s\\n' '{\"dyad\":\"alpha\",\"found\":true,\"actor\":{\"name\":\"si-dyad-alpha-actor\",\"status\":\"running\"},\"critic\":{\"name\":\"si-dyad-alpha-critic\",\"status\":\"running\"}}'\n    ;;\n  logs)\n    printf '%s\\n' '{\"dyad\":\"alpha\",\"member\":\"critic\",\"tail\":50,\"logs\":\"critic logs\\n\"}'\n    ;;\n  start)\n    printf '%s\\n' 'started'\n    ;;\n  stop)\n    printf '%s\\n' 'stopped'\n    ;;\n  restart)\n    printf '%s\\n' 'restarted'\n    ;;\n  remove)\n    printf '%s\\n' 'removed'\n    ;;\n  cleanup)\n    printf '%s\\n' 'removed=3'\n    ;;\n  *)\n    printf 'unexpected command: %s\\n' \"$2\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	statusOutput := captureOutputForTest(t, func() {
		cmdDyadStatus([]string{"--json", "alpha"})
	})
	if !strings.Contains(statusOutput, "\"dyad\":\"alpha\"") {
		t.Fatalf("unexpected status output: %q", statusOutput)
	}

	logsOutput := captureOutputForTest(t, func() {
		cmdDyadLogs([]string{"--json", "--member", "critic", "--tail", "50", "alpha"})
	})
	if !strings.Contains(logsOutput, "\"logs\":\"critic logs\\n\"") {
		t.Fatalf("unexpected logs output: %q", logsOutput)
	}

	startOutput := captureOutputForTest(t, func() {
		cmdDyadStart([]string{"alpha"})
	})
	if !strings.Contains(startOutput, "dyad alpha started") {
		t.Fatalf("unexpected start output: %q", startOutput)
	}

	stopOutput := captureOutputForTest(t, func() {
		cmdDyadStop([]string{"alpha"})
	})
	if !strings.Contains(stopOutput, "dyad alpha stopped") {
		t.Fatalf("unexpected stop output: %q", stopOutput)
	}

	restartOutput := captureOutputForTest(t, func() {
		cmdDyadRestart([]string{"alpha"})
	})
	if !strings.Contains(restartOutput, "dyad alpha restarted") {
		t.Fatalf("unexpected restart output: %q", restartOutput)
	}

	removeOutput := captureOutputForTest(t, func() {
		cmdDyadRemove([]string{"alpha"})
	})
	if !strings.Contains(removeOutput, "dyad alpha removed") {
		t.Fatalf("unexpected remove output: %q", removeOutput)
	}

	cleanupOutput := captureOutputForTest(t, func() {
		cmdDyadCleanup(nil)
	})
	if !strings.Contains(cleanupOutput, "removed 3 stopped dyad containers") {
		t.Fatalf("unexpected cleanup output: %q", cleanupOutput)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	for _, expected := range []string{
		"dyad\nstatus\nalpha\n--format\njson",
		"dyad\nlogs\nalpha\n--member\ncritic\n--tail\n50\n--format\njson",
		"dyad\nstart\nalpha",
		"dyad\nstop\nalpha",
		"dyad\nrestart\nalpha",
		"dyad\nremove\nalpha",
		"dyad\ncleanup",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected delegated args %q in %q", expected, argsText)
		}
	}
}

func TestDyadDelegatedFullLifecycleSmoke(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\ncase \"$2\" in\n  spawn-plan)\n    printf '%s\\n' '{\"name\":\"alpha\",\"role\":\"research\",\"workspace_host\":\"/tmp/workspace\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"/tmp/workspace\",\"configs_host\":\"/tmp/configs\",\"codex_volume\":\"codex-alpha-rust\",\"skills_volume\":\"skills-alpha-rust\",\"network_name\":\"si-alpha-rust\",\"forward_ports\":\"1555-1556\",\"docker_socket\":true,\"actor\":{\"name\":\"si-dyad-alpha-actor\",\"image\":\"actor-alpha-rust\"},\"critic\":{\"name\":\"si-dyad-alpha-critic\",\"image\":\"critic-alpha-rust\"}}'\n    ;;\n  spawn-start)\n    ;;\n  status)\n    printf '%s\\n' '{\"dyad\":\"alpha\",\"found\":true,\"actor\":{\"name\":\"si-dyad-alpha-actor\",\"status\":\"running\"},\"critic\":{\"name\":\"si-dyad-alpha-critic\",\"status\":\"running\"}}'\n    ;;\n  logs)\n    printf '%s\\n' '{\"dyad\":\"alpha\",\"member\":\"actor\",\"tail\":25,\"logs\":\"actor logs\\n\"}'\n    ;;\n  stop)\n    printf '%s\\n' 'stopped'\n    ;;\n  start)\n    printf '%s\\n' 'started'\n    ;;\n  remove)\n    printf '%s\\n' 'removed'\n    ;;\n  *)\n    printf 'unexpected command: %s\\n' \"$2\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prevClient := newDyadClientFn
	prevLookup := dyadContainerByNameFn
	prevEnsure := ensureDyadFn
	prevMaybeStart := maybeStartRustDyadSpawnFn
	prevEnsureHome := ensureDyadContainerSiHomeOwnershipFn
	prevSeedProfile := seedDyadProfileAuthFn
	prevHostGit := hostGitIdentityFn
	prevSeedGit := seedGitIdentityFn
	t.Cleanup(func() {
		newDyadClientFn = prevClient
		dyadContainerByNameFn = prevLookup
		ensureDyadFn = prevEnsure
		maybeStartRustDyadSpawnFn = prevMaybeStart
		ensureDyadContainerSiHomeOwnershipFn = prevEnsureHome
		seedDyadProfileAuthFn = prevSeedProfile
		hostGitIdentityFn = prevHostGit
		seedGitIdentityFn = prevSeedGit
	})

	newDyadClientFn = func() (*shared.Client, error) {
		return &shared.Client{}, nil
	}
	spawnStarted := false
	maybeStartRustDyadSpawnFn = func(request rustDyadSpawnPlanRequest) (bool, error) {
		spawnStarted = true
		return maybeStartRustDyadSpawn(request)
	}
	dyadContainerByNameFn = func(client *shared.Client, ctx context.Context, name string) (string, *types.ContainerJSON, error) {
		if !spawnStarted {
			return "", nil, nil
		}
		switch name {
		case "si-dyad-alpha-actor":
			return "actor-id", nil, nil
		case "si-dyad-alpha-critic":
			return "critic-id", nil, nil
		default:
			return "", nil, nil
		}
	}
	ensureDyadFn = func(client *shared.Client, ctx context.Context, opts shared.DyadOptions) (string, string, error) {
		t.Fatalf("did not expect Go EnsureDyad fallback")
		return "", "", nil
	}
	var ownership []string
	ensureDyadContainerSiHomeOwnershipFn = func(ctx context.Context, client *shared.Client, containerID string) {
		ownership = append(ownership, containerID)
	}
	seedDyadProfileAuthFn = func(context.Context, *shared.Client, string, codexProfile) {
		t.Fatalf("did not expect profile auth seeding without profile")
	}
	hostGitIdentityFn = func() (gitIdentity, bool) { return gitIdentity{}, false }
	seedGitIdentityFn = func(context.Context, *shared.Client, string, string, string, gitIdentity) {}

	spawnOutput := captureOutputForTest(t, func() {
		cmdDyadSpawn([]string{"alpha", "--skip-auth"})
	})
	if !strings.Contains(spawnOutput, "dyad alpha ready (role=research)") {
		t.Fatalf("unexpected spawn output: %q", spawnOutput)
	}
	if strings.Join(ownership, "\n") != "actor-id\ncritic-id" {
		t.Fatalf("unexpected ownership calls: %v", ownership)
	}

	statusOutput := captureOutputForTest(t, func() {
		cmdDyadStatus([]string{"--json", "alpha"})
	})
	if !strings.Contains(statusOutput, "\"dyad\":\"alpha\"") {
		t.Fatalf("unexpected status output: %q", statusOutput)
	}
	logsOutput := captureOutputForTest(t, func() {
		cmdDyadLogs([]string{"--json", "--member", "actor", "--tail", "25", "alpha"})
	})
	if !strings.Contains(logsOutput, "\"logs\":\"actor logs\\n\"") {
		t.Fatalf("unexpected logs output: %q", logsOutput)
	}
	stopOutput := captureOutputForTest(t, func() {
		cmdDyadStop([]string{"alpha"})
	})
	if !strings.Contains(stopOutput, "dyad alpha stopped") {
		t.Fatalf("unexpected stop output: %q", stopOutput)
	}
	startOutput := captureOutputForTest(t, func() {
		cmdDyadStart([]string{"alpha"})
	})
	if !strings.Contains(startOutput, "dyad alpha started") {
		t.Fatalf("unexpected start output: %q", startOutput)
	}
	removeOutput := captureOutputForTest(t, func() {
		cmdDyadRemove([]string{"alpha"})
	})
	if !strings.Contains(removeOutput, "dyad alpha removed") {
		t.Fatalf("unexpected remove output: %q", removeOutput)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	for _, expected := range []string{
		"dyad\nspawn-start\n--name\nalpha",
		"dyad\nstatus\nalpha\n--format\njson",
		"dyad\nlogs\nalpha\n--member\nactor\n--tail\n25\n--format\njson",
		"dyad\nstop\nalpha",
		"dyad\nstart\nalpha",
		"dyad\nremove\nalpha",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected delegated args %q in %q", expected, argsText)
		}
	}
}

func TestCmdDyadPeekDelegatesPlanToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"actor_container_name\":\"si-dyad-alpha-actor-rust\",\"critic_container_name\":\"si-dyad-alpha-critic-rust\",\"actor_session_name\":\"actor-rust\",\"critic_session_name\":\"critic-rust\",\"peek_session_name\":\"peek-rust\",\"actor_attach_command\":\"attach actor rust\",\"critic_attach_command\":\"attach critic rust\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prevLookup := lookupDyadPeekContainersFn
	prevEnsure := ensureDyadPeekTmuxAvailableFn
	prevCleanup := cleanupDyadPeekTmuxSessionsFn
	prevRun := runDyadTmuxCommandFn
	prevAttach := attachDyadTmuxSessionFn
	prevInteractive := dyadPeekIsInteractiveFn
	t.Cleanup(func() {
		lookupDyadPeekContainersFn = prevLookup
		ensureDyadPeekTmuxAvailableFn = prevEnsure
		cleanupDyadPeekTmuxSessionsFn = prevCleanup
		runDyadTmuxCommandFn = prevRun
		attachDyadTmuxSessionFn = prevAttach
		dyadPeekIsInteractiveFn = prevInteractive
	})

	lookupDyadPeekContainersFn = func(context.Context, string, string) (string, string, error) {
		return "actor-id", "critic-id", nil
	}
	ensureDyadPeekTmuxAvailableFn = func() error { return nil }
	cleanupDyadPeekTmuxSessionsFn = func(context.Context, string, time.Duration, statusOptions) {}
	var tmuxCalls [][]string
	runDyadTmuxCommandFn = func(args ...string) error {
		tmuxCalls = append(tmuxCalls, append([]string(nil), args...))
		return nil
	}
	attachDyadTmuxSessionFn = func(string) error {
		t.Fatalf("did not expect attach in detached mode")
		return nil
	}

	output := captureOutputForTest(t, func() {
		cmdDyadPeek([]string{"--detached", "alpha"})
	})
	if !strings.Contains(output, "dyad peek session ready: peek-rust") {
		t.Fatalf("unexpected output: %q", output)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\npeek-plan\nalpha\n--member\nboth\n--format\njson\n--session\nsi-dyad-peek-alpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if len(tmuxCalls) == 0 {
		t.Fatalf("expected tmux calls")
	}
	foundNewSession := false
	foundSplit := false
	for _, call := range tmuxCalls {
		text := strings.Join(call, "\n")
		if strings.Contains(text, "new-session") && strings.Contains(text, "peek-rust") && strings.Contains(text, "attach actor rust") {
			foundNewSession = true
		}
		if strings.Contains(text, "split-window") && strings.Contains(text, "attach critic rust") {
			foundSplit = true
		}
	}
	if !foundNewSession {
		t.Fatalf("expected new-session call in %v", tmuxCalls)
	}
	if !foundSplit {
		t.Fatalf("expected split-window call in %v", tmuxCalls)
	}
}

func TestCmdDyadPeekAttachedUsesRustSessionName(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"actor_container_name\":\"si-dyad-alpha-actor-rust\",\"critic_container_name\":\"si-dyad-alpha-critic-rust\",\"actor_session_name\":\"actor-rust\",\"critic_session_name\":\"critic-rust\",\"peek_session_name\":\"peek-rust\",\"actor_attach_command\":\"attach actor rust\",\"critic_attach_command\":\"attach critic rust\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prevLookup := lookupDyadPeekContainersFn
	prevEnsure := ensureDyadPeekTmuxAvailableFn
	prevCleanup := cleanupDyadPeekTmuxSessionsFn
	prevRun := runDyadTmuxCommandFn
	prevAttach := attachDyadTmuxSessionFn
	t.Cleanup(func() {
		lookupDyadPeekContainersFn = prevLookup
		ensureDyadPeekTmuxAvailableFn = prevEnsure
		cleanupDyadPeekTmuxSessionsFn = prevCleanup
		runDyadTmuxCommandFn = prevRun
		attachDyadTmuxSessionFn = prevAttach
	})

	lookupDyadPeekContainersFn = func(context.Context, string, string) (string, string, error) {
		return "actor-id", "critic-id", nil
	}
	ensureDyadPeekTmuxAvailableFn = func() error { return nil }
	cleanupDyadPeekTmuxSessionsFn = func(context.Context, string, time.Duration, statusOptions) {}
	runDyadTmuxCommandFn = func(args ...string) error { return nil }
	dyadPeekIsInteractiveFn = func() bool { return true }

	attached := ""
	attachDyadTmuxSessionFn = func(session string) error {
		attached = session
		return nil
	}

	_ = captureOutputForTest(t, func() {
		cmdDyadPeek([]string{"alpha"})
	})

	if attached != "peek-rust" {
		t.Fatalf("unexpected attached session: %q", attached)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\npeek-plan\nalpha\n--member\nboth\n--format\njson\n--session\nsi-dyad-peek-alpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdDyadSpawnUsesRustPlanBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	workspace := filepath.Join(dir, "workspace")
	configs := filepath.Join(dir, "configs")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"alpha\",\"role\":\"research\",\"workspace_host\":\"" + filepath.ToSlash(workspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(workspace) + "\",\"configs_host\":\"" + filepath.ToSlash(configs) + "\",\"codex_volume\":\"codex-rust\",\"skills_volume\":\"skills-rust\",\"network_name\":\"si-rust\",\"forward_ports\":\"1555-1556\",\"docker_socket\":true,\"actor\":{\"name\":\"si-dyad-alpha-actor\",\"image\":\"actor-rust\"},\"critic\":{\"name\":\"si-dyad-alpha-critic\",\"image\":\"critic-rust\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prevRun := runDyadSpawnFlowFn
	t.Cleanup(func() {
		runDyadSpawnFlowFn = prevRun
	})

	var gotOpts shared.DyadOptions
	var gotProfile *codexProfile
	runDyadSpawnFlowFn = func(opts shared.DyadOptions, profile *codexProfile) error {
		gotOpts = opts
		gotProfile = profile
		return nil
	}

	_ = captureOutputForTest(t, func() {
		cmdDyadSpawn([]string{
			"alpha",
			"--skip-auth",
			"--workspace", workspace,
			"--configs", configs,
			"--actor-image", "actor-old",
			"--critic-image", "critic-old",
			"--forward-ports", "1234-1235",
			"--docker-socket=false",
		})
	})

	if gotProfile != nil {
		t.Fatalf("expected nil profile with --skip-auth")
	}
	if gotOpts.Dyad != "alpha" || gotOpts.Role != "research" {
		t.Fatalf("unexpected opts: %#v", gotOpts)
	}
	if gotOpts.ActorImage != "actor-rust" || gotOpts.CriticImage != "critic-rust" {
		t.Fatalf("unexpected images: %#v", gotOpts)
	}
	if gotOpts.WorkspaceHost != filepath.ToSlash(workspace) || gotOpts.ConfigsHost != filepath.ToSlash(configs) {
		t.Fatalf("unexpected paths: %#v", gotOpts)
	}
	if gotOpts.CodexVolume != "codex-rust" || gotOpts.SkillsVolume != "skills-rust" {
		t.Fatalf("unexpected volumes: %#v", gotOpts)
	}
	if gotOpts.Network != "si-rust" || gotOpts.ForwardPorts != "1555-1556" || !gotOpts.DockerSocket {
		t.Fatalf("unexpected runtime opts: %#v", gotOpts)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	if !strings.Contains(argsText, "dyad\nspawn-plan\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
	if !strings.Contains(argsText, "--actor-image\nactor-old") || !strings.Contains(argsText, "--critic-image\ncritic-old") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
}

func TestCmdDyadSpawnWorkspaceConfigsMatrix(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	alphaWorkspace := filepath.Join(dir, "alpha-workspace")
	alphaConfigs := filepath.Join(dir, "alpha-configs")
	betaWorkspace := filepath.Join(dir, "beta-workspace")
	betaConfigs := filepath.Join(dir, "beta-configs")
	for _, path := range []string{alphaWorkspace, alphaConfigs, betaWorkspace, betaConfigs} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\nargs=\"$*\"\ncase \"$args\" in\n  *--name\\ alpha*)\n    printf '%s\\n' '{\"name\":\"alpha\",\"role\":\"research\",\"workspace_host\":\"" + filepath.ToSlash(alphaWorkspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(alphaWorkspace) + "\",\"configs_host\":\"" + filepath.ToSlash(alphaConfigs) + "\",\"codex_volume\":\"codex-alpha-rust\",\"skills_volume\":\"skills-alpha-rust\",\"network_name\":\"si-alpha-rust\",\"forward_ports\":\"1555-1556\",\"docker_socket\":true,\"actor\":{\"name\":\"si-dyad-alpha-actor\",\"image\":\"actor-alpha-rust\"},\"critic\":{\"name\":\"si-dyad-alpha-critic\",\"image\":\"critic-alpha-rust\"}}'\n    ;;\n  *--name\\ beta*)\n    printf '%s\\n' '{\"name\":\"beta\",\"role\":\"design\",\"workspace_host\":\"" + filepath.ToSlash(betaWorkspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(betaWorkspace) + "\",\"configs_host\":\"" + filepath.ToSlash(betaConfigs) + "\",\"codex_volume\":\"codex-beta-rust\",\"skills_volume\":\"skills-beta-rust\",\"network_name\":\"si-beta-rust\",\"forward_ports\":\"2555-2556\",\"docker_socket\":false,\"actor\":{\"name\":\"si-dyad-beta-actor\",\"image\":\"actor-beta-rust\"},\"critic\":{\"name\":\"si-dyad-beta-critic\",\"image\":\"critic-beta-rust\"}}'\n    ;;\n  *)\n    printf 'unexpected args: %s\\n' \"$args\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prevRun := runDyadSpawnFlowFn
	t.Cleanup(func() {
		runDyadSpawnFlowFn = prevRun
	})

	cases := []struct {
		name             string
		wantRole         string
		wantWorkspace    string
		wantConfigs      string
		wantActorImage   string
		wantCriticImage  string
		wantNetwork      string
		wantForwardPorts string
		wantDockerSocket bool
		wantCodexVolume  string
		wantSkillsVolume string
	}{
		{
			name:             "alpha",
			wantRole:         "research",
			wantWorkspace:    filepath.ToSlash(alphaWorkspace),
			wantConfigs:      filepath.ToSlash(alphaConfigs),
			wantActorImage:   "actor-alpha-rust",
			wantCriticImage:  "critic-alpha-rust",
			wantNetwork:      "si-alpha-rust",
			wantForwardPorts: "1555-1556",
			wantDockerSocket: true,
			wantCodexVolume:  "codex-alpha-rust",
			wantSkillsVolume: "skills-alpha-rust",
		},
		{
			name:             "beta",
			wantRole:         "design",
			wantWorkspace:    filepath.ToSlash(betaWorkspace),
			wantConfigs:      filepath.ToSlash(betaConfigs),
			wantActorImage:   "actor-beta-rust",
			wantCriticImage:  "critic-beta-rust",
			wantNetwork:      "si-beta-rust",
			wantForwardPorts: "2555-2556",
			wantDockerSocket: false,
			wantCodexVolume:  "codex-beta-rust",
			wantSkillsVolume: "skills-beta-rust",
		},
	}

	var gotOpts shared.DyadOptions
	runDyadSpawnFlowFn = func(opts shared.DyadOptions, profile *codexProfile) error {
		gotOpts = opts
		if profile != nil {
			t.Fatalf("expected nil profile with --skip-auth")
		}
		return nil
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotOpts = shared.DyadOptions{}
			_ = captureOutputForTest(t, func() {
				cmdDyadSpawn([]string{tc.name, "--skip-auth"})
			})
			if gotOpts.Dyad != tc.name || gotOpts.Role != tc.wantRole {
				t.Fatalf("unexpected opts: %#v", gotOpts)
			}
			if gotOpts.WorkspaceHost != tc.wantWorkspace || gotOpts.ConfigsHost != tc.wantConfigs {
				t.Fatalf("unexpected paths: %#v", gotOpts)
			}
			if gotOpts.ActorImage != tc.wantActorImage || gotOpts.CriticImage != tc.wantCriticImage {
				t.Fatalf("unexpected images: %#v", gotOpts)
			}
			if gotOpts.Network != tc.wantNetwork || gotOpts.ForwardPorts != tc.wantForwardPorts || gotOpts.DockerSocket != tc.wantDockerSocket {
				t.Fatalf("unexpected runtime opts: %#v", gotOpts)
			}
			if gotOpts.CodexVolume != tc.wantCodexVolume || gotOpts.SkillsVolume != tc.wantSkillsVolume {
				t.Fatalf("unexpected volumes: %#v", gotOpts)
			}
		})
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	for _, expected := range []string{
		"dyad\nspawn-plan\n--name\nalpha",
		"dyad\nspawn-plan\n--name\nbeta",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("unexpected Rust CLI args: %q", argsText)
		}
	}
}

func TestCmdDyadRecreateUsesDelegatedRemoveAndSpawnFlow(t *testing.T) {
	prevRemove := removeDyadWithCompatibilityFn
	prevSpawn := runDyadSpawnCmdFn
	prevClient := newDyadClientFn
	t.Cleanup(func() {
		removeDyadWithCompatibilityFn = prevRemove
		runDyadSpawnCmdFn = prevSpawn
		newDyadClientFn = prevClient
	})

	removeCalled := false
	removeDyadWithCompatibilityFn = func(ctx context.Context, client *shared.Client, name string) (string, bool, error) {
		removeCalled = true
		if client == nil {
			t.Fatalf("expected shared client")
		}
		if name != "alpha" {
			t.Fatalf("unexpected recreate target: %q", name)
		}
		return "dyad alpha removed\n", true, nil
	}

	var spawnedArgs []string
	runDyadSpawnCmdFn = func(args []string) {
		spawnedArgs = append([]string(nil), args...)
	}
	newDyadClientFn = func() (*shared.Client, error) {
		return &shared.Client{}, nil
	}

	output := captureOutputForTest(t, func() {
		cmdDyadRecreate([]string{"alpha", "--skip-auth"})
	})

	if !removeCalled {
		t.Fatalf("expected delegated remove flow")
	}
	if !strings.Contains(output, "dyad alpha removed") {
		t.Fatalf("unexpected output: %q", output)
	}
	if strings.Join(spawnedArgs, "\n") != "alpha\n--skip-auth" {
		t.Fatalf("unexpected spawn args: %q", strings.Join(spawnedArgs, "\n"))
	}
}

func TestCmdDyadRemoveAllUsesBatchFlow(t *testing.T) {
	prev := runDyadRemoveAllFn
	t.Cleanup(func() {
		runDyadRemoveAllFn = prev
	})

	called := false
	runDyadRemoveAllFn = func(ctx context.Context) error {
		called = true
		return nil
	}

	_ = captureOutputForTest(t, func() {
		cmdDyadRemove([]string{"--all"})
	})

	if !called {
		t.Fatalf("expected batch remove flow")
	}
}

func TestShortContainerID(t *testing.T) {
	if got := shortContainerID("1234567890ab"); got != "1234567890ab" {
		t.Fatalf("unexpected unchanged id: %q", got)
	}
	if got := shortContainerID("1234567890abcdef"); got != "1234567890ab" {
		t.Fatalf("unexpected short id: %q", got)
	}
}

func TestDefaultEffort(t *testing.T) {
	actor, critic := defaultEffort("infra")
	if actor != "xhigh" || critic != "xhigh" {
		t.Fatalf("unexpected infra defaults: actor=%q critic=%q", actor, critic)
	}
	actor, critic = defaultEffort("web")
	if actor != "medium" || critic != "high" {
		t.Fatalf("unexpected web defaults: actor=%q critic=%q", actor, critic)
	}
	actor, critic = defaultEffort("webdev")
	if actor != "medium" || critic != "high" {
		t.Fatalf("unexpected webdev defaults: actor=%q critic=%q", actor, critic)
	}
	actor, critic = defaultEffort("research")
	if actor != "high" || critic != "high" {
		t.Fatalf("unexpected research defaults: actor=%q critic=%q", actor, critic)
	}
	actor, critic = defaultEffort("generic")
	if actor != "medium" || critic != "medium" {
		t.Fatalf("unexpected generic defaults: actor=%q critic=%q", actor, critic)
	}
}

func TestSplitDyadLogsNameAndFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantDyad   string
		wantRemain []string
	}{
		{
			name:       "name-first",
			args:       []string{"figi", "--member", "critic", "--tail", "5"},
			wantDyad:   "figi",
			wantRemain: []string{"--member", "critic", "--tail", "5"},
		},
		{
			name:       "flags-first",
			args:       []string{"--member", "actor", "--tail", "10", "figi"},
			wantDyad:   "figi",
			wantRemain: []string{"--member", "actor", "--tail", "10"},
		},
		{
			name:       "json-flag-before-name",
			args:       []string{"--json", "--member", "critic", "figi"},
			wantDyad:   "figi",
			wantRemain: []string{"--json", "--member", "critic"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotDyad, gotRemain := splitDyadLogsNameAndFlags(tc.args)
			if gotDyad != tc.wantDyad {
				t.Fatalf("unexpected dyad name: got %q want %q", gotDyad, tc.wantDyad)
			}
			if len(gotRemain) != len(tc.wantRemain) {
				t.Fatalf("unexpected remaining args len=%d want=%d (%v)", len(gotRemain), len(tc.wantRemain), gotRemain)
			}
			for i := range gotRemain {
				if gotRemain[i] != tc.wantRemain[i] {
					t.Fatalf("unexpected remaining arg[%d]: got %q want %q", i, gotRemain[i], tc.wantRemain[i])
				}
			}
		})
	}
}
