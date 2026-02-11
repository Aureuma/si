package main

import (
	"testing"
)

func TestDockerBuildArgsIncludesSecretWhenProvided(t *testing.T) {
	spec := imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	}
	args := dockerBuildArgs(spec, spec.secrets)
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--secret" && args[i+1] == "id=si_host_codex_config,src=/tmp/config.toml" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected secret args to be present, got %v", args)
	}
}

func TestRunDockerBuildSkipsSecretsWhenBuildxMissing(t *testing.T) {
	origCheck := dockerBuildxAvailableFn
	origRun := runDockerBuildCommandFn
	defer func() {
		dockerBuildxAvailableFn = origCheck
		runDockerBuildCommandFn = origRun
	}()

	dockerBuildxAvailableFn = func() (bool, error) { return false, nil }
	var captured []string
	runDockerBuildCommandFn = func(args []string) error {
		captured = append([]string(nil), args...)
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	})
	if err != nil {
		t.Fatalf("runDockerBuild returned error: %v", err)
	}
	for _, arg := range captured {
		if arg == "--secret" {
			t.Fatalf("did not expect --secret when buildx missing, args=%v", captured)
		}
	}
}

func TestRunDockerBuildKeepsSecretsWhenBuildxAvailable(t *testing.T) {
	origCheck := dockerBuildxAvailableFn
	origRun := runDockerBuildCommandFn
	defer func() {
		dockerBuildxAvailableFn = origCheck
		runDockerBuildCommandFn = origRun
	}()

	dockerBuildxAvailableFn = func() (bool, error) { return true, nil }
	var captured []string
	runDockerBuildCommandFn = func(args []string) error {
		captured = append([]string(nil), args...)
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	})
	if err != nil {
		t.Fatalf("runDockerBuild returned error: %v", err)
	}
	found := false
	for i := 0; i < len(captured)-1; i++ {
		if captured[i] == "--secret" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --secret when buildx is available, args=%v", captured)
	}
}

func TestRunDockerBuildSkipsSecretsWhenBuildxCheckErrors(t *testing.T) {
	origCheck := dockerBuildxAvailableFn
	origRun := runDockerBuildCommandFn
	defer func() {
		dockerBuildxAvailableFn = origCheck
		runDockerBuildCommandFn = origRun
	}()

	dockerBuildxAvailableFn = func() (bool, error) { return false, assertErr("probe failed") }
	var captured []string
	runDockerBuildCommandFn = func(args []string) error {
		captured = append([]string(nil), args...)
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	})
	if err != nil {
		t.Fatalf("runDockerBuild returned error: %v", err)
	}
	for _, arg := range captured {
		if arg == "--secret" {
			t.Fatalf("did not expect --secret when buildx probe errors, args=%v", captured)
		}
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
