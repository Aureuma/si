package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdVaultDecrypt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault decrypt", flag.ExitOnError)
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
	stdout := fs.Bool("stdout", false, "print transformed file to stdout")
	inplace := fs.Bool("inplace", false, "write decrypted values back to file")
	inPlaceAlias := fs.Bool("in-place", false, "alias for --inplace")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault decrypt [--env-file <path>]... [--repo <slug>] [--env <name>] [--key <glob>] [--exclude-key <glob>] [--stdout] [--inplace]")
		return
	}

	writeInPlace := *inplace || *inPlaceAlias
	printStdout := *stdout || !writeInPlace
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
		encryptedBytes, readErr := osReadFileIfExists(target.EnvFile)
		if readErr != nil {
			fatal(readErr)
		}
		doc, err := readDotenvOrEmpty(target.EnvFile)
		if err != nil {
			fatal(err)
		}
		stats, err := decryptDotenvDoc(&doc, siVaultPrivateKeyCandidates(material), include, exclude)
		if err != nil {
			fatal(err)
		}

		if writeInPlace {
			if len(encryptedBytes) > 0 {
				if err := saveEncryptedRestoreBackup(target.EnvFile, encryptedBytes); err != nil {
					fatal(err)
				}
			}
			if err := writeDotenv(target.EnvFile, doc); err != nil {
				fatal(err)
			}
			fmt.Printf("file:      %s\n", filepath.Clean(target.EnvFile))
			fmt.Printf("repo/env:  %s/%s\n", target.Repo, target.Env)
			fmt.Printf("decrypted: %d\n", stats.Decrypted)
			fmt.Printf("backup:    %s\n", filepath.Clean(restoreBackupPathForEnvFile(target.EnvFile)))
		}

		if printStdout {
			if idx > 0 {
				fmt.Print("\n")
			}
			fmt.Print(string(doc.Bytes()))
		}
	}
}

func osReadFileIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}
