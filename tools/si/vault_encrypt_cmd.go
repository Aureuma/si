package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdVaultEncrypt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault encrypt", flag.ExitOnError)
	var envFiles multiFlag
	fs.Var(&envFiles, "env-file", "dotenv file path (repeatable)")
	fs.Var(&envFiles, "f", "alias for --env-file")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	var includeKeys multiFlag
	var excludeKeys multiFlag
	fs.Var(&includeKeys, "key", "key filter (repeatable, supports glob)")
	fs.Var(&excludeKeys, "exclude-key", "exclude key filter (repeatable, supports glob)")
	stdout := fs.Bool("stdout", false, "print transformed file to stdout instead of writing")
	reencrypt := fs.Bool("reencrypt", false, "re-encrypt values already encrypted")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault encrypt [--env-file <path>]... [--repo <slug>] [--env <name>] [--key <glob>] [--exclude-key <glob>] [--stdout] [--reencrypt]")
		return
	}
	envName := strings.TrimSpace(*envFlag)
	if envName == "" {
		envName = strings.TrimSpace(*scopeAlias)
	}
	paths := collectVaultEnvFiles(envFiles, strings.TrimSpace(*fileAlias))
	if len(paths) == 0 {
		paths = []string{defaultSIVaultDotenvFile}
	}
	include := parseFilterPatterns(includeKeys)
	exclude := parseFilterPatterns(excludeKeys)

	for idx, candidate := range paths {
		target, err := resolveSIVaultTarget(strings.TrimSpace(*repoFlag), envName, candidate)
		if err != nil {
			fatal(err)
		}
		material, err := ensureSIVaultKeyMaterial(settings, target)
		if err != nil {
			fatal(err)
		}
		doc, err := readDotenvOrEmpty(target.EnvFile)
		if err != nil {
			fatal(err)
		}
		if _, err := ensureSIVaultPublicKeyHeader(&doc, material.PublicKey); err != nil {
			fatal(err)
		}
		stats, err := encryptDotenvDoc(&doc, material.PublicKey, siVaultPrivateKeyCandidates(material), include, exclude, *reencrypt)
		if err != nil {
			fatal(err)
		}
		if *stdout {
			if idx > 0 {
				fmt.Print("\n")
			}
			fmt.Print(string(doc.Bytes()))
		} else {
			if err := writeDotenv(target.EnvFile, doc); err != nil {
				fatal(err)
			}
		}
		if !*stdout {
			fmt.Printf("file:       %s\n", filepath.Clean(target.EnvFile))
			fmt.Printf("repo/env:   %s/%s\n", target.Repo, target.Env)
			fmt.Printf("encrypted:  %d\n", stats.Encrypted)
			if *reencrypt {
				fmt.Printf("reencrypted:%d\n", stats.Reencrypted)
			}
			fmt.Printf("skipped:    %d\n", stats.SkippedEncrypted)
		}
	}
}

func collectVaultEnvFiles(values multiFlag, fallback string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	appendPath := func(pathValue string) {
		pathValue = strings.TrimSpace(pathValue)
		if pathValue == "" {
			return
		}
		if !filepath.IsAbs(pathValue) {
			cwd, err := os.Getwd()
			if err == nil {
				pathValue = filepath.Join(cwd, pathValue)
			}
		}
		pathValue = filepath.Clean(pathValue)
		if _, ok := seen[pathValue]; ok {
			return
		}
		seen[pathValue] = struct{}{}
		out = append(out, pathValue)
	}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			appendPath(part)
		}
	}
	appendPath(fallback)
	return out
}
