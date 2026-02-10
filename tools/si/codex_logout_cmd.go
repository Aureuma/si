package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codexLogoutOptions struct {
	Home  string
	All   bool
}

type codexLogoutResult struct {
	Removed []string
	Skipped []string
}

func cmdCodexLogout(args []string) {
	fs := flag.NewFlagSet("logout", flag.ExitOnError)
	force := fs.Bool("force", false, "delete without prompting")
	all := fs.Bool("all", false, "also remove si-managed codex auth cache under ~/.si/codex (in addition to ~/.codex)")
	_ = fs.Parse(args)
	if fs.NArg() != 0 {
		printUsage("usage: si logout [--force] [--all]")
		return
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = fmt.Errorf("home dir not available")
		}
		fatal(err)
	}

	if !*force {
		prompt := "Delete host codex cache at " + filepath.Join(home, ".codex")
		if *all {
			prompt += " and si-managed codex cache at " + filepath.Join(home, ".si", "codex")
		}
		prompt += "?"
		confirmed, ok := confirmYN(prompt, false)
		if !ok {
			fatal(fmt.Errorf("non-interactive; re-run with --force"))
		}
		if !confirmed {
			return
		}
	}

	res, err := codexLogout(codexLogoutOptions{
		Home:  home,
		All:   *all,
	})
	if err != nil {
		fatal(err)
	}

	if len(res.Removed) == 0 && len(res.Skipped) == 0 {
		successf("codex logout complete (nothing to remove)")
		return
	}
	for _, p := range res.Removed {
		successf("removed %s", p)
	}
	for _, p := range res.Skipped {
		infof("skipped %s", p)
	}
}

func codexLogout(opts codexLogoutOptions) (codexLogoutResult, error) {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		return codexLogoutResult{}, fmt.Errorf("home required")
	}
	paths := []string{filepath.Join(home, ".codex")}
	if opts.All {
		paths = append(paths, filepath.Join(home, ".si", "codex"))
	}

	res := codexLogoutResult{}
	for _, p := range paths {
		removed, err := removeHomeChildDir(home, p)
		if err != nil {
			return res, err
		}
		if removed {
			res.Removed = append(res.Removed, p)
		} else {
			res.Skipped = append(res.Skipped, p)
		}
	}
	return res, nil
}

// removeHomeChildDir removes the given path if it exists and is a directory (or a symlink to a directory).
// It refuses to remove anything outside the provided home directory, and refuses obviously-dangerous paths.
func removeHomeChildDir(home, target string) (bool, error) {
	home = filepath.Clean(strings.TrimSpace(home))
	target = filepath.Clean(strings.TrimSpace(target))
	if home == "" || target == "" {
		return false, fmt.Errorf("invalid path")
	}
	if target == "/" || target == "." {
		return false, fmt.Errorf("refusing to remove %q", target)
	}

	// Ensure target is inside home.
	rel, err := filepath.Rel(home, target)
	if err != nil {
		return false, err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return false, fmt.Errorf("refusing to remove path outside home: %s", target)
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Remove the symlink itself (do not follow).
		if err := os.Remove(target); err != nil {
			return false, err
		}
		return true, nil
	}
	if !info.IsDir() {
		return false, fmt.Errorf("refusing to remove non-directory %s", target)
	}
	if err := os.RemoveAll(target); err != nil {
		return false, err
	}
	return true, nil
}
