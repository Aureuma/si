package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

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

type siVaultDecryptabilityStats struct {
	Encrypted     int
	Decryptable   int
	Undecryptable []string
}

func loadVaultSettingsOrFail() Settings {
	settings, err := loadSettings()
	if err != nil {
		fatal(fmt.Errorf("vault settings load failed; refusing default fallback: %w", err))
	}
	return settings
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

	envFile := strings.TrimSpace(envFileFlag)
	if envFile == "" {
		envFile = strings.TrimSpace(os.Getenv("SI_VAULT_ENV_FILE"))
	}
	if envFile == "" {
		envFile = defaultSIVaultDotenvFile
	}
	if !filepath.IsAbs(envFile) {
		envFile = filepath.Join(cwd, envFile)
	}
	envFile = filepath.Clean(envFile)

	repo := normalizeVaultRepoEnvSlug(repoFlag)
	if repo == "" {
		repo = inferSIVaultRepoFromEnvFile(envFile)
	}
	if repo == "" {
		repo = normalizeVaultRepoEnvSlug(filepath.Base(repoDir))
	}
	if repo == "" {
		repo = "repo"
	}
	if !vaultRepoEnvSlugPattern.MatchString(repo) {
		return siVaultTarget{}, fmt.Errorf("invalid repo %q (allowed: [a-z0-9._-], max 64 chars, must start with [a-z0-9])", repo)
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

	return siVaultTarget{
		Repo:    repo,
		Env:     env,
		EnvFile: envFile,
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

func inferSIVaultRepoFromEnvFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasPrefix(base, ".env") {
		return ""
	}
	parent := normalizeVaultRepoEnvSlug(filepath.Base(filepath.Dir(path)))
	if parent == "" || parent == "safe" {
		return ""
	}
	return parent
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

func ensureSIVaultKeyMaterial(settings Settings, target siVaultTarget) (siVaultKeyMaterial, error) {
	_ = settings
	keyring, err := loadSIVaultKeyring()
	if err != nil {
		return siVaultKeyMaterial{}, err
	}
	canonical, hasCanonical, err := canonicalSIVaultKeyMaterial(keyring)
	if err != nil {
		return siVaultKeyMaterial{}, err
	}
	key := siVaultKeyringEntryKey(target.Repo, target.Env)
	if existing, ok := keyring.Entries[key]; ok {
		normalized, err := normalizeSIVaultMaterial(existing, target)
		if err != nil {
			return siVaultKeyMaterial{}, err
		}
		if hasCanonical {
			if err := validateSIVaultMaterialMatchesCanonical(normalized, canonical, key); err != nil {
				return siVaultKeyMaterial{}, err
			}
		}
		return normalized, nil
	}

	if !hasCanonical {
		bootstrapMaterial, ok, bootstrapErr := bootstrapSIVaultMaterialFromEnv(target)
		if bootstrapErr != nil {
			return siVaultKeyMaterial{}, bootstrapErr
		}
		if !ok {
			return siVaultKeyMaterial{}, fmt.Errorf(
				"si vault key material missing for %s/%s: canonical keypair is not initialized (set %s/%s or seed keyring explicitly)",
				target.Repo,
				target.Env,
				vault.SIVaultPublicKeyName,
				vault.SIVaultPrivateKeyName,
			)
		}
		keyring.Entries[key] = bootstrapMaterial
		if err := saveSIVaultKeyring(keyring); err != nil {
			return siVaultKeyMaterial{}, err
		}
		return bootstrapMaterial, nil
	}

	seeded := canonical
	seeded.Repo = target.Repo
	seeded.Env = target.Env
	seeded.BackupPrivateKeys = nil
	seeded.UpdatedAt = ""
	normalized, err := normalizeSIVaultMaterial(seeded, target)
	if err != nil {
		return siVaultKeyMaterial{}, err
	}
	if err := validateSIVaultMaterialMatchesCanonical(normalized, canonical, key); err != nil {
		return siVaultKeyMaterial{}, err
	}
	keyring.Entries[key] = normalized
	if err := saveSIVaultKeyring(keyring); err != nil {
		return siVaultKeyMaterial{}, err
	}
	return normalized, nil
}

func canonicalSIVaultKeyMaterial(keyring siVaultKeyring) (siVaultKeyMaterial, bool, error) {
	if len(keyring.Entries) == 0 {
		return siVaultKeyMaterial{}, false, nil
	}
	scopes := make([]string, 0, len(keyring.Entries))
	for scope := range keyring.Entries {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	var canonical siVaultKeyMaterial
	hasCanonical := false
	canonicalScope := ""
	for _, scope := range scopes {
		entry := keyring.Entries[scope]
		scopeRepo, scopeEnv := splitSIVaultKeyringEntryScope(scope)
		target := siVaultTarget{
			Repo: firstNonEmpty(strings.TrimSpace(entry.Repo), scopeRepo),
			Env:  firstNonEmpty(strings.TrimSpace(entry.Env), scopeEnv),
		}
		normalized, err := normalizeSIVaultMaterial(entry, target)
		if err != nil {
			return siVaultKeyMaterial{}, false, fmt.Errorf("invalid si vault key material for %s: %w", scope, err)
		}
		if len(normalized.BackupPrivateKeys) > 0 {
			return siVaultKeyMaterial{}, false, fmt.Errorf("si vault key sprawl detected for %s: backup_private_keys are not allowed", scope)
		}
		if !hasCanonical {
			canonical = normalized
			hasCanonical = true
			canonicalScope = scope
			continue
		}
		if err := validateSIVaultMaterialMatchesCanonical(normalized, canonical, scope); err != nil {
			return siVaultKeyMaterial{}, false, fmt.Errorf("%w (canonical_scope=%s)", err, canonicalScope)
		}
	}
	return canonical, hasCanonical, nil
}

func validateSIVaultMaterialMatchesCanonical(candidate siVaultKeyMaterial, canonical siVaultKeyMaterial, scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "<unknown>"
	}
	if len(candidate.BackupPrivateKeys) > 0 {
		return fmt.Errorf("si vault key sprawl detected for %s: backup_private_keys are not allowed", scope)
	}
	candidatePub := strings.TrimSpace(strings.ToLower(candidate.PublicKey))
	candidatePriv := strings.TrimSpace(strings.ToLower(candidate.PrivateKey))
	canonicalPub := strings.TrimSpace(strings.ToLower(canonical.PublicKey))
	canonicalPriv := strings.TrimSpace(strings.ToLower(canonical.PrivateKey))
	if candidatePub == "" || candidatePriv == "" {
		return fmt.Errorf("si vault key material for %s is incomplete", scope)
	}
	if candidatePub != canonicalPub || candidatePriv != canonicalPriv {
		return fmt.Errorf("si vault key sprawl detected for %s: keypair diverges from canonical", scope)
	}
	return nil
}

func splitSIVaultKeyringEntryScope(scope string) (string, string) {
	scope = strings.TrimSpace(strings.ToLower(scope))
	if scope == "" {
		return "", ""
	}
	parts := strings.SplitN(scope, "/", 2)
	if len(parts) != 2 {
		return normalizeVaultRepoEnvSlug(parts[0]), ""
	}
	return normalizeVaultRepoEnvSlug(parts[0]), normalizeVaultRepoEnvSlug(parts[1])
}

func bootstrapSIVaultMaterialFromEnv(target siVaultTarget) (siVaultKeyMaterial, bool, error) {
	publicKey := strings.TrimSpace(strings.ToLower(os.Getenv(vault.SIVaultPublicKeyName)))
	privateKey := strings.TrimSpace(strings.ToLower(os.Getenv(vault.SIVaultPrivateKeyName)))
	if publicKey == "" && privateKey == "" {
		return siVaultKeyMaterial{}, false, nil
	}
	if publicKey == "" || privateKey == "" {
		return siVaultKeyMaterial{}, false, fmt.Errorf(
			"si vault bootstrap requires both %s and %s",
			vault.SIVaultPublicKeyName,
			vault.SIVaultPrivateKeyName,
		)
	}
	normalized, err := normalizeSIVaultMaterial(siVaultKeyMaterial{
		Repo:       target.Repo,
		Env:        target.Env,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}, target)
	if err != nil {
		return siVaultKeyMaterial{}, false, err
	}
	return normalized, true, nil
}

func normalizeSIVaultMaterial(in siVaultKeyMaterial, target siVaultTarget) (siVaultKeyMaterial, error) {
	in.Repo = normalizeVaultRepoEnvSlug(firstNonEmpty(strings.TrimSpace(in.Repo), target.Repo))
	in.Env = normalizeVaultRepoEnvSlug(firstNonEmpty(strings.TrimSpace(in.Env), target.Env))
	in.PublicKey = strings.TrimSpace(strings.ToLower(in.PublicKey))
	in.PrivateKey = strings.TrimSpace(strings.ToLower(in.PrivateKey))
	if in.PrivateKey == "" {
		return siVaultKeyMaterial{}, fmt.Errorf("si vault key material missing private key for %s/%s", target.Repo, target.Env)
	}
	if in.PublicKey == "" {
		privateKey, err := ecies.NewPrivateKeyFromHex(in.PrivateKey)
		if err != nil {
			return siVaultKeyMaterial{}, fmt.Errorf("si vault private key is invalid: %w", err)
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

type siVaultKeyring struct {
	Entries map[string]siVaultKeyMaterial `json:"entries"`
}

func siVaultKeyringPath() string {
	if explicit := strings.TrimSpace(os.Getenv("SI_VAULT_KEYRING_FILE")); explicit != "" {
		return filepath.Clean(explicit)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "vault", "si-vault-keyring.json")
}

func siVaultKeyringEntryKey(repo string, env string) string {
	return normalizeVaultRepoEnvSlug(repo) + "/" + normalizeVaultRepoEnvSlug(env)
}

func loadSIVaultKeyring() (siVaultKeyring, error) {
	path := siVaultKeyringPath()
	if path == "" {
		return siVaultKeyring{Entries: map[string]siVaultKeyMaterial{}}, nil
	}
	if err := ensureStrictVaultSecretFile(path); err != nil {
		if os.IsNotExist(err) {
			return siVaultKeyring{Entries: map[string]siVaultKeyMaterial{}}, nil
		}
		return siVaultKeyring{}, err
	}
	// #nosec G304 -- path is from local trusted config/env.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return siVaultKeyring{Entries: map[string]siVaultKeyMaterial{}}, nil
		}
		return siVaultKeyring{}, err
	}
	var parsed siVaultKeyring
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return siVaultKeyring{}, err
	}
	if parsed.Entries == nil {
		parsed.Entries = map[string]siVaultKeyMaterial{}
	}
	return parsed, nil
}

func ensureStrictVaultSecretFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("secret file path must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("secret file permissions must be 0600 or stricter")
	}
	return nil
}

func saveSIVaultKeyring(keyring siVaultKeyring) error {
	path := siVaultKeyringPath()
	if path == "" {
		return fmt.Errorf("si vault keyring path is not configured")
	}
	if keyring.Entries == nil {
		keyring.Entries = map[string]siVaultKeyMaterial{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(keyring, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func siVaultPrivateKeyCandidates(material siVaultKeyMaterial) []string {
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

func dotenvSIVaultPublicKey(doc vault.DotenvFile) (string, bool, error) {
	raw, ok := doc.Lookup(vault.SIVaultPublicKeyName)
	if !ok {
		return "", false, nil
	}
	value, err := vault.NormalizeDotenvValue(raw)
	if err != nil {
		return "", true, fmt.Errorf("normalize %s: %w", vault.SIVaultPublicKeyName, err)
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", true, fmt.Errorf("%s is empty", vault.SIVaultPublicKeyName)
	}
	return value, true, nil
}

func siVaultPublicKeyCandidates(material siVaultKeyMaterial) []string {
	seen := map[string]struct{}{}
	out := []string{}
	appendPublic := func(raw string) {
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
	appendPublic(material.PublicKey)
	for _, privateKey := range siVaultPrivateKeyCandidates(material) {
		priv, err := ecies.NewPrivateKeyFromHex(privateKey)
		if err != nil {
			continue
		}
		appendPublic(priv.PublicKey.Hex(true))
	}
	return out
}

func ensureSIVaultDecryptMaterialCompatibility(doc vault.DotenvFile, material siVaultKeyMaterial, target siVaultTarget, _ Settings) error {
	entries, err := vault.Entries(doc)
	if err != nil {
		return err
	}
	hasEncrypted := false
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || key == vault.SIVaultPublicKeyName {
			continue
		}
		if vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			hasEncrypted = true
			break
		}
	}
	if !hasEncrypted {
		return nil
	}

	expected, hasHeader, err := dotenvSIVaultPublicKey(doc)
	if err != nil {
		return err
	}
	if !hasHeader {
		return nil
	}
	candidates := siVaultPublicKeyCandidates(material)
	if slices.Contains(candidates, expected) {
		return nil
	}
	active := strings.TrimSpace(strings.ToLower(material.PublicKey))
	if active == "" && len(candidates) > 0 {
		active = candidates[0]
	}
	return fmt.Errorf(
		"vault key drift detected for %s/%s (%s): dotenv %s=%s does not match active SI vault key material (active_public_key=%s); verify SI_VAULT_KEYRING_FILE and rerun `si vault keypair --repo %s --env %s`",
		target.Repo,
		target.Env,
		target.EnvFile,
		vault.SIVaultPublicKeyName,
		expected,
		active,
		target.Repo,
		target.Env,
	)
}

func analyzeDotenvDecryptability(doc vault.DotenvFile, privateKeyCandidates []string) (siVaultDecryptabilityStats, error) {
	stats := siVaultDecryptabilityStats{
		Undecryptable: []string{},
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		return stats, err
	}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || key == vault.SIVaultPublicKeyName {
			continue
		}
		if !vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			continue
		}
		stats.Encrypted++
		if _, decErr := vault.DecryptSIVaultValue(entry.ValueRaw, privateKeyCandidates); decErr != nil {
			stats.Undecryptable = append(stats.Undecryptable, key)
			continue
		}
		stats.Decryptable++
	}
	sort.Strings(stats.Undecryptable)
	return stats, nil
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
