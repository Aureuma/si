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
	File           string // absolute path to the target env file
	FileIsExplicit bool
}

type ResolveOptions struct {
	CWD string

	// File is the explicitly provided env file path (flag).
	File string

	// DefaultFile is used when File is empty.
	DefaultFile string

	AllowMissingFile bool
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

	file := strings.TrimSpace(opts.File)
	fileIsExplicit := file != ""
	if file == "" {
		file = strings.TrimSpace(opts.DefaultFile)
	}
	if file == "" {
		return Target{}, fmt.Errorf("vault env file not configured (run `si vault init` or pass --file)")
	}

	fileAbs, err := CleanAbsFrom(cwd, file)
	if err != nil {
		return Target{}, err
	}
	if !opts.AllowMissingFile {
		if _, err := os.Stat(fileAbs); err != nil {
			return Target{}, err
		}
	}

	// Repo root is used for optional features (trust + staged checks). It can be empty.
	repoRoot, _ := GitRoot(filepath.Dir(fileAbs))
	if repoRoot == "" {
		repoRoot, _ = GitRoot(cwd)
	}

	return Target{
		CWD:            cwd,
		RepoRoot:       repoRoot,
		File:           filepath.Clean(fileAbs),
		FileIsExplicit: fileIsExplicit,
	}, nil
}

