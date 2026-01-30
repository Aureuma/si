package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

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

func execDockerCLIWithOutput(args []string, handler func([]byte)) error {
	if len(args) == 0 {
		return errors.New("docker args required")
	}
	if handler == nil {
		return execDockerCLI(args...)
	}
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if os.Getenv("DOCKER_HOST") == "" {
		if host, ok := shared.AutoDockerHost(); ok {
			cmd.Env = append(os.Environ(), "DOCKER_HOST="+host)
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	stream := func(r io.Reader, w io.Writer) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				handler(chunk)
				_, _ = w.Write(chunk)
			}
			if readErr != nil {
				return
			}
		}
	}
	wg.Add(2)
	go stream(stdout, os.Stdout)
	go stream(stderr, os.Stderr)
	err = cmd.Wait()
	wg.Wait()
	return err
}
