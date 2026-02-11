package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type imageBuildSpec struct {
	tag        string
	contextDir string
	dockerfile string
	buildArgs  []string
	secrets    []string
}

var dockerBuildxAvailableFn = dockerBuildxAvailable
var runDockerBuildCommandFn = runDockerBuildCommand

func cmdBuildImage(args []string) {
	if len(args) != 0 {
		printUsage("usage: si build image")
		return
	}
	root := mustRepoRoot()
	secrets := hostCodexConfigBuildSecrets()
	specs := []imageBuildSpec{
		{tag: "aureuma/si:local", contextDir: root, dockerfile: filepath.Join(root, "tools/si-image/Dockerfile"), secrets: secrets},
	}
	for _, spec := range specs {
		if err := runDockerBuild(spec); err != nil {
			fatal(err)
		}
	}
	_ = args
}

func runDockerBuild(spec imageBuildSpec) error {
	if spec.tag == "" || spec.contextDir == "" {
		return fmt.Errorf("image tag and context required")
	}
	enableBuildKit := false
	buildxOK, buildxErr := dockerBuildxAvailableFn()
	if buildxErr != nil {
		warnf("docker buildx detection failed; using legacy docker build path: %v", buildxErr)
	} else if buildxOK {
		enableBuildKit = true
	} else {
		warnf("docker buildx is not available; using legacy docker build path")
	}
	secrets := spec.secrets
	if len(secrets) > 0 && !enableBuildKit {
		warnf("building without host build secrets because BuildKit/buildx is unavailable")
		secrets = nil
	}
	args := dockerBuildArgs(spec, secrets)
	infof("docker %s", redactedDockerBuildArgs(args))
	return runDockerBuildCommandFn(args, enableBuildKit)
}

func dockerBuildArgs(spec imageBuildSpec, secrets []string) []string {
	args := []string{"build", "-t", spec.tag}
	if spec.dockerfile != "" {
		args = append(args, "-f", spec.dockerfile)
	}
	for _, arg := range spec.buildArgs {
		args = append(args, "--build-arg", arg)
	}
	for _, secret := range secrets {
		args = append(args, "--secret", secret)
	}
	args = append(args, spec.contextDir)
	return args
}

func runDockerBuildCommand(args []string, enableBuildKit bool) error {
	if len(args) == 0 {
		return fmt.Errorf("docker build args required")
	}
	buildKitSetting := "DOCKER_BUILDKIT=0"
	if enableBuildKit {
		buildKitSetting = "DOCKER_BUILDKIT=1"
	}
	cmd := dockerCommandWithEnv([]string{buildKitSetting}, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func dockerBuildxAvailable() (bool, error) {
	cmd := dockerCommand("buildx", "version")
	cmd.Stdin = nil
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func hostCodexConfigBuildSecrets() []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	path := filepath.Join(home, ".codex", "config.toml")
	data, err := readLocalFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			warnf("host codex config secret skipped: %v", err)
		}
		return nil
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil
	}
	infof("including host codex config.toml in image build via build secret")
	return []string{"id=si_host_codex_config,src=" + path}
}

func redactedDockerBuildArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		part := strings.TrimSpace(args[i])
		if part == "--build-arg" && i+1 < len(args) {
			key := strings.TrimSpace(args[i+1])
			if idx := strings.Index(key, "="); idx != -1 {
				key = key[:idx]
			}
			parts = append(parts, "--build-arg", key+"=***")
			i++
			continue
		}
		if part == "--secret" && i+1 < len(args) {
			parts = append(parts, "--secret", "id=***,src=***")
			i++
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
