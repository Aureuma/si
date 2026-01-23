package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cmdImages(args []string) {
	if len(args) == 0 {
		printUsage("usage: si images <build>")
		return
	}
	switch args[0] {
	case "build":
		cmdImagesBuild(args[1:])
	default:
		printUnknown("images", args[0])
	}
}

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

func cmdImagesBuild(args []string) {
	root := mustRepoRoot()
	specs := []imageBuildSpec{
		{tag: "silexa/si-codex:local", contextDir: filepath.Join(root, "tools/si-codex-image")},
		{tag: "silexa/telegram-bot:local", contextDir: filepath.Join(root, "agents/telegram-bot")},
		{tag: "silexa/resource-broker:local", contextDir: filepath.Join(root, "agents/resource-broker")},
		{tag: "silexa/infra-broker:local", contextDir: filepath.Join(root, "agents/infra-broker")},
		{tag: "silexa/manager:local", contextDir: filepath.Join(root, "agents/manager")},
		{tag: "silexa/codex-monitor:local", contextDir: root, dockerfile: filepath.Join(root, "agents/codex-monitor/Dockerfile")},
		{tag: "silexa/router:local", contextDir: filepath.Join(root, "agents/router")},
		{tag: "silexa/actor:local", contextDir: root, dockerfile: filepath.Join(root, "agents/actor/Dockerfile")},
		{tag: "silexa/critic:local", contextDir: root, dockerfile: filepath.Join(root, "agents/critic/Dockerfile")},
		{tag: "silexa/coder-agent:local", contextDir: filepath.Join(root, "agents/coder")},
		{tag: "silexa/mcp-gateway:local", contextDir: filepath.Join(root, "tools/mcp-gateway")},
		{tag: "silexa/credentials-mcp:local", contextDir: filepath.Join(root, "tools/credentials-mcp")},
		{tag: "silexa/program-manager:local", contextDir: filepath.Join(root, "agents/program-manager")},
	}
	for _, spec := range specs {
		if err := runDockerBuild(spec); err != nil {
			fatal(err)
		}
	}
	_ = args
}

func cmdImageBuild(args []string) {
	fs := flag.NewFlagSet("image build", flag.ExitOnError)
	tag := fs.String("t", "", "image tag")
	tagLong := fs.String("tag", "", "image tag")
	dockerfile := fs.String("f", "", "dockerfile")
	dockerfileLong := fs.String("file", "", "dockerfile")
	buildArgs := multiFlag{}
	fs.Var(&buildArgs, "build-arg", "build argument (repeatable)")
	fs.Parse(args)
	if *tag == "" {
		*tag = *tagLong
	}
	if *dockerfile == "" {
		*dockerfile = *dockerfileLong
	}
	if fs.NArg() < 1 || *tag == "" {
		printUsage("usage: si image build -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>")
		return
	}
	contextDir := fs.Arg(0)
	spec := imageBuildSpec{
		tag:        *tag,
		contextDir: contextDir,
		dockerfile: *dockerfile,
		buildArgs:  buildArgs,
	}
	if err := runDockerBuild(spec); err != nil {
		fatal(err)
	}
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
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
