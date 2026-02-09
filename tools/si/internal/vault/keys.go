package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

const (
	KeyringService = "si-vault"
	KeyringAccount = "age-identity"
)

type IdentityInfo struct {
	Identity *age.X25519Identity
	Source   string // env, env_file, keyring, file
	Path     string // for *_file sources
}

type KeyConfig struct {
	Backend string // keyring|file
	KeyFile string // for file backend
}

func LoadIdentity(cfg KeyConfig) (*IdentityInfo, error) {
	// CI/ephemeral override: environment variable identity.
	if raw := strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")); raw != "" {
		id, err := age.ParseX25519Identity(raw)
		if err != nil {
			return nil, fmt.Errorf("SI_VAULT_IDENTITY invalid: %w", err)
		}
		return &IdentityInfo{Identity: id, Source: "env"}, nil
	}
	if raw := strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")); raw != "" {
		id, err := age.ParseX25519Identity(raw)
		if err != nil {
			return nil, fmt.Errorf("SI_VAULT_PRIVATE_KEY invalid: %w", err)
		}
		return &IdentityInfo{Identity: id, Source: "env"}, nil
	}
	if path := strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")); path != "" {
		path, err := ExpandHome(path)
		if err != nil {
			return nil, err
		}
		id, err := loadIdentityFromFile(path)
		if err != nil {
			return nil, err
		}
		return &IdentityInfo{Identity: id, Source: "env_file", Path: path}, nil
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "keyring"
	}
	switch backend {
	case "keyring":
		secret, err := keyringGet(KeyringService, KeyringAccount)
		if err == nil {
			id, err := age.ParseX25519Identity(strings.TrimSpace(secret))
			if err != nil {
				return nil, fmt.Errorf("keyring identity invalid: %w", err)
			}
			return &IdentityInfo{Identity: id, Source: "keyring"}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		// Fall through to file backend if configured, or if the key file already exists.
		if strings.ToLower(strings.TrimSpace(cfg.Backend)) == "file" {
			break
		}
		if keyFile := strings.TrimSpace(cfg.KeyFile); keyFile != "" {
			keyFile, expandErr := ExpandHome(keyFile)
			if expandErr == nil {
				if _, statErr := os.Stat(keyFile); statErr == nil {
					cfg.Backend = "file"
					break
				}
			}
		}
		return nil, fmt.Errorf("vault identity not found in keyring; set vault.key_backend=\"file\" or export SI_VAULT_IDENTITY")
	case "file":
		keyFile := strings.TrimSpace(cfg.KeyFile)
		if keyFile == "" {
			return nil, fmt.Errorf("vault.key_file required for file backend")
		}
		keyFile, err := ExpandHome(keyFile)
		if err != nil {
			return nil, err
		}
		id, err := loadIdentityFromFile(keyFile)
		if err != nil {
			return nil, err
		}
		return &IdentityInfo{Identity: id, Source: "file", Path: keyFile}, nil
	default:
		return nil, fmt.Errorf("unsupported key backend %q (expected keyring or file)", cfg.Backend)
	}

	keyFile := strings.TrimSpace(cfg.KeyFile)
	if keyFile == "" {
		return nil, fmt.Errorf("vault.key_file required for file backend")
	}
	keyFile, err := ExpandHome(keyFile)
	if err != nil {
		return nil, err
	}
	id, err := loadIdentityFromFile(keyFile)
	if err != nil {
		return nil, err
	}
	return &IdentityInfo{Identity: id, Source: "file", Path: keyFile}, nil
}

func EnsureIdentity(cfg KeyConfig) (*IdentityInfo, bool, error) {
	info, err := LoadIdentity(cfg)
	if err == nil {
		return info, false, nil
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "keyring"
	}
	id, err := GenerateIdentity()
	if err != nil {
		return nil, false, err
	}
	secret := strings.TrimSpace(id.String())

	switch backend {
	case "keyring":
		if err := keyringSet(KeyringService, KeyringAccount, secret); err != nil {
			// If keyring isn't available, fall back to file backend if we can.
			if cfg.KeyFile != "" {
				fall := KeyConfig{Backend: "file", KeyFile: cfg.KeyFile}
				info, ok, fileErr := ensureIdentityFile(fall, id)
				return info, ok, fileErr
			}
			return nil, false, err
		}
		return &IdentityInfo{Identity: id, Source: "keyring"}, true, nil
	case "file":
		info, ok, err := ensureIdentityFile(cfg, id)
		return info, ok, err
	default:
		return nil, false, fmt.Errorf("unsupported key backend %q (expected keyring or file)", cfg.Backend)
	}
}

func ensureIdentityFile(cfg KeyConfig, id *age.X25519Identity) (*IdentityInfo, bool, error) {
	keyFile := strings.TrimSpace(cfg.KeyFile)
	if keyFile == "" {
		return nil, false, fmt.Errorf("vault.key_file required for file backend")
	}
	keyFile, err := ExpandHome(keyFile)
	if err != nil {
		return nil, false, err
	}
	if _, err := os.Stat(keyFile); err == nil {
		existing, err := loadIdentityFromFile(keyFile)
		if err != nil {
			return nil, false, err
		}
		return &IdentityInfo{Identity: existing, Source: "file", Path: keyFile}, false, nil
	}
	if err := saveIdentityToFile(keyFile, strings.TrimSpace(id.String())); err != nil {
		return nil, false, err
	}
	return &IdentityInfo{Identity: id, Source: "file", Path: keyFile}, true, nil
}

func loadIdentityFromFile(path string) (*age.X25519Identity, error) {
	if err := ensureSecureKeyFile(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return age.ParseX25519Identity(line)
		}
	}
	return nil, fmt.Errorf("no AGE-SECRET-KEY found in %s", filepath.Clean(path))
}

func ensureSecureKeyFile(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("key file path required")
	}
	if strings.TrimSpace(os.Getenv("SI_VAULT_ALLOW_INSECURE_KEY_FILE")) != "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("insecure key file (%s): symlinks are not allowed (set SI_VAULT_ALLOW_INSECURE_KEY_FILE=1 to override)", filepath.Clean(path))
	}
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		return fmt.Errorf("insecure key file permissions (%s): expected 0600, got %04o (chmod 600 %s)", filepath.Clean(path), perm, filepath.Clean(path))
	}
	return nil
}

func saveIdentityToFile(path string, secret string) error {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "age-identity-*.key")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(secret + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
