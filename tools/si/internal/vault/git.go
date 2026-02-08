package vault

import (
	"bytes"
	"errors"
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

func GitSubmoduleAdd(repoRoot, url, path string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	url = strings.TrimSpace(url)
	path = strings.TrimSpace(path)
	if repoRoot == "" || url == "" || path == "" {
		return fmt.Errorf("repo root, url, and path required")
	}
	cmd := exec.Command("git", "submodule", "add", url, path)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func GitSubmoduleUpdate(repoRoot, path string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	path = strings.TrimSpace(path)
	if repoRoot == "" || path == "" {
		return fmt.Errorf("repo root and path required")
	}
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", "--", path)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func GitSubmoduleAddForce(repoRoot, url, path, branch string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	url = strings.TrimSpace(url)
	path = strings.TrimSpace(path)
	branch = strings.TrimSpace(branch)
	if repoRoot == "" || url == "" || path == "" {
		return fmt.Errorf("repo root, url, and path required")
	}
	args := []string{"submodule", "add", "--force"}
	if branch != "" {
		args = append(args, "-b", branch)
	}
	args = append(args, url, path)
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
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
		for _, candidate := range []string{"main", "master"} {
			remote := "origin/" + candidate
			if !hasRemote[remote] {
				continue
			}
			cmd := exec.Command("git", "checkout", "-B", candidate, remote)
			cmd.Dir = repoDir
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		for _, b := range branches {
			if strings.HasPrefix(b, "origin/") {
				name := strings.TrimPrefix(b, "origin/")
				if strings.TrimSpace(name) == "" {
					continue
				}
				cmd := exec.Command("git", "checkout", "-B", name, b)
				cmd.Dir = repoDir
				if err := cmd.Run(); err == nil {
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

type SubmoduleStatus struct {
	Present bool
	Prefix  string // " ", "-", "+", "U"
	Commit  string
	Path    string
	Meta    string
}

func GitSubmoduleStatus(repoRoot, path string) (*SubmoduleStatus, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	path = strings.TrimSpace(path)
	if repoRoot == "" || path == "" {
		return nil, fmt.Errorf("repo root and path required")
	}
	cmd := exec.Command("git", "submodule", "status", "--", path)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		// If the path isn't a submodule, treat as "not present" instead of fatal.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return &SubmoduleStatus{Present: false, Path: path}, nil
		}
		return nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return &SubmoduleStatus{Present: false, Path: path}, nil
	}
	// Example: " 1d2c3f4 vault (heads/main)"
	prefix := string(line[0])
	rest := strings.TrimSpace(line[1:])
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return &SubmoduleStatus{Present: true, Prefix: prefix, Path: path}, nil
	}
	commit := fields[0]
	actualPath := fields[1]
	meta := strings.TrimSpace(strings.TrimPrefix(rest, commit+" "+actualPath))
	return &SubmoduleStatus{
		Present: true,
		Prefix:  prefix,
		Commit:  commit,
		Path:    actualPath,
		Meta:    meta,
	}, nil
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
