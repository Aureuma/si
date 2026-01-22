package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func cmdDocker(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si docker <args...>")
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
	return cmd.Run()
}
