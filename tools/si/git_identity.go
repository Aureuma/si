package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

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
	user = strings.TrimSpace(user)
	home = strings.TrimSpace(home)
	opts := shared.ExecOptions{User: user}
	if home != "" {
		opts.Env = []string{"HOME=" + home}
	}
	if !containerHasGit(ctx, client, containerID, opts) {
		warnf("git identity seed skipped: git not available for %s", opts.User)
		return
	}
	if home != "" && !waitForGitIdentityWritable(ctx, client, containerID, opts, user, home) {
		if homeWritable(ctx, client, containerID, opts) {
			warnf("git identity seed skipped: gitconfig not writable for %s (%s)", opts.User, home)
		} else {
			warnf("git identity seed skipped: home not writable for %s (%s)", opts.User, home)
		}
		return
	}
	if identity.Name != "" {
		if err := execGitConfig(ctx, client, containerID, opts, "user.name", identity.Name); err != nil {
			warnf("git user.name set failed: %v", err)
		}
	}
	if identity.Email != "" {
		if err := execGitConfig(ctx, client, containerID, opts, "user.email", identity.Email); err != nil {
			warnf("git user.email set failed: %v", err)
		}
	}
}

func homeWritable(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions) bool {
	if err := client.Exec(ctx, containerID, []string{"sh", "-lc", "test -d \"$HOME\" && test -w \"$HOME\""}, opts, nil, nil, nil); err != nil {
		return false
	}
	return true
}

func waitForWritableHome(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions) bool {
	const attempts = 10
	const delay = 200 * time.Millisecond
	for i := 0; i < attempts; i++ {
		if homeWritable(ctx, client, containerID, opts) {
			return true
		}
		time.Sleep(delay)
	}
	return false
}

func waitForGitIdentityWritable(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions, user, home string) bool {
	const attempts = 12
	const delay = 250 * time.Millisecond
	for i := 0; i < attempts; i++ {
		if gitConfigWritable(ctx, client, containerID, opts) {
			return true
		}
		if ensureGitConfigWritable(ctx, client, containerID, opts, user, home) {
			return true
		}
		time.Sleep(delay)
	}
	return false
}

func containerHasGit(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions) bool {
	if err := client.Exec(ctx, containerID, []string{"git", "--version"}, opts, nil, nil, nil); err != nil {
		return false
	}
	return true
}

func ensureGitConfigWritable(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions, user, home string) bool {
	if strings.TrimSpace(home) == "" {
		return true
	}
	if gitConfigWritable(ctx, client, containerID, opts) {
		return true
	}
	if strings.TrimSpace(user) == "" {
		return false
	}
	rootOpts := shared.ExecOptions{}
	if strings.TrimSpace(home) != "" {
		rootOpts.Env = []string{"HOME=" + strings.TrimSpace(home)}
	}
	_ = client.Exec(ctx, containerID, []string{"chown", user + ":" + user, home}, rootOpts, nil, nil, nil)
	_ = client.Exec(ctx, containerID, []string{"chown", user + ":" + user, home + "/.gitconfig"}, rootOpts, nil, nil, nil)
	_ = client.Exec(ctx, containerID, []string{"chmod", "u+rw", home}, rootOpts, nil, nil, nil)
	_ = client.Exec(ctx, containerID, []string{"chmod", "u+rw", home + "/.gitconfig"}, rootOpts, nil, nil, nil)
	return gitConfigWritable(ctx, client, containerID, opts)
}

func gitConfigWritable(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions) bool {
	script := `test -d "$HOME" && test -w "$HOME" && { [ ! -e "$HOME/.gitconfig" ] || { test -f "$HOME/.gitconfig" && test -w "$HOME/.gitconfig"; }; }`
	if err := client.Exec(ctx, containerID, []string{"sh", "-lc", script}, opts, nil, nil, nil); err != nil {
		return false
	}
	return true
}

func execGitConfig(ctx context.Context, client *shared.Client, containerID string, opts shared.ExecOptions, key, value string) error {
	var stderr bytes.Buffer
	if err := client.Exec(ctx, containerID, []string{"git", "config", "--global", key, value}, opts, nil, nil, &stderr); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
