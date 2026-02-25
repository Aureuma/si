package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	ecies "github.com/ecies/go/v2"

	"si/tools/si/internal/vault"
)

const (
	defaultSIVaultEnv        = "dev"
	defaultSIVaultDotenvFile = ".env"
)

var vaultRepoEnvSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type siVaultTarget struct {
	Repo    string
	Env     string
	EnvFile string
	RepoDir string
}

type siVaultEncryptStats struct {
	Encrypted        int
	Reencrypted      int
	SkippedEncrypted int
}

type siVaultDecryptStats struct {
	Decrypted int
}

func resolveSIVaultTarget(repoFlag string, envFlag string, envFileFlag string) (siVaultTarget, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return siVaultTarget{}, err
	}
	repoDir := cwd
	if gitRoot, gitErr := vault.GitRoot(cwd); gitErr == nil && strings.TrimSpace(gitRoot) != "" {
		repoDir = gitRoot
	}

	repo := normalizeVaultRepoEnvSlug(repoFlag)
	if repo == "" {
		repo = normalizeVaultRepoEnvSlug(filepath.Base(repoDir))
	}
	if repo == "" {
		repo = "repo"
	}
	if !vaultRepoEnvSlugPattern.MatchString(repo) {
		return siVaultTarget{}, fmt.Errorf("invalid repo %q (allowed: [a-z0-9._-], max 64 chars, must start with [a-z0-9])", repo)
	}

	envFile := strings.TrimSpace(envFileFlag)
	if envFile == "" {
		envFile = strings.TrimSpace(os.Getenv("SI_VAULT_ENV_FILE"))
	}
	if envFile == "" {
		envFile = defaultSIVaultDotenvFile
	}

	env := normalizeVaultRepoEnvSlug(envFlag)
	if env == "" {
		env = normalizeVaultRepoEnvSlug(os.Getenv("SI_VAULT_ENV"))
	}
	if env == "" {
		env = inferSIVaultEnvFromEnvFile(envFile)
	}
	if env == "" {
		env = defaultSIVaultEnv
	}
	if !vaultRepoEnvSlugPattern.MatchString(env) {
		return siVaultTarget{}, fmt.Errorf("invalid env %q (allowed: [a-z0-9._-], max 64 chars, must start with [a-z0-9])", env)
	}

	if !filepath.IsAbs(envFile) {
		envFile = filepath.Join(cwd, envFile)
	}

	return siVaultTarget{
		Repo:    repo,
		Env:     env,
		EnvFile: filepath.Clean(envFile),
		RepoDir: filepath.Clean(repoDir),
	}, nil
}

func inferSIVaultEnvFromEnvFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasPrefix(base, ".env.") {
		return ""
	}
	suffix := strings.TrimPrefix(base, ".env.")
	if suffix == "" {
		return ""
	}
	envPart := suffix
	if idx := strings.Index(envPart, "."); idx >= 0 {
		envPart = envPart[:idx]
	}
	return normalizeVaultRepoEnvSlug(envPart)
}

func normalizeVaultRepoEnvSlug(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, ch := range raw {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
			lastDash = false
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
			lastDash = false
		case ch == '.', ch == '_', ch == '-':
			b.WriteRune(ch)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-._")
	}
	return out
}

func ensureSIVaultKeyMaterial(settings Settings, target siVaultTarget) (sunVaultPrivateKey, error) {
	// Best-effort legacy compatibility: hydrate the old age identity into the
	// process environment so legacy encrypted:si:v1/v2 values can still decrypt.
	_, _ = vaultEnsureStrictSunIdentity(settings, "si_vault_legacy_compat")

	client, err := sunClientFromSettings(settings)
	if err != nil {
		return sunVaultPrivateKey{}, err
	}
	timeout := time.Duration(settings.Sun.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	current, err := client.getVaultPrivateKey(ctx, target.Repo, target.Env)
	if err == nil {
		return normalizeSunVaultMaterial(current, target)
	}
	if !isSunNotFoundError(err) {
		return sunVaultPrivateKey{}, err
	}
	publicKey, privateKey, genErr := vault.GenerateSIVaultKeyPair()
	if genErr != nil {
		return sunVaultPrivateKey{}, genErr
	}
	created, putErr := client.putVaultPrivateKey(ctx, sunVaultPrivateKey{
		Repo:       target.Repo,
		Env:        target.Env,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}, nil)
	if putErr != nil {
		return sunVaultPrivateKey{}, putErr
	}
	return normalizeSunVaultMaterial(created, target)
}

func normalizeSunVaultMaterial(in sunVaultPrivateKey, target siVaultTarget) (sunVaultPrivateKey, error) {
	in.Repo = normalizeVaultRepoEnvSlug(firstNonEmpty(strings.TrimSpace(in.Repo), target.Repo))
	in.Env = normalizeVaultRepoEnvSlug(firstNonEmpty(strings.TrimSpace(in.Env), target.Env))
	in.PublicKey = strings.TrimSpace(strings.ToLower(in.PublicKey))
	in.PrivateKey = strings.TrimSpace(strings.ToLower(in.PrivateKey))
	if in.PrivateKey == "" {
		return sunVaultPrivateKey{}, fmt.Errorf("sun vault key material missing private key for %s/%s", target.Repo, target.Env)
	}
	if in.PublicKey == "" {
		privateKey, err := ecies.NewPrivateKeyFromHex(in.PrivateKey)
		if err != nil {
			return sunVaultPrivateKey{}, fmt.Errorf("sun vault private key is invalid: %w", err)
		}
		in.PublicKey = strings.TrimSpace(privateKey.PublicKey.Hex(true))
	}
	normalizedBackups := make([]string, 0, len(in.BackupPrivateKeys))
	seen := map[string]struct{}{}
	for _, backup := range in.BackupPrivateKeys {
		backup = strings.TrimSpace(strings.ToLower(backup))
		if backup == "" || backup == in.PrivateKey {
			continue
		}
		if _, ok := seen[backup]; ok {
			continue
		}
		seen[backup] = struct{}{}
		normalizedBackups = append(normalizedBackups, backup)
	}
	in.BackupPrivateKeys = normalizedBackups
	return in, nil
}

func siVaultPrivateKeyCandidates(material sunVaultPrivateKey) []string {
	seen := map[string]struct{}{}
	out := []string{}
	appendKey := func(raw string) {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	envRaw := strings.TrimSpace(os.Getenv(vault.SIVaultPrivateKeyName))
	if envRaw != "" {
		for _, part := range strings.Split(envRaw, ",") {
			appendKey(part)
		}
	}
	appendKey(material.PrivateKey)
	for _, backup := range material.BackupPrivateKeys {
		appendKey(backup)
	}
	return out
}

func readDotenvOrEmpty(path string) (vault.DotenvFile, error) {
	doc, err := vault.ReadDotenvFile(path)
	if err == nil {
		if strings.TrimSpace(doc.DefaultNL) == "" {
			doc.DefaultNL = "\n"
		}
		return doc, nil
	}
	if os.IsNotExist(err) {
		return vault.DotenvFile{DefaultNL: "\n"}, nil
	}
	return vault.DotenvFile{}, err
}

func writeDotenv(path string, doc vault.DotenvFile) error {
	return vault.WriteDotenvFileAtomic(path, doc.Bytes())
}

func ensureSIVaultPublicKeyHeader(doc *vault.DotenvFile, publicKey string) (bool, error) {
	if doc == nil {
		return false, fmt.Errorf("dotenv document is nil")
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return false, fmt.Errorf("public key is required")
	}
	if doc.DefaultNL == "" {
		doc.DefaultNL = "\n"
	}
	before := string(doc.Bytes())
	filtered := make([]vault.RawLine, 0, len(doc.Lines))
	for _, line := range doc.Lines {
		key, ok := dotenvAssignmentKey(line.Text)
		if ok && key == vault.SIVaultPublicKeyName {
			continue
		}
		filtered = append(filtered, line)
	}
	doc.Lines = filtered

	insertAt := 0
	if len(doc.Lines) > 0 && strings.HasPrefix(doc.Lines[0].Text, "#!") {
		insertAt = 1
		if doc.Lines[0].NL == "" {
			doc.Lines[0].NL = doc.DefaultNL
		}
	}
	headerLine := vault.RawLine{
		Text: vault.SIVaultPublicKeyName + "=" + vault.RenderDotenvValuePlain(publicKey),
		NL:   doc.DefaultNL,
	}
	doc.Lines = append(doc.Lines[:insertAt], append([]vault.RawLine{headerLine}, doc.Lines[insertAt:]...)...)

	blankIdx := insertAt + 1
	hasBlankAfterHeader := blankIdx < len(doc.Lines) && strings.TrimSpace(doc.Lines[blankIdx].Text) == ""
	if hasBlankAfterHeader {
		if doc.Lines[blankIdx].NL == "" {
			doc.Lines[blankIdx].NL = doc.DefaultNL
		}
	} else {
		blankLine := vault.RawLine{Text: "", NL: doc.DefaultNL}
		doc.Lines = append(doc.Lines[:blankIdx], append([]vault.RawLine{blankLine}, doc.Lines[blankIdx:]...)...)
	}

	for blankIdx+1 < len(doc.Lines) && strings.TrimSpace(doc.Lines[blankIdx+1].Text) == "" {
		doc.Lines = append(doc.Lines[:blankIdx+1], doc.Lines[blankIdx+2:]...)
	}
	return before != string(doc.Bytes()), nil
}

func dotenvAssignmentKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", false
	}
	if err := vault.ValidateKeyName(key); err != nil {
		return "", false
	}
	return key, true
}

func parseFilterPatterns(items []string) []string {
	out := []string{}
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
	}
	return out
}

func matchesFilters(key string, include []string, exclude []string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, pattern := range exclude {
		ok, _ := path.Match(pattern, key)
		if ok || pattern == key {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, pattern := range include {
		ok, _ := path.Match(pattern, key)
		if ok || pattern == key {
			return true
		}
	}
	return false
}

func encryptDotenvDoc(doc *vault.DotenvFile, publicKey string, privateKeyCandidates []string, include []string, exclude []string, reencrypt bool) (siVaultEncryptStats, error) {
	stats := siVaultEncryptStats{}
	entries, err := vault.Entries(*doc)
	if err != nil {
		return stats, err
	}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || key == vault.SIVaultPublicKeyName {
			continue
		}
		if !matchesFilters(key, include, exclude) {
			continue
		}
		if vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			if !reencrypt {
				stats.SkippedEncrypted++
				continue
			}
			plain, decErr := vault.DecryptSIVaultValue(entry.ValueRaw, privateKeyCandidates)
			if decErr != nil {
				return stats, fmt.Errorf("decrypt %s for reencrypt: %w", key, decErr)
			}
			cipher, encErr := vault.EncryptSIVaultValue(plain, publicKey)
			if encErr != nil {
				return stats, fmt.Errorf("encrypt %s: %w", key, encErr)
			}
			if _, setErr := doc.Set(key, vault.RenderDotenvValuePlain(cipher), vault.SetOptions{}); setErr != nil {
				return stats, setErr
			}
			stats.Reencrypted++
			continue
		}
		cipher, encErr := vault.EncryptSIVaultValue(entry.ValueRaw, publicKey)
		if encErr != nil {
			return stats, fmt.Errorf("encrypt %s: %w", key, encErr)
		}
		if _, setErr := doc.Set(key, vault.RenderDotenvValuePlain(cipher), vault.SetOptions{}); setErr != nil {
			return stats, setErr
		}
		stats.Encrypted++
	}
	return stats, nil
}

func decryptDotenvDoc(doc *vault.DotenvFile, privateKeyCandidates []string, include []string, exclude []string) (siVaultDecryptStats, error) {
	stats := siVaultDecryptStats{}
	entries, err := vault.Entries(*doc)
	if err != nil {
		return stats, err
	}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || key == vault.SIVaultPublicKeyName {
			continue
		}
		if !matchesFilters(key, include, exclude) {
			continue
		}
		if !vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			continue
		}
		plain, decErr := vault.DecryptSIVaultValue(entry.ValueRaw, privateKeyCandidates)
		if decErr != nil {
			return stats, fmt.Errorf("decrypt %s: %w", key, decErr)
		}
		if _, setErr := doc.Set(key, vault.RenderDotenvValuePlain(plain), vault.SetOptions{}); setErr != nil {
			return stats, setErr
		}
		stats.Decrypted++
	}
	return stats, nil
}

func decryptDotenvValues(doc vault.DotenvFile, privateKeyCandidates []string) (map[string]string, []string, error) {
	entries, err := vault.Entries(doc)
	if err != nil {
		return nil, nil, err
	}
	values := map[string]string{}
	plaintext := []string{}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || key == vault.SIVaultPublicKeyName {
			continue
		}
		if vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			plain, decErr := vault.DecryptSIVaultValue(entry.ValueRaw, privateKeyCandidates)
			if decErr != nil {
				return nil, nil, fmt.Errorf("decrypt %s: %w", key, decErr)
			}
			values[key] = plain
			continue
		}
		values[key] = entry.ValueRaw
		plaintext = append(plaintext, key)
	}
	sort.Strings(plaintext)
	return values, plaintext, nil
}

func restoreBackupPathForEnvFile(envFile string) string {
	abs := filepath.Clean(envFile)
	sum := sha256.Sum256([]byte(abs))
	name := hex.EncodeToString(sum[:16]) + ".enc"
	return filepath.Join(filepath.Dir(abs), ".si-vault-restore", name)
}

func saveEncryptedRestoreBackup(envFile string, encrypted []byte) error {
	backupPath := restoreBackupPathForEnvFile(envFile)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(backupPath, encrypted, 0o600)
}

func loadEncryptedRestoreBackup(envFile string) ([]byte, string, error) {
	backupPath := restoreBackupPathForEnvFile(envFile)
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil, backupPath, err
	}
	return data, backupPath, nil
}
