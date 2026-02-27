package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	shared "si/agents/shared/docker"
	"si/tools/si/internal/vault"
)

func cmdVaultDockerExec(args []string) {
	settings := loadVaultSettingsOrFail()
	fs := flag.NewFlagSet("vault docker exec", flag.ExitOnError)
	container := fs.String("container", "", "container name or id")
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	allowInsecure := fs.Bool("allow-insecure-docker-host", false, "allow injecting secrets over an insecure remote DOCKER_HOST")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow injecting even if plaintext keys exist")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if strings.TrimSpace(*container) == "" || len(rest) == 0 {
		printUsage("usage: si vault docker exec --container <name|id> [--env-file <path>] [--repo <slug>] [--env <name>] [--allow-insecure-docker-host] [--allow-plaintext] -- <cmd...>")
		return
	}
	if insecure, reason := isInsecureDockerHost(); insecure && !*allowInsecure {
		fatal(fmt.Errorf("refusing to inject secrets over insecure docker host (%s); set --allow-insecure-docker-host to override", reason))
	}

	envName := strings.TrimSpace(*envFlag)
	if envName == "" {
		envName = strings.TrimSpace(*scopeAlias)
	}
	fileValue := strings.TrimSpace(*envFile)
	if strings.TrimSpace(*fileAlias) != "" {
		fileValue = strings.TrimSpace(*fileAlias)
	}
	target, err := resolveSIVaultTarget(strings.TrimSpace(*repoFlag), envName, fileValue)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.EnvFile)
	if err != nil {
		fatal(err)
	}
	material, err := ensureSIVaultKeyMaterial(settings, target)
	if err != nil {
		fatal(err)
	}
	values, plaintextKeys, err := decryptDotenvValues(doc, siVaultPrivateKeyCandidates(material))
	if err != nil {
		fatal(err)
	}
	if len(plaintextKeys) > 0 {
		sort.Strings(plaintextKeys)
		if !*allowPlaintext {
			fatal(fmt.Errorf("dotenv file contains plaintext keys: %s (run `si vault encrypt` or pass --allow-plaintext)", strings.Join(plaintextKeys, ", ")))
		}
		warnf("dotenv file contains plaintext keys (allowed): %s", strings.Join(plaintextKeys, ", "))
	}

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	id, _, err := client.ContainerByName(context.Background(), strings.TrimSpace(*container))
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("container not found: %s", strings.TrimSpace(*container)))
	}
	tty := isInteractiveTerminal()
	opts := shared.ExecOptions{Env: envPairs(values), TTY: tty}
	if err := client.Exec(context.Background(), id, rest, opts, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fatal(err)
	}
}

func isInsecureDockerHost() (bool, string) {
	host := strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	if host == "" || strings.HasPrefix(host, "unix://") || strings.HasPrefix(host, "npipe://") {
		return false, ""
	}
	if strings.HasPrefix(host, "tcp://") {
		if strings.TrimSpace(os.Getenv("DOCKER_TLS_VERIFY")) == "" {
			return true, "DOCKER_HOST uses tcp:// without DOCKER_TLS_VERIFY"
		}
		return false, ""
	}
	return true, "unknown DOCKER_HOST scheme"
}
