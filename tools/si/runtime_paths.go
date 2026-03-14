package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type resolvedPathSource string

const (
	resolvedPathSourceFlag     resolvedPathSource = "flag"
	resolvedPathSourceEnv      resolvedPathSource = "env"
	resolvedPathSourceSettings resolvedPathSource = "settings"
	resolvedPathSourceCwd      resolvedPathSource = "cwd"
	resolvedPathSourceRepo     resolvedPathSource = "repo"
	resolvedPathSourceBundled  resolvedPathSource = "bundled"
)

type resolvedDirectory struct {
	Path          string
	Source        resolvedPathSource
	StaleSettings bool
}

const bundledDyadConfigTemplate = `# managed by si-codex-init
#
# Shared Codex defaults for si dyads.
# Seeded from /opt/si/data/codex/shared/actor/config.toml

model = "__CODEX_MODEL__"
model_reasoning_effort = "__CODEX_REASONING_EFFORT__"

# Codex deprecated [features].web_search_request; configure web search at the top level.
# Values: "live" | "cached" | "disabled"
web_search = "live"

[sandbox_workspace_write]
network_access = true

[si]
dyad = "__DYAD_NAME__"
member = "__DYAD_MEMBER__"
role = "__ROLE__"
initialized_utc = "__INITIALIZED_UTC__"
`

func resolveDirectoryPath(path string, label string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s path is empty", label)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s path %q: %w", label, path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s path %q: %w", label, abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s path %q is not a directory", label, abs)
	}
	return abs, nil
}

func configuredDirectoryMissing(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	_, err := resolveDirectoryPath(path, "configured directory")
	return err != nil
}

func resolveWorkspaceDirectory(
	scope workspaceDefaultScope,
	flagProvided bool,
	flagValue string,
	envValue string,
	settings *Settings,
	cwd string,
) (resolvedDirectory, error) {
	label := fmt.Sprintf("%s workspace", workspaceScopeLabel(scope))
	if flagProvided {
		path, err := resolveDirectoryPath(flagValue, label)
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceFlag}, nil
	}
	if strings.TrimSpace(envValue) != "" {
		path, err := resolveDirectoryPath(envValue, label)
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceEnv}, nil
	}

	staleSettings := false
	if settings != nil {
		if configured := workspaceDefaultValue(*settings, scope); configured != "" {
			path, err := resolveDirectoryPath(configured, label)
			if err == nil {
				return resolvedDirectory{Path: path, Source: resolvedPathSourceSettings}, nil
			}
			staleSettings = true
		}
	}

	path, err := resolveDirectoryPath(cwd, label)
	if err != nil {
		return resolvedDirectory{}, err
	}
	return resolvedDirectory{
		Path:          path,
		Source:        resolvedPathSourceCwd,
		StaleSettings: staleSettings,
	}, nil
}

func resolveWorkspaceRootDirectory(
	flagProvided bool,
	flagValue string,
	envValue string,
	settings *Settings,
	cwd string,
) (resolvedDirectory, error) {
	if flagProvided {
		path, err := resolveDirectoryPath(flagValue, "workspace root")
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceFlag}, nil
	}
	if strings.TrimSpace(envValue) != "" {
		path, err := resolveDirectoryPath(envValue, "workspace root")
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceEnv}, nil
	}

	staleSettings := false
	if settings != nil {
		if configured := strings.TrimSpace(settings.Paths.WorkspaceRoot); configured != "" {
			path, err := resolveDirectoryPath(configured, "workspace root")
			if err == nil {
				return resolvedDirectory{Path: path, Source: resolvedPathSourceSettings}, nil
			}
			staleSettings = true
		}
	}

	path, err := inferWorkspaceRootFromCWD(cwd)
	if err != nil {
		return resolvedDirectory{}, err
	}
	return resolvedDirectory{
		Path:          path,
		Source:        resolvedPathSourceCwd,
		StaleSettings: staleSettings,
	}, nil
}

func resolveDyadConfigsDirectory(
	flagProvided bool,
	flagValue string,
	envValue string,
	settings *Settings,
	workspaceHost string,
) (resolvedDirectory, error) {
	if flagProvided {
		path, err := resolveDirectoryPath(flagValue, "dyad configs")
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceFlag}, nil
	}
	if strings.TrimSpace(envValue) != "" {
		path, err := resolveDirectoryPath(envValue, "dyad configs")
		if err != nil {
			return resolvedDirectory{}, err
		}
		return resolvedDirectory{Path: path, Source: resolvedPathSourceEnv}, nil
	}

	staleSettings := false
	if settings != nil {
		if configured := strings.TrimSpace(settings.Dyad.Configs); configured != "" {
			path, err := resolveDirectoryPath(configured, "dyad configs")
			if err == nil {
				return resolvedDirectory{Path: path, Source: resolvedPathSourceSettings}, nil
			}
			staleSettings = true
		}
	}

	if root, err := repoRootFrom(workspaceHost); err == nil {
		path := filepath.Join(root, "configs")
		if resolved, err := resolveDirectoryPath(path, "dyad configs"); err == nil {
			return resolvedDirectory{Path: resolved, Source: resolvedPathSourceRepo, StaleSettings: staleSettings}, nil
		}
	}
	if root, err := repoRoot(); err == nil {
		path := filepath.Join(root, "configs")
		if resolved, err := resolveDirectoryPath(path, "dyad configs"); err == nil {
			return resolvedDirectory{Path: resolved, Source: resolvedPathSourceRepo, StaleSettings: staleSettings}, nil
		}
	}
	if root, err := repoRootFromExecutable(); err == nil {
		path := filepath.Join(root, "configs")
		if resolved, err := resolveDirectoryPath(path, "dyad configs"); err == nil {
			return resolvedDirectory{Path: resolved, Source: resolvedPathSourceRepo, StaleSettings: staleSettings}, nil
		}
	}

	bundled, err := ensureBundledDyadConfigsDir()
	if err != nil {
		return resolvedDirectory{}, err
	}
	return resolvedDirectory{
		Path:          bundled,
		Source:        resolvedPathSourceBundled,
		StaleSettings: staleSettings,
	}, nil
}

func inferWorkspaceRootFromCWD(cwd string) (string, error) {
	cwd, err := resolveDirectoryPath(cwd, "workspace root")
	if err != nil {
		return "", err
	}
	if hasDirectGitChildren(cwd) {
		return cwd, nil
	}
	repoRoot, err := gitRepoRootFrom(cwd)
	if err != nil {
		return "", fmt.Errorf("unable to determine workspace root from %s; pass --root or set [paths].workspace_root", cwd)
	}
	parent := filepath.Dir(repoRoot)
	if parent == repoRoot {
		return "", fmt.Errorf("unable to determine workspace root from %s; pass --root or set [paths].workspace_root", cwd)
	}
	return resolveDirectoryPath(parent, "workspace root")
}

func hasDirectGitChildren(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if isGitRepoDir(filepath.Join(dir, entry.Name())) {
			return true
		}
	}
	return false
}

func gitRepoRootFrom(start string) (string, error) {
	start = strings.TrimSpace(start)
	if start == "" {
		return "", fmt.Errorf("git repo root not found (empty start dir)")
	}
	dir := filepath.Clean(start)
	for {
		if isGitRepoDir(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("git repo root not found from %s", start)
}

func isGitRepoDir(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}

func ensureBundledDyadConfigsDir() (string, error) {
	root, err := settingsRootPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	templatePath := filepath.Join(dir, "codex-config.template.toml")
	if err := ensureFileContent(templatePath, bundledDyadConfigTemplate, 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func ensureFileContent(path string, content string, mode os.FileMode) error {
	current, err := os.ReadFile(path)
	if err == nil && string(current) == content {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func abbreviateHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	if path == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(filepath.Separator) + strings.TrimPrefix(path, prefix)
	}
	return path
}
