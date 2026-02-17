package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	paasContextConfigFileName  = "config.json"
	paasContextCurrentFileName = "current_context"
)

type paasContextConfig struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	StateRoot string `json:"state_root,omitempty"`
	VaultFile string `json:"vault_file,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func initializePaasContextLayout(name, contextType, stateRoot, vaultFile string) (paasContextConfig, error) {
	resolvedName := strings.TrimSpace(name)
	if resolvedName == "" {
		return paasContextConfig{}, fmt.Errorf("context name is required")
	}
	contextDir, err := resolvePaasContextDir(resolvedName)
	if err != nil {
		return paasContextConfig{}, err
	}
	root, err := resolvePaasStateRoot()
	if err != nil {
		return paasContextConfig{}, err
	}
	resolvedType := strings.ToLower(strings.TrimSpace(contextType))
	if resolvedType == "" {
		resolvedType = "internal-dogfood"
	}
	resolvedStateRoot := strings.TrimSpace(stateRoot)
	if resolvedStateRoot == "" {
		resolvedStateRoot = root
	}
	resolvedVaultFile := strings.TrimSpace(vaultFile)
	if resolvedVaultFile == "" {
		resolvedVaultFile = filepath.Join(contextDir, "vault", "secrets.env")
	}

	dirs := []string{
		contextDir,
		filepath.Join(contextDir, "events"),
		filepath.Join(contextDir, "cache"),
		filepath.Join(contextDir, "vault"),
		filepath.Join(contextDir, "releases"),
		filepath.Join(contextDir, "addons"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return paasContextConfig{}, err
		}
	}

	config := paasContextConfig{
		Name:      resolvedName,
		Type:      resolvedType,
		StateRoot: resolvedStateRoot,
		VaultFile: resolvedVaultFile,
		CreatedAt: utcNowRFC3339(),
		UpdatedAt: utcNowRFC3339(),
	}
	if existing, err := loadPaasContextConfig(resolvedName); err == nil {
		config.CreatedAt = firstNonEmptyString(strings.TrimSpace(existing.CreatedAt), config.CreatedAt)
	}
	if err := savePaasContextConfig(config); err != nil {
		return paasContextConfig{}, err
	}
	return config, nil
}

func resolvePaasContextConfigPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, paasContextConfigFileName), nil
}

func loadPaasContextConfig(contextName string) (paasContextConfig, error) {
	path, err := resolvePaasContextConfigPath(contextName)
	if err != nil {
		return paasContextConfig{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path derived from context root.
	if err != nil {
		return paasContextConfig{}, err
	}
	var config paasContextConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return paasContextConfig{}, err
	}
	config.Name = strings.TrimSpace(config.Name)
	config.Type = strings.ToLower(strings.TrimSpace(config.Type))
	config.StateRoot = strings.TrimSpace(config.StateRoot)
	config.VaultFile = strings.TrimSpace(config.VaultFile)
	config.CreatedAt = strings.TrimSpace(config.CreatedAt)
	config.UpdatedAt = strings.TrimSpace(config.UpdatedAt)
	return config, nil
}

func savePaasContextConfig(config paasContextConfig) error {
	path, err := resolvePaasContextConfigPath(config.Name)
	if err != nil {
		return err
	}
	config.Name = strings.TrimSpace(config.Name)
	config.Type = strings.ToLower(strings.TrimSpace(config.Type))
	config.StateRoot = strings.TrimSpace(config.StateRoot)
	config.VaultFile = strings.TrimSpace(config.VaultFile)
	config.CreatedAt = strings.TrimSpace(config.CreatedAt)
	config.UpdatedAt = strings.TrimSpace(config.UpdatedAt)
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func listPaasContextConfigs() ([]paasContextConfig, error) {
	root, err := resolvePaasStateRoot()
	if err != nil {
		return nil, err
	}
	contextsDir := filepath.Join(root, "contexts")
	entries, err := os.ReadDir(contextsDir) // #nosec G304 -- path derived from state root.
	if err != nil {
		if os.IsNotExist(err) {
			return []paasContextConfig{}, nil
		}
		return nil, err
	}
	out := make([]paasContextConfig, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		config, err := loadPaasContextConfig(name)
		if err != nil {
			config = paasContextConfig{Name: name}
		}
		if strings.TrimSpace(config.Name) == "" {
			config.Name = name
		}
		out = append(out, config)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(out[i].Name)) < strings.ToLower(strings.TrimSpace(out[j].Name))
	})
	return out, nil
}

func resolvePaasCurrentContextFilePath() (string, error) {
	root, err := resolvePaasStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, paasContextCurrentFileName), nil
}

func savePaasSelectedContext(name string) error {
	path, err := resolvePaasCurrentContextFilePath()
	if err != nil {
		return err
	}
	selected := strings.TrimSpace(name)
	if selected == "" {
		return fmt.Errorf("context name is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(selected+"\n"), 0o600)
}

func loadPaasSelectedContext() (string, error) {
	path, err := resolvePaasCurrentContextFilePath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path derived from state root.
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func removePaasContextLayout(name string, force bool) error {
	contextDir, err := resolvePaasContextDir(name)
	if err != nil {
		return err
	}
	if !force {
		entries, err := os.ReadDir(contextDir) // #nosec G304 -- path derived from context root.
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("context %q is not empty; pass --force to remove", strings.TrimSpace(name))
		}
	}
	if err := os.RemoveAll(contextDir); err != nil {
		return err
	}
	current, err := loadPaasSelectedContext()
	if err == nil && strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(name)) {
		_ = savePaasSelectedContext(defaultPaasContext)
	}
	return nil
}
