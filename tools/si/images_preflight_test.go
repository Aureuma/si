package main

import (
	"reflect"
	"testing"
)

func TestCmdBuildImageRunsPreflightBeforeBuild(t *testing.T) {
	origPreflight := runImageBuildPreflightFn
	origBuild := runDockerBuildSpecFn
	defer func() {
		runImageBuildPreflightFn = origPreflight
		runDockerBuildSpecFn = origBuild
	}()

	var calls []string
	runImageBuildPreflightFn = func(repoRoot string) error {
		calls = append(calls, "preflight")
		return nil
	}
	runDockerBuildSpecFn = func(spec imageBuildSpec) error {
		calls = append(calls, "build")
		if spec.tag != "aureuma/si:local" {
			t.Fatalf("unexpected image tag: %q", spec.tag)
		}
		return nil
	}

	cmdBuildImage(nil)

	want := []string{"preflight", "build"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected call order: got=%v want=%v", calls, want)
	}
}

func TestCmdBuildImageSkipPreflightFlag(t *testing.T) {
	origPreflight := runImageBuildPreflightFn
	origBuild := runDockerBuildSpecFn
	defer func() {
		runImageBuildPreflightFn = origPreflight
		runDockerBuildSpecFn = origBuild
	}()

	var calls []string
	runImageBuildPreflightFn = func(repoRoot string) error {
		calls = append(calls, "preflight")
		return nil
	}
	runDockerBuildSpecFn = func(spec imageBuildSpec) error {
		calls = append(calls, "build")
		return nil
	}

	cmdBuildImage([]string{"--skip-preflight"})

	want := []string{"build"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected call order: got=%v want=%v", calls, want)
	}
}

func TestCmdBuildImageSkipPreflightEnv(t *testing.T) {
	origPreflight := runImageBuildPreflightFn
	origBuild := runDockerBuildSpecFn
	defer func() {
		runImageBuildPreflightFn = origPreflight
		runDockerBuildSpecFn = origBuild
	}()

	t.Setenv("SI_IMAGE_BUILD_SKIP_PREFLIGHT", "1")

	var calls []string
	runImageBuildPreflightFn = func(repoRoot string) error {
		calls = append(calls, "preflight")
		return nil
	}
	runDockerBuildSpecFn = func(spec imageBuildSpec) error {
		calls = append(calls, "build")
		return nil
	}

	cmdBuildImage(nil)

	want := []string{"build"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected call order: got=%v want=%v", calls, want)
	}
}

func TestCmdBuildImagePreflightOnly(t *testing.T) {
	origPreflight := runImageBuildPreflightFn
	origBuild := runDockerBuildSpecFn
	defer func() {
		runImageBuildPreflightFn = origPreflight
		runDockerBuildSpecFn = origBuild
	}()

	var calls []string
	runImageBuildPreflightFn = func(repoRoot string) error {
		calls = append(calls, "preflight")
		return nil
	}
	runDockerBuildSpecFn = func(spec imageBuildSpec) error {
		calls = append(calls, "build")
		return nil
	}

	cmdBuildImage([]string{"--preflight-only"})

	want := []string{"preflight"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected call order: got=%v want=%v", calls, want)
	}
}

func TestCmdBuildImageUsageOnUnexpectedArgs(t *testing.T) {
	origPreflight := runImageBuildPreflightFn
	origBuild := runDockerBuildSpecFn
	defer func() {
		runImageBuildPreflightFn = origPreflight
		runDockerBuildSpecFn = origBuild
	}()

	called := false
	runImageBuildPreflightFn = func(repoRoot string) error {
		called = true
		return nil
	}
	runDockerBuildSpecFn = func(spec imageBuildSpec) error {
		called = true
		return nil
	}

	cmdBuildImage([]string{"unexpected"})

	if called {
		t.Fatalf("expected no preflight/build calls on invalid args")
	}
}

