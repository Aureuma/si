package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultRun(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault run", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow running even if plaintext keys exist (not recommended)")
	shellFlag := fs.Bool("shell", false, "run via a shell (exec: $SHELL -lc <cmd>); enables pipes/redirection/etc; does not inherit parent shell functions/aliases unless you source them")
	shellInteractive := fs.Bool("shell-interactive", false, "when --shell is set, use -ic instead of -lc (loads interactive rc; may have side effects)")
	shellPath := fs.String("shell-path", "", "when --shell is set, shell binary to use (default: $SHELL, fallback: /bin/bash)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		printUsage("usage: si vault run [--scope <name>] [--allow-plaintext] [--shell] [--shell-interactive] [--shell-path <path>] -- <cmd...>")
		return
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
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_run")
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

	keys := make([]string, 0, len(dec.Values))
	for k := range dec.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vaultAuditEvent(settings, target, "run", map[string]any{
		"scope":        strings.TrimSpace(target.File),
		"cmd0":         rest[0],
		"argsLen":      len(rest) - 1,
		"keysCount":    len(keys),
		"decryptCount": len(dec.DecryptedKeys),
		"plainCount":   len(dec.PlaintextKeys),
		"shell":        *shellFlag,
		"shellI":       *shellInteractive,
		"source":       "sun-kv",
	})

	var cmd *exec.Cmd
	if *shellFlag {
		// Note: This cannot "see" functions/aliases from the parent shell process.
		// If you want those, you must explicitly source the defining file inside the shell command.
		sh := strings.TrimSpace(*shellPath)
		if sh == "" {
			sh = strings.TrimSpace(os.Getenv("SHELL"))
		}
		if sh == "" {
			sh = "/bin/bash"
		}
		mode := "-lc"
		if *shellInteractive {
			mode = "-ic"
		}
		shellCmd := strings.Join(rest, " ")
		// #nosec G204 -- shell command is explicitly provided by the local operator.
		cmd = exec.CommandContext(context.Background(), sh, mode, shellCmd)
	} else {
		// #nosec G204 -- command is explicitly provided by the local operator.
		cmd = exec.CommandContext(context.Background(), rest[0], rest[1:]...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(envWithoutGitVars(os.Environ()), envPairs(dec.Values)...)
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func envPairs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out
}

func envWithoutGitVars(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, pair := range env {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		out = append(out, pair)
	}
	return out
}
