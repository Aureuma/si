package main

import (
	"errors"
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
	gotBuildKit := true
	runDockerBuildCommandFn = func(args []string, enableBuildKit bool) error {
		captured = append([]string(nil), args...)
		gotBuildKit = enableBuildKit
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
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
	if gotBuildKit {
		t.Fatalf("expected buildkit to be disabled when buildx is missing")
	}
	want := "/workspace/tools/si-image/Dockerfile.legacy"
	if !argsContain(captured, "-f", want) {
		t.Fatalf("expected legacy dockerfile when buildx is missing, args=%v", captured)
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
	gotBuildKit := false
	runDockerBuildCommandFn = func(args []string, enableBuildKit bool) error {
		captured = append([]string(nil), args...)
		gotBuildKit = enableBuildKit
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
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
	if !gotBuildKit {
		t.Fatalf("expected buildkit to be enabled when buildx is available")
	}
	want := "/workspace/tools/si-image/Dockerfile"
	if !argsContain(captured, "-f", want) {
		t.Fatalf("expected default dockerfile when buildx is available, args=%v", captured)
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
	gotBuildKit := true
	runDockerBuildCommandFn = func(args []string, enableBuildKit bool) error {
		captured = append([]string(nil), args...)
		gotBuildKit = enableBuildKit
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
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
	if gotBuildKit {
		t.Fatalf("expected buildkit to be disabled when buildx probe errors")
	}
	want := "/workspace/tools/si-image/Dockerfile.legacy"
	if !argsContain(captured, "-f", want) {
		t.Fatalf("expected legacy dockerfile when buildx probe errors, args=%v", captured)
	}
}

func TestRunDockerBuildRetriesLegacyOnRecoverableBuildKitError(t *testing.T) {
	origCheck := dockerBuildxAvailableFn
	origRun := runDockerBuildCommandFn
	defer func() {
		dockerBuildxAvailableFn = origCheck
		runDockerBuildCommandFn = origRun
	}()

	dockerBuildxAvailableFn = func() (bool, error) { return true, nil }
	var calls []struct {
		args           []string
		enableBuildKit bool
	}
	runDockerBuildCommandFn = func(args []string, enableBuildKit bool) error {
		calls = append(calls, struct {
			args           []string
			enableBuildKit bool
		}{args: append([]string(nil), args...), enableBuildKit: enableBuildKit})
		if len(calls) == 1 {
			return &dockerBuildCommandError{
				err:    assertErr("build failed"),
				output: "ERROR: failed to solve: failed to prepare extraction snapshot",
			}
		}
		return nil
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	})
	if err != nil {
		t.Fatalf("runDockerBuild returned error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 docker build attempts, got %d", len(calls))
	}
	if !calls[0].enableBuildKit {
		t.Fatalf("expected first attempt to use BuildKit")
	}
	if !argsContain(calls[0].args, "-f", "/workspace/tools/si-image/Dockerfile") {
		t.Fatalf("expected first attempt to use buildkit dockerfile, args=%v", calls[0].args)
	}
	if !argsContainKey(calls[0].args, "--secret") {
		t.Fatalf("expected first attempt to include --secret, args=%v", calls[0].args)
	}
	if calls[1].enableBuildKit {
		t.Fatalf("expected second attempt to disable BuildKit")
	}
	if !argsContain(calls[1].args, "-f", "/workspace/tools/si-image/Dockerfile.legacy") {
		t.Fatalf("expected second attempt to use legacy dockerfile, args=%v", calls[1].args)
	}
	if argsContainKey(calls[1].args, "--secret") {
		t.Fatalf("did not expect second attempt to include --secret, args=%v", calls[1].args)
	}
}

func TestRunDockerBuildDoesNotRetryLegacyOnNonRecoverableBuildKitError(t *testing.T) {
	origCheck := dockerBuildxAvailableFn
	origRun := runDockerBuildCommandFn
	defer func() {
		dockerBuildxAvailableFn = origCheck
		runDockerBuildCommandFn = origRun
	}()

	dockerBuildxAvailableFn = func() (bool, error) { return true, nil }
	attempts := 0
	runDockerBuildCommandFn = func(args []string, enableBuildKit bool) error {
		attempts++
		return &dockerBuildCommandError{
			err:    assertErr("build failed"),
			output: "ERROR: failed to solve: process \"/bin/sh -c exit 42\" did not complete successfully",
		}
	}

	err := runDockerBuild(imageBuildSpec{
		tag:        "aureuma/si:local",
		contextDir: "/workspace",
		dockerfile: "/workspace/tools/si-image/Dockerfile",
		secrets:    []string{"id=si_host_codex_config,src=/tmp/config.toml"},
	})
	if err == nil {
		t.Fatalf("expected runDockerBuild to return error")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt for non-recoverable error, got %d", attempts)
	}
}

func TestShouldRetryLegacyBuild(t *testing.T) {
	recoverable := &dockerBuildCommandError{
		err:    errors.New("build failed"),
		output: "failed to prepare extraction snapshot",
	}
	if !shouldRetryLegacyBuild(recoverable) {
		t.Fatalf("expected recoverable error to trigger legacy retry")
	}
	other := &dockerBuildCommandError{
		err:    errors.New("build failed"),
		output: "failed to solve: go test failed",
	}
	if shouldRetryLegacyBuild(other) {
		t.Fatalf("did not expect non-recoverable build failure to trigger legacy retry")
	}
	if shouldRetryLegacyBuild(assertErr("plain error")) {
		t.Fatalf("did not expect non-build error type to trigger legacy retry")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func argsContain(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func argsContainKey(args []string, key string) bool {
	for _, arg := range args {
		if arg == key {
			return true
		}
	}
	return false
}
