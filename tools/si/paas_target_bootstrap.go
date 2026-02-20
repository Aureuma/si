package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	paasSSHPassBinEnvKey         = "SI_PAAS_SSHPASS_BIN"
	paasTargetBootstrapUsageText = "usage: si paas target bootstrap --target <id> --password-env <env_key> [--public-key <path>] [--timeout <duration>] [--json]"
)

func cmdPaasTargetBootstrap(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target bootstrap", flag.ExitOnError)
	targetName := fs.String("target", "", "target id")
	passwordEnv := fs.String("password-env", "", "env var containing target SSH password")
	publicKeyPath := fs.String("public-key", "", "public key path to install")
	timeout := fs.String("timeout", "45s", "bootstrap timeout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTargetBootstrapUsageText)
		return
	}
	if !requirePaasValue(*targetName, "target", paasTargetBootstrapUsageText) {
		return
	}
	if !requirePaasValue(*passwordEnv, "password-env", paasTargetBootstrapUsageText) {
		return
	}
	timeoutValue, err := time.ParseDuration(strings.TrimSpace(*timeout))
	if err != nil || timeoutValue <= 0 {
		fatal(fmt.Errorf("invalid --timeout %q", strings.TrimSpace(*timeout)))
	}

	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	idx := findPaasTarget(store, strings.TrimSpace(*targetName))
	if idx == -1 {
		fatal(fmt.Errorf("target %q not found", strings.TrimSpace(*targetName)))
	}
	target := store.Targets[idx]
	if isPaasLocalTarget(target) {
		fatal(fmt.Errorf("target %q uses local transport; bootstrap is not required", target.Name))
	}
	password := strings.TrimSpace(os.Getenv(strings.TrimSpace(*passwordEnv)))
	if password == "" {
		fatal(fmt.Errorf("environment variable %q is empty or missing", strings.TrimSpace(*passwordEnv)))
	}
	resolvedPublicKeyPath := strings.TrimSpace(*publicKeyPath)
	if resolvedPublicKeyPath == "" {
		resolvedPublicKeyPath, err = resolveDefaultPublicKeyPath()
		if err != nil {
			fatal(err)
		}
	}
	keyRaw, err := readLocalFile(resolvedPublicKeyPath)
	if err != nil {
		fatal(err)
	}
	publicKey := strings.TrimSpace(string(keyRaw))
	if publicKey == "" {
		fatal(fmt.Errorf("public key file is empty: %s", resolvedPublicKeyPath))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutValue)
	defer cancel()
	if err := runPaasSSHBootstrapWithPassword(ctx, target, password, publicKey); err != nil {
		fatal(err)
	}
	if _, err := runPaasSSHCommand(ctx, target, "echo si-key-auth-ok"); err != nil {
		fatal(fmt.Errorf("post-bootstrap key-auth validation failed: %w", err))
	}

	target.AuthMethod = "key"
	target.UpdatedAt = utcNowRFC3339()
	store.Targets[idx] = target
	if err := savePaasTargetStore(currentPaasContext(), store); err != nil {
		fatal(err)
	}

	printPaasScaffold("target bootstrap", map[string]string{
		"auth_method":  target.AuthMethod,
		"password_env": strings.TrimSpace(*passwordEnv),
		"public_key":   resolvedPublicKeyPath,
		"target":       target.Name,
		"timeout":      timeoutValue.String(),
	}, jsonOut)
}

func runPaasSSHBootstrapWithPassword(ctx context.Context, target paasTarget, password, publicKey string) error {
	switch resolvePaasSSHTransportEngine() {
	case paasSSHEngineExec:
		return runPaasSSHBootstrapWithPasswordExec(ctx, target, password, publicKey)
	default:
		return runPaasSSHBootstrapWithPasswordGo(ctx, target, password, publicKey)
	}
}

func runPaasSSHBootstrapWithPasswordGo(ctx context.Context, target paasTarget, password, publicKey string) error {
	passwordMethods, err := buildPaasPasswordAuthMethods(password)
	if err != nil {
		return err
	}
	quotedKey := quoteSingle(publicKey)
	remoteScript := fmt.Sprintf("mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && (grep -qxF %s ~/.ssh/authorized_keys || echo %s >> ~/.ssh/authorized_keys)", quotedKey, quotedKey)
	if _, err := runPaasSSHCommandGoWithAuth(ctx, target, remoteScript, passwordMethods); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	return nil
}

func runPaasSSHBootstrapWithPasswordExec(ctx context.Context, target paasTarget, password, publicKey string) error {
	sshpassBin := strings.TrimSpace(os.Getenv(paasSSHPassBinEnvKey))
	if sshpassBin == "" {
		sshpassBin = "sshpass"
	}
	sshBin := strings.TrimSpace(os.Getenv(paasSSHBinEnvKey))
	if sshBin == "" {
		sshBin = "ssh"
	}
	quotedKey := quoteSingle(publicKey)
	remoteScript := fmt.Sprintf("mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && (grep -qxF %s ~/.ssh/authorized_keys || echo %s >> ~/.ssh/authorized_keys)", quotedKey, quotedKey)
	args := []string{
		"-e",
		sshBin,
		"-p", fmt.Sprintf("%d", target.Port),
		"-o", "PreferredAuthentications=password",
		"-o", "PubkeyAuthentication=no",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", target.User, target.Host),
		remoteScript,
	}
	cmd := exec.CommandContext(ctx, sshpassBin, args...)
	cmd.Env = append(os.Environ(), "SSHPASS="+password)
	var stderr bytes.Buffer
	cmd.Stdout = ioDiscard{}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("bootstrap failed: %s", errMsg)
	}
	return nil
}

func resolveDefaultPublicKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519.pub"),
		filepath.Join(home, ".ssh", "id_rsa.pub"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no default public key found under ~/.ssh (checked id_ed25519.pub and id_rsa.pub)")
}

func quoteSingle(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
