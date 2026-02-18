package main

import (
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
	if value, ok := dyadProfileArg([]string{"--profile", "berylla"}); !ok || value != "berylla" {
		t.Fatalf("expected --profile value, got ok=%v value=%q", ok, value)
	}
	if value, ok := dyadProfileArg([]string{"--profile=einsteina"}); !ok || value != "einsteina" {
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
		{name: "missing", args: []string{"--profile", "berylla"}, want: false},
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
