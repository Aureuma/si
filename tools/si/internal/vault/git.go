package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitRoot(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("git root: dir required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git not found in PATH")
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git root not found (run inside a git repo): %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git root not found")
	}
	return filepath.Clean(root), nil
}

func GitHooksDir(repoDir string) (string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "rev-parse", "--git-path", "hooks")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	hooks := strings.TrimSpace(string(out))
	if hooks == "" {
		return "", fmt.Errorf("hooks dir not found")
	}
	// rev-parse returns a path relative to repoDir in some setups.
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(repoDir, hooks)
	}
	return filepath.Clean(hooks), nil
}

func GitStagedFiles(repoDir string) ([]string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return nil, fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(line))
	}
	return paths, nil
}

func GitShowIndexFile(repoDir, path string) ([]byte, error) {
	repoDir = strings.TrimSpace(repoDir)
	path = strings.TrimSpace(path)
	if repoDir == "" || path == "" {
		return nil, fmt.Errorf("repo dir and path required")
	}
	path, err := validateGitIndexPath(path)
	if err != nil {
		return nil, err
	}
	// #nosec G204 -- path is validated and exec.Command does not invoke a shell.
	cmd := exec.Command("git", "show", ":"+path)
	cmd.Dir = repoDir
	return cmd.Output()
}

func GitRemoteBranches(repoDir string) ([]string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return nil, fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "origin/HEAD" {
			continue
		}
		branches = append(branches, line)
	}
	return branches, nil
}

func GitCurrentBranch(repoDir string) (string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GitEnsureCheckout(repoDir string) error {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return fmt.Errorf("repo dir required")
	}
	if _, err := GitHeadCommit(repoDir); err == nil {
		return nil
	}

	branches, err := GitRemoteBranches(repoDir)
	if err == nil {
		hasRemote := map[string]bool{}
		for _, b := range branches {
			hasRemote[b] = true
		}
		if hasRemote["origin/main"] {
			if err := gitCheckoutBranch(repoDir, "main", "origin/main"); err == nil {
				return nil
			}
		}
		if hasRemote["origin/master"] {
			if err := gitCheckoutBranch(repoDir, "master", "origin/master"); err == nil {
				return nil
			}
		}
		for _, b := range branches {
			if strings.HasPrefix(b, "origin/") {
				name := strings.TrimPrefix(b, "origin/")
				if err := validateGitRefName(name); err != nil {
					continue
				}
				if err := validateGitRefName(b); err != nil {
					continue
				}
				if err := gitCheckoutBranch(repoDir, name, b); err == nil {
					return nil
				}
			}
		}
	}

	// Last resort for an empty or HEADless repo: create an orphan main branch.
	cmd := exec.Command("git", "checkout", "--orphan", "main")
	cmd.Dir = repoDir
	return cmd.Run()
}

func gitCheckoutBranch(repoDir, local, remote string) error {
	if err := validateGitRefName(local); err != nil {
		return err
	}
	if err := validateGitRefName(remote); err != nil {
		return err
	}
	// #nosec G204 -- refs are validated and exec.Command does not invoke a shell.
	cmd := exec.Command("git", "checkout", "-B", local, remote)
	cmd.Dir = repoDir
	return cmd.Run()
}

func validateGitIndexPath(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return "", fmt.Errorf("git path required")
	}
	if strings.HasPrefix(path, "-") {
		return "", fmt.Errorf("invalid git path %q: option-like paths are not allowed", path)
	}
	if strings.Contains(path, "\x00") {
		return "", fmt.Errorf("invalid git path %q: NUL byte is not allowed", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("invalid git path %q: must be repo-relative", path)
	}
	return clean, nil
}

func validateGitRefName(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("git ref required")
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid git ref %q: option-like refs are not allowed", ref)
	}
	if strings.ContainsAny(ref, " \t\n\r\x00\\~^:?*[") {
		return fmt.Errorf("invalid git ref %q: contains forbidden character", ref)
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "//") || strings.Contains(ref, "@{") {
		return fmt.Errorf("invalid git ref %q", ref)
	}
	if strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") {
		return fmt.Errorf("invalid git ref %q", ref)
	}
	return nil
}

func GitRemoteOriginURL(repoDir string) (string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GitHeadCommit(repoDir string) (string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GitDirty(repoDir string) (bool, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return false, fmt.Errorf("repo dir required")
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
