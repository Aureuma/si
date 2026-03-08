package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const usageText = `Import plaintext .env files into si vault (native SI format).

Defaults:
  --src .
  --section default
  --identity-file $SI_VAULT_IDENTITY_FILE or ~/.si/vault/keys/age.key

Examples:
  tools/vault/import-dotenv-to-si-vault.sh --src .
  tools/vault/import-dotenv-to-si-vault.sh --src . --section app-dev
  tools/vault/import-dotenv-to-si-vault.sh --src . --dry-run

Notes:
  - This reads plaintext .env files. Use for migration/bootstrap only.
  - Target env is inferred per file:
      *.prod*|*.production* -> prod
      otherwise             -> dev
  - Requires: the si binary on PATH.`

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type config struct {
	Src          string
	Section      string
	IdentityFile string
	DryRun       bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	cfg, showHelp, err := parseArgs(args)
	if showHelp {
		_, _ = fmt.Fprintln(stdout, usageText)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		_, _ = fmt.Fprintln(stderr, usageText)
		return 2
	}

	if _, err := os.Stat(cfg.Src); err != nil {
		_, _ = fmt.Fprintf(stderr, "source directory not found: %s\n", cfg.Src)
		return 1
	}
	if _, err := exec.LookPath("si"); err != nil {
		_, _ = fmt.Fprintln(stderr, "si not found on PATH")
		return 1
	}
	if _, err := os.Stat(cfg.IdentityFile); err != nil {
		_, _ = fmt.Fprintf(stderr, "vault identity file not found: %s\n", cfg.IdentityFile)
		_, _ = fmt.Fprintln(stderr, "hint: export SI_VAULT_IDENTITY_FILE=... or pass --identity-file")
		return 1
	}

	envFiles, err := listEnvFiles(cfg.Src)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "find env files: %v\n", err)
		return 1
	}
	if len(envFiles) == 0 {
		_, _ = fmt.Fprintf(stderr, "no .env* files found in: %s\n", cfg.Src)
		return 1
	}

	for _, path := range envFiles {
		base := filepath.Base(path)
		targetEnv := inferTargetEnv(base)
		_, _ = fmt.Fprintf(stdout, "import: %s -> si vault env=%s section=%s\n", path, targetEnv, cfg.Section)

		raw, err := os.ReadFile(path)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "read %s: %v\n", path, err)
			return 1
		}
		values := parseDotenv(string(raw))
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if cfg.DryRun {
				_, _ = fmt.Fprintf(stdout, "dry-run: %s:%s:%s\n", targetEnv, cfg.Section, key)
				continue
			}
			if err := setVaultValue(cfg.IdentityFile, targetEnv, cfg.Section, key, values[key]); err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return 1
			}
			_, _ = fmt.Fprintf(stdout, "imported: %s:%s:%s\n", targetEnv, cfg.Section, key)
		}
	}
	return 0
}

func parseArgs(args []string) (config, bool, error) {
	home, _ := os.UserHomeDir()
	defaultIdentity := filepath.Join(home, ".si", "vault", "keys", "age.key")
	if envIdentity := strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")); envIdentity != "" {
		defaultIdentity = envIdentity
	}

	fs := flag.NewFlagSet("import-dotenv-to-si-vault", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	src := fs.String("src", ".", "source directory")
	section := fs.String("section", "default", "vault section")
	identityFile := fs.String("identity-file", defaultIdentity, "vault identity file")
	dryRun := fs.Bool("dry-run", false, "dry run")
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		return config{}, false, err
	}
	if *help {
		return config{}, true, nil
	}
	if fs.NArg() > 0 {
		return config{}, false, fmt.Errorf("unknown arg: %s", strings.Join(fs.Args(), " "))
	}
	cfg := config{
		Src:          strings.TrimSpace(*src),
		Section:      strings.TrimSpace(*section),
		IdentityFile: strings.TrimSpace(*identityFile),
		DryRun:       *dryRun,
	}
	if cfg.Src == "" {
		cfg.Src = "."
	}
	if cfg.IdentityFile == "" {
		return config{}, false, errors.New("identity file required")
	}
	return cfg, false, nil
}

func listEnvFiles(src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, ".env") {
			continue
		}
		if name == ".env.keys" || name == ".env.vault" {
			continue
		}
		out = append(out, filepath.Join(src, name))
	}
	sort.Strings(out)
	return out, nil
}

func inferTargetEnv(base string) string {
	lower := strings.ToLower(base)
	if strings.Contains(lower, ".prod") || strings.Contains(lower, "production") {
		return "prod"
	}
	return "dev"
}

func parseDotenv(content string) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		value := strings.TrimSpace(v)
		if !keyPattern.MatchString(key) {
			continue
		}
		if len(value) >= 2 {
			if value[0] == '\'' && value[len(value)-1] == '\'' {
				value = value[1 : len(value)-1]
			} else if value[0] == '"' && value[len(value)-1] == '"' {
				value = decodeDoubleQuoted(value[1 : len(value)-1])
			}
		}
		out[key] = value
	}
	return out
}

func decodeDoubleQuoted(raw string) string {
	decoded, err := strconv.Unquote("\"" + strings.ReplaceAll(raw, "\"", "\\\"") + "\"")
	if err != nil {
		return raw
	}
	return decoded
}

func setVaultValue(identityFile, targetEnv, section, key, value string) error {
	args := []string{"vault", "set", "--stdin", "--env", targetEnv, "--format"}
	if strings.TrimSpace(section) != "" {
		args = append(args, "--section", strings.TrimSpace(section))
	}
	args = append(args, key)

	cmd := exec.Command("si", args...)
	cmd.Stdin = strings.NewReader(value)
	cmd.Stdout = ioDiscard{}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "SI_VAULT_IDENTITY_FILE="+identityFile)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return errors.New(msg)
	}
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
