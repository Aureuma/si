package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Target struct {
	CWD            string
	RepoRoot       string // git repo root when resolvable
	VaultDir       string // absolute path (directory containing env files)
	VaultDirRel    string // relative to RepoRoot when known
	File           string // absolute path to the target env file
	FileIsExplicit bool
}

type ResolveOptions struct {
	CWD string

	File     string
	VaultDir string

	DefaultVaultDir string

	AllowMissingVaultDir bool
	AllowMissingFile     bool
}

func ResolveTarget(opts ResolveOptions) (Target, error) {
	cwd := strings.TrimSpace(opts.CWD)
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Target{}, err
		}
	}

	if strings.TrimSpace(opts.File) != "" {
		fileAbs, err := CleanAbs(opts.File)
		if err != nil {
			return Target{}, err
		}
		repoRoot, _ := GitRoot(filepath.Dir(fileAbs))
		vaultDir := filepath.Dir(fileAbs)
		vaultRel := ""
		if repoRoot != "" {
			if rel, err := filepath.Rel(repoRoot, vaultDir); err == nil {
				vaultRel = filepath.Clean(rel)
			}
		}
		if !opts.AllowMissingFile {
			if _, err := os.Stat(fileAbs); err != nil {
				return Target{}, err
			}
		}
		return Target{
			CWD:            cwd,
			RepoRoot:       repoRoot,
			VaultDir:       filepath.Clean(vaultDir),
			VaultDirRel:    vaultRel,
			File:           fileAbs,
			FileIsExplicit: true,
		}, nil
	}

	repoRoot, err := GitRoot(cwd)
	if err != nil {
		return Target{}, err
	}
	vaultRel := strings.TrimSpace(opts.VaultDir)
	if vaultRel == "" {
		vaultRel = strings.TrimSpace(opts.DefaultVaultDir)
	}
	if vaultRel == "" {
		vaultRel = "vault"
	}
	vaultAbs := vaultRel
	if !filepath.IsAbs(vaultAbs) {
		vaultAbs = filepath.Join(repoRoot, vaultRel)
	}
	vaultAbs = filepath.Clean(vaultAbs)
	if !opts.AllowMissingVaultDir && !IsDir(vaultAbs) {
		return Target{}, fmt.Errorf("vault dir not found: %s (run si vault init)", vaultAbs)
	}
	vaultRelClean := filepath.Clean(vaultRel)
	if filepath.IsAbs(vaultRelClean) {
		if rel, err := filepath.Rel(repoRoot, vaultAbs); err == nil {
			vaultRelClean = filepath.Clean(rel)
		}
	}

	// Default vault env file is a plain ".env" inside the vault dir. If you want
	// multiple dotenv files (e.g. .env.dev/.env.prod), use --file explicitly.
	fileAbs := filepath.Join(vaultAbs, ".env")
	if !opts.AllowMissingFile {
		if _, err := os.Stat(fileAbs); err != nil {
			return Target{}, err
		}
	}

	return Target{
		CWD:            cwd,
		RepoRoot:       repoRoot,
		VaultDir:       vaultAbs,
		VaultDirRel:    vaultRelClean,
		File:           fileAbs,
		FileIsExplicit: false,
	}, nil
}
