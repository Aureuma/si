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
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault docker exec", flag.ExitOnError)
	container := fs.String("container", "", "container name or id")
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	allowInsecure := fs.Bool("allow-insecure-docker-host", false, "allow injecting secrets over an insecure remote DOCKER_HOST")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow injecting even if plaintext keys exist (not recommended)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if strings.TrimSpace(*container) == "" || len(rest) == 0 {
		printUsage("usage: si vault docker exec --container <name|id> [--scope <name>] [--allow-insecure-docker-host] [--allow-plaintext] -- <cmd...>")
		return
	}

	if insecure, reason := isInsecureDockerHost(); insecure && !*allowInsecure {
		fatal(fmt.Errorf("refusing to inject secrets over insecure docker host (%s); set --allow-insecure-docker-host to override", reason))
	}

	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	values, used, sunErr := vaultSunKVLoadRawValues(settings, target)
	if sunErr != nil {
		fatal(sunErr)
	}
	if !used {
		fatal(fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`"))
	}
	sourceKeys := make([]string, 0, len(values))
	for key := range values {
		sourceKeys = append(sourceKeys, key)
	}
	sort.Strings(sourceKeys)
	lines := make([]string, 0, len(sourceKeys))
	for _, key := range sourceKeys {
		lines = append(lines, key+"="+values[key])
	}
	doc := vault.ParseDotenv([]byte(strings.Join(lines, "\n") + "\n"))
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_docker_exec")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}
	dec, err := vault.DecryptEnv(doc, identity)
	if err != nil {
		fatal(err)
	}
	if len(dec.PlaintextKeys) > 0 {
		sort.Strings(dec.PlaintextKeys)
		if !*allowPlaintext {
			fatal(fmt.Errorf("vault file contains plaintext keys: %s (run `si vault encrypt` or pass --allow-plaintext)", strings.Join(dec.PlaintextKeys, ", ")))
		}
		warnf("vault file contains plaintext keys (allowed): %s", strings.Join(dec.PlaintextKeys, ", "))
	}
	keyNames := make([]string, 0, len(dec.Values))
	for k := range dec.Values {
		keyNames = append(keyNames, k)
	}
	sort.Strings(keyNames)
	vaultAuditEvent(settings, target, "docker_exec", map[string]any{
		"scope":        strings.TrimSpace(target.File),
		"container":    strings.TrimSpace(*container),
		"cmd0":         rest[0],
		"argsLen":      len(rest) - 1,
		"keysCount":    len(keyNames),
		"remoteDocker": func() bool { insecure, _ := isInsecureDockerHost(); return insecure }(),
	})

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
	opts := shared.ExecOptions{Env: envPairs(dec.Values), TTY: tty}
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
