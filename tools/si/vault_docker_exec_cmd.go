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
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	allowInsecure := fs.Bool("allow-insecure-docker-host", false, "allow injecting secrets over an insecure remote DOCKER_HOST")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow injecting even if plaintext keys exist (not recommended)")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if strings.TrimSpace(*container) == "" || len(rest) == 0 {
		printUsage("usage: si vault docker exec --container <name|id> [--vault-dir <path>] [--env <name>] -- <cmd...>")
		return
	}

	if insecure, reason := isInsecureDockerHost(); insecure && !*allowInsecure {
		fatal(fmt.Errorf("refusing to inject secrets over insecure docker host (%s); set --allow-insecure-docker-host to override", reason))
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	if _, err := vaultRequireTrusted(settings, target, doc); err != nil {
		fatal(err)
	}
	info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
	if err != nil {
		fatal(err)
	}
	dec, err := vault.DecryptEnv(doc, info.Identity)
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
	keys := make([]string, 0, len(dec.Values))
	for k := range dec.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vaultAuditEvent(settings, target, "docker_exec", map[string]any{
		"envFile":      target.File,
		"container":    strings.TrimSpace(*container),
		"cmd0":         rest[0],
		"argsLen":      len(rest) - 1,
		"keysCount":    len(keys),
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
