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
	settings := loadVaultSettingsOrFail()
	fs := flag.NewFlagSet("vault run", flag.ExitOnError)
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow plaintext values")
	shellFlag := fs.Bool("shell", false, "run command via shell")
	shellInteractive := fs.Bool("shell-interactive", false, "use -ic instead of -lc when --shell")
	shellPath := fs.String("shell-path", "", "shell binary for --shell")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		printUsage("usage: si vault run [--env-file <path>] [--repo <slug>] [--env <name>] [--allow-plaintext] [--shell] -- <cmd...>")
		return
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
	if err := ensureSIVaultDecryptMaterialCompatibility(doc, material, target, settings); err != nil {
		fatal(err)
	}
	values, plaintextKeys, err := decryptDotenvValues(doc, siVaultPrivateKeyCandidates(material))
	if err != nil {
		fatal(err)
	}
	if len(plaintextKeys) > 0 && !*allowPlaintext {
		fatal(fmt.Errorf("dotenv contains plaintext keys: %s (use --allow-plaintext or run `si vault encrypt`)", strings.Join(plaintextKeys, ", ")))
	}

	var cmd *exec.Cmd
	if *shellFlag {
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
		// #nosec G204 -- operator-provided command string.
		cmd = exec.CommandContext(context.Background(), sh, mode, strings.Join(rest, " "))
	} else {
		// #nosec G204 -- operator-provided command/args.
		cmd = exec.CommandContext(context.Background(), rest[0], rest[1:]...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(envWithoutGitVars(os.Environ()), envPairs(values)...)
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
	for _, key := range keys {
		out = append(out, key+"="+values[key])
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
		if strings.HasPrefix(strings.TrimSpace(key), "GIT_") {
			continue
		}
		out = append(out, pair)
	}
	return out
}
