package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
