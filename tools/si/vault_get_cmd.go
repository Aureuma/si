package main

import (
	"flag"
	"fmt"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultGet(args []string) {
	settings := loadVaultSettingsOrFail()
	args = stripeFlagsFirst(args, map[string]bool{"reveal": true})
	fs := flag.NewFlagSet("vault get", flag.ExitOnError)
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	reveal := fs.Bool("reveal", false, "print decrypted value")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 1 {
		printUsage("usage: si vault get <KEY> [--env-file <path>] [--repo <slug>] [--env <name>] [--reveal]")
		return
	}
	key := strings.TrimSpace(fs.Arg(0))
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
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
	raw, ok := doc.Lookup(key)
	if !ok {
		fatal(fmt.Errorf("key not found: %s", key))
	}
	value, err := vault.NormalizeDotenvValue(raw)
	if err != nil {
		fatal(err)
	}
	if !*reveal {
		if vault.IsSIVaultEncryptedValue(value) {
			fmt.Printf("%s: encrypted\n", key)
		} else {
			fmt.Printf("%s: plaintext\n", key)
		}
		return
	}
	if !vault.IsSIVaultEncryptedValue(value) {
		fmt.Println(value)
		return
	}
	material, err := ensureSIVaultKeyMaterial(settings, target)
	if err != nil {
		fatal(err)
	}
	if err := ensureSIVaultDecryptMaterialCompatibility(doc, material, target, settings); err != nil {
		fatal(err)
	}
	plain, err := vault.DecryptSIVaultValue(value, siVaultPrivateKeyCandidates(material))
	if err != nil {
		fatal(err)
	}
	fmt.Println(plain)
}
