package main

import (
	"errors"
	"os"
	"os/exec"

	shared "silexa/agents/shared/docker"
)

func cmdDocker(args []string) {
	if len(args) == 0 {
		printUsage("usage: si docker <args...>")
		return
	}
	if err := execDockerCLI(args...); err != nil {
		fatal(err)
	}
}

func execDockerCLI(args ...string) error {
	if len(args) == 0 {
		return errors.New("docker args required")
	}
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if os.Getenv("DOCKER_HOST") == "" {
		if host, ok := shared.AutoDockerHost(); ok {
			cmd.Env = append(os.Environ(), "DOCKER_HOST="+host)
		}
	}
	return cmd.Run()
}
