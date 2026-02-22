package main

import (
	"bytes"
	"flag"
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
var runImageBuildPreflightFn = runImageBuildPreflight
var runDockerBuildSpecFn = runDockerBuild

func cmdBuildImage(args []string) {
	fs := flag.NewFlagSet("build image", flag.ExitOnError)
	skipPreflight := fs.Bool("skip-preflight", envIsTrue("SI_IMAGE_BUILD_SKIP_PREFLIGHT"), "skip codex compatibility preflight tests before build")
	preflightOnly := fs.Bool("preflight-only", false, "run codex compatibility preflight tests and exit")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 0 {
		printUsage("usage: si build image [--skip-preflight] [--preflight-only]")
		return
	}
	root := mustRepoRoot()
	if !*skipPreflight {
		if err := runImageBuildPreflightFn(root); err != nil {
			fatal(fmt.Errorf("codex image preflight failed: %w", err))
		}
	}
	if *preflightOnly {
		successf("codex image preflight passed")
		return
	}
	secrets := hostCodexConfigBuildSecrets()
	specs := []imageBuildSpec{
		{tag: "aureuma/si:local", contextDir: root, dockerfile: filepath.Join(root, "tools/si-image/Dockerfile"), secrets: secrets},
	}
	for _, spec := range specs {
		if err := runDockerBuildSpecFn(spec); err != nil {
			fatal(err)
		}
	}
	_ = args
}

func runDockerBuild(spec imageBuildSpec) error {
	if spec.tag == "" || spec.contextDir == "" {
		return fmt.Errorf("image tag and context required")
	}
	enableBuildx := false
	buildxOK, buildxErr := dockerBuildxAvailableFn()
	if buildxErr != nil {
		warnf("docker buildx detection failed; using legacy docker build path: %v", buildxErr)
	} else if buildxOK {
		enableBuildx = true
	} else {
		warnf("docker buildx is not available; using legacy docker build path")
	}
	secrets := spec.secrets
	if enableBuildx && len(secrets) > 0 {
		for _, s := range secrets {
			if strings.Contains(s, "id=si_host_codex_config") {
				infof("including host codex config.toml in image build via build secret")
				break
			}
		}
	}
	if len(secrets) > 0 && !enableBuildx {
		warnf("building without host build secrets because BuildKit/buildx is unavailable")
		secrets = nil
	}
	spec.dockerfile = dockerfileForBuildMode(spec.dockerfile, enableBuildx)
	args := dockerBuildArgs(spec, secrets, enableBuildx)
	infof("docker %s", redactedDockerBuildArgs(args))
	return runDockerBuildCommandFn(args, enableBuildx)
}

func dockerfileForBuildMode(dockerfile string, enableBuildKit bool) string {
	if enableBuildKit || strings.TrimSpace(dockerfile) == "" {
		return dockerfile
	}
	return dockerfile + ".legacy"
}

func dockerBuildArgs(spec imageBuildSpec, secrets []string, enableBuildx bool) []string {
	// In buildx mode, force a local image result (tag available in `docker images`)
	// by using --load. Without it, some buildx drivers won't import into the local
	// image store.
	args := []string{"build"}
	if enableBuildx {
		args = []string{"buildx", "build", "--load"}
	}
	args = append(args, "-t", spec.tag)
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
	env := []string{"DOCKER_BUILDKIT=0"}
	if enableBuildKit {
		// Local image builds do not require default provenance attestations.
		env = []string{"DOCKER_BUILDKIT=1", "BUILDX_NO_DEFAULT_ATTESTATIONS=1"}
	}
	cmd := dockerCommandWithEnv(env, args...)
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
		trimmed := strings.TrimSpace(output.String())
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "not a docker command") ||
			strings.Contains(lower, "unknown command \"buildx\"") ||
			strings.Contains(lower, "unknown command: docker buildx") ||
			strings.Contains(lower, "buildx: command not found") {
			return false, nil
		}
		if trimmed == "" {
			return false, err
		}
		return false, fmt.Errorf("docker buildx probe failed: %s", trimmed)
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
