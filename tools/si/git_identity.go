package main

import (
	"context"
	"os/exec"
	"strings"

	shared "si/agents/shared/docker"
)

type gitIdentity struct {
	Name  string
	Email string
}

func hostGitIdentity() (gitIdentity, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return gitIdentity{}, false
	}
	name, err := gitConfigGlobalGet("user.name")
	if err != nil {
		warnf("git config read failed: %v", err)
	}
	email, err := gitConfigGlobalGet("user.email")
	if err != nil {
		warnf("git config read failed: %v", err)
	}
	identity := gitIdentity{
		Name:  strings.TrimSpace(name),
		Email: strings.TrimSpace(email),
	}
	if identity.Name == "" && identity.Email == "" {
		return gitIdentity{}, false
	}
	return identity, true
}

func gitConfigGlobalGet(key string) (string, error) {
	cmd := exec.Command("git", "config", "--global", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return "", nil
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func seedGitIdentity(ctx context.Context, client *shared.Client, containerID, user, home string, identity gitIdentity) {
	if strings.TrimSpace(containerID) == "" {
		return
	}
	if identity.Name == "" && identity.Email == "" {
		return
	}
	opts := shared.ExecOptions{User: strings.TrimSpace(user)}
	if strings.TrimSpace(home) != "" {
		opts.Env = []string{"HOME=" + strings.TrimSpace(home)}
	}
	if identity.Name != "" {
		if err := client.Exec(ctx, containerID, []string{"git", "config", "--global", "user.name", identity.Name}, opts, nil, nil, nil); err != nil {
			warnf("git user.name set failed: %v", err)
		}
	}
	if identity.Email != "" {
		if err := client.Exec(ctx, containerID, []string{"git", "config", "--global", "user.email", identity.Email}, opts, nil, nil, nil); err != nil {
			warnf("git user.email set failed: %v", err)
		}
	}
}
