package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cmdImage(args []string) {
	if len(args) == 0 {
		printUsage("usage: si image <build>")
		return
	}
	switch args[0] {
	case "build":
		cmdImageBuild(args[1:])
	default:
		printUnknown("image", args[0])
	}
}

type imageBuildSpec struct {
	tag        string
	contextDir string
	dockerfile string
	buildArgs  []string
}

func cmdImageBuild(args []string) {
	if len(args) != 0 {
		printUsage("usage: si image build")
		return
	}
	root := mustRepoRoot()
	specs := []imageBuildSpec{
		{tag: "aureuma/si:local", contextDir: root, dockerfile: filepath.Join(root, "tools/si-image/Dockerfile")},
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
	args := []string{"build", "-t", spec.tag}
	if spec.dockerfile != "" {
		args = append(args, "-f", spec.dockerfile)
	}
	for _, arg := range spec.buildArgs {
		args = append(args, "--build-arg", arg)
	}
	args = append(args, spec.contextDir)
	infof("docker %s", strings.Join(args, " "))
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
