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

var (
	// ErrIdentityNotFound indicates there is no configured identity available from
	// the selected source (env, file, or OS secure store).
	ErrIdentityNotFound = errors.New("vault identity not found")

	// ErrIdentityInvalid indicates an identity was found but is malformed/unusable.
	// We treat this as a hard error to avoid silently rotating keys, which can
	// permanently break decryption for existing ciphertext.
	ErrIdentityInvalid = errors.New("vault identity invalid")
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

func NormalizeKeyBackend(backend string) string {
	backend = strings.ToLower(strings.TrimSpace(backend))
	switch backend {
	case "keychain":
		return "keyring"
	default:
		return backend
	}
}

func LoadIdentity(cfg KeyConfig) (*IdentityInfo, error) {
	// CI/ephemeral override: environment variable identity.
	if raw := strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")); raw != "" {
		id, err := age.ParseX25519Identity(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: SI_VAULT_IDENTITY invalid: %w", ErrIdentityInvalid, err)
		}
		return &IdentityInfo{Identity: id, Source: "env"}, nil
	}
	if raw := strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")); raw != "" {
		id, err := age.ParseX25519Identity(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: SI_VAULT_PRIVATE_KEY invalid: %w", ErrIdentityInvalid, err)
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

	backend := NormalizeKeyBackend(cfg.Backend)
	if backend == "" {
		backend = "keyring"
	}
	switch backend {
	case "keyring":
		secret, err := keyringGet(KeyringService, KeyringAccount)
		if err == nil {
			id, err := age.ParseX25519Identity(strings.TrimSpace(secret))
			if err != nil {
				return nil, fmt.Errorf("%w: keyring identity invalid: %w", ErrIdentityInvalid, err)
			}
			return &IdentityInfo{Identity: id, Source: "keyring"}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: vault identity not found in keyring (run `si vault keygen` or set vault.key_backend=\"file\")", ErrIdentityNotFound)
	case "file":
		keyFile := strings.TrimSpace(cfg.KeyFile)
		if keyFile == "" {
			return nil, fmt.Errorf("vault.key_file required for file backend")
		}
		keyFile, err := ExpandHome(keyFile)
		if err != nil {
			return nil, err
		}
		if _, statErr := os.Stat(keyFile); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: vault identity file missing (%s) (run `si vault keygen --key-backend file --key-file %s`)", ErrIdentityNotFound, filepath.Clean(keyFile), filepath.Clean(keyFile))
			}
			return nil, statErr
		}
		id, err := loadIdentityFromFile(keyFile)
		if err != nil {
			return nil, err
		}
		return &IdentityInfo{Identity: id, Source: "file", Path: keyFile}, nil
	default:
		return nil, fmt.Errorf("unsupported key backend %q (expected keyring, keychain, or file)", cfg.Backend)
	}
}

func EnsureIdentity(cfg KeyConfig) (*IdentityInfo, bool, error) {
	info, err := LoadIdentity(cfg)
	if err == nil {
		return info, false, nil
	}
	// Never overwrite/rotate an existing-but-invalid identity implicitly; doing so
	// can permanently break decryption for existing ciphertext.
	if !errors.Is(err, ErrIdentityNotFound) {
		return nil, false, err
	}

	backend := NormalizeKeyBackend(cfg.Backend)
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
			return nil, false, err
		}
		return &IdentityInfo{Identity: id, Source: "keyring"}, true, nil
	case "file":
		info, ok, err := ensureIdentityFile(cfg, id)
		return info, ok, err
	default:
		return nil, false, fmt.Errorf("unsupported key backend %q (expected keyring, keychain, or file)", cfg.Backend)
	}
}

// RotateIdentity generates and stores a new identity, even if one already exists.
// WARNING: rotating without retaining the old key will prevent decrypting any
// ciphertext encrypted to the old recipient.
func RotateIdentity(cfg KeyConfig) (*IdentityInfo, error) {
	backend := NormalizeKeyBackend(cfg.Backend)
	if backend == "" {
		backend = "keyring"
	}
	id, err := GenerateIdentity()
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(id.String())
	switch backend {
	case "keyring":
		if err := keyringSet(KeyringService, KeyringAccount, secret); err != nil {
			return nil, err
		}
		return &IdentityInfo{Identity: id, Source: "keyring"}, nil
	case "file":
		keyFile := strings.TrimSpace(cfg.KeyFile)
		if keyFile == "" {
			return nil, fmt.Errorf("vault.key_file required for file backend")
		}
		keyFile, err := ExpandHome(keyFile)
		if err != nil {
			return nil, err
		}
		if !isTruthyEnv("SI_VAULT_ALLOW_INSECURE_KEY_FILE") {
			if info, err := os.Lstat(keyFile); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return nil, fmt.Errorf("insecure key file (%s): symlinks are not allowed (set SI_VAULT_ALLOW_INSECURE_KEY_FILE=1 to override)", filepath.Clean(keyFile))
				}
			}
		}
		if err := saveIdentityToFile(keyFile, secret); err != nil {
			return nil, err
		}
		return &IdentityInfo{Identity: id, Source: "file", Path: keyFile}, nil
	default:
		return nil, fmt.Errorf("unsupported key backend %q (expected keyring, keychain, or file)", cfg.Backend)
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
	data, err := readFileScoped(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: vault identity file missing (%s)", ErrIdentityNotFound, filepath.Clean(path))
		}
		return nil, err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			id, err := age.ParseX25519Identity(line)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid AGE-SECRET-KEY in %s: %w", ErrIdentityInvalid, filepath.Clean(path), err)
			}
			return id, nil
		}
	}
	return nil, fmt.Errorf("%w: no AGE-SECRET-KEY found in %s", ErrIdentityInvalid, filepath.Clean(path))
}

func ensureSecureKeyFile(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("key file path required")
	}
	if isTruthyEnv("SI_VAULT_ALLOW_INSECURE_KEY_FILE") {
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
